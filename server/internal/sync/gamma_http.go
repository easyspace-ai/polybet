package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/easyspace-ai/polybet/internal/logx"
)

const gammaAPI = "https://gamma-api.polymarket.com"

// optionalFee unmarshals Gamma fee fields that may be JSON strings or numbers
// (API responses are not consistent across leagues / versions).
type optionalFee struct {
	s *string
}

func (o *optionalFee) UnmarshalJSON(data []byte) error {
	if o == nil {
		return fmt.Errorf("optionalFee: nil receiver")
	}
	o.s = nil
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	switch data[0] {
	case '"':
		var str string
		if err := json.Unmarshal(data, &str); err != nil {
			return err
		}
		str = strings.TrimSpace(str)
		if str != "" {
			o.s = &str
		}
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		var n json.Number
		if err := json.Unmarshal(data, &n); err != nil {
			return err
		}
		str := strings.TrimSpace(string(n))
		if str != "" {
			o.s = &str
		}
	default:
		return fmt.Errorf("optionalFee: unsupported JSON %s", string(data))
	}
	return nil
}

func optionalFeeString(o *optionalFee) *string {
	if o == nil {
		return nil
	}
	return o.s
}

type gammaMarket struct {
	ConditionID      string   `json:"conditionId"`
	Question         string   `json:"question"`
	ClobTokenIDs     string   `json:"clobTokenIds"`
	Outcomes         string   `json:"outcomes"`
	OutcomePrices    string   `json:"outcomePrices"`
	Active           bool     `json:"active"`
	Closed           bool     `json:"closed"`
	Liquidity        string   `json:"liquidity"`
	SportsMarketType *string  `json:"sportsMarketType"`
	Line             *float64 `json:"line"`
	GameStartTime    *string  `json:"gameStartTime"`

	// Fee fields. Polymarket has shipped multiple shapes over time and Gamma
	// is not always consistent with the CLOB new_market event. We accept any
	// of the following and pick the first non-zero in marketFeeRate:
	//
	//   feeRateBps      decimal string in basis points ("200" → 0.02)
	//   feeRate         decimal string as fraction ("0.02" → 0.02)
	//   takerBaseFee    same shape as feeRate (legacy)
	//
	// The legacy hardcoded fallback (sportsFeeRate=0.03) is preserved when
	// none of these decode, so behaviour is unchanged for unparseable rows.
	FeeRateBps   *optionalFee `json:"feeRateBps,omitempty"`
	FeeRate      *optionalFee `json:"feeRate,omitempty"`
	TakerBaseFee *optionalFee `json:"takerBaseFee,omitempty"`
}

// marketFeeRate returns the per-market taker fee as a fraction (e.g. 0.02
// for 2%). Returns ok=false when no field on the market decodes to a
// finite, non-negative, plausible (< 1.0) value, in which case callers
// should fall back to the league-wide default.
func marketFeeRate(m *gammaMarket) (float64, bool) {
	if m == nil {
		return 0, false
	}
	parse := func(s *string, scale float64) (float64, bool) {
		if s == nil {
			return 0, false
		}
		t := strings.TrimSpace(*s)
		if t == "" {
			return 0, false
		}
		v, err := strconv.ParseFloat(t, 64)
		if err != nil {
			return 0, false
		}
		v *= scale
		if v < 0 || v >= 1 {
			return 0, false
		}
		return v, true
	}
	if v, ok := parse(optionalFeeString(m.FeeRateBps), 0.0001); ok {
		return v, true
	}
	if v, ok := parse(optionalFeeString(m.FeeRate), 1); ok {
		return v, true
	}
	if v, ok := parse(optionalFeeString(m.TakerBaseFee), 1); ok {
		return v, true
	}
	return 0, false
}

type gammaEvent struct {
	ID         string        `json:"id"`
	Slug       string        `json:"slug"`
	Title      string        `json:"title"`
	StartDate  string        `json:"startDate"`
	EndDate    string        `json:"endDate"`
	Volume     *optionalFee  `json:"volume"`
	VolumeNum  *optionalFee  `json:"volumeNum"`
	Volume24hr *optionalFee  `json:"volume24hr"`
	Markets    []gammaMarket `json:"markets"`
}

func optionalFeeFloat(o *optionalFee) float64 {
	s := optionalFeeString(o)
	if s == nil {
		return 0
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(*s), 64)
	if err != nil || v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

func fetchGammaEvents(ctx context.Context, httpProxy string, seriesID int) ([]gammaEvent, error) {
	const maxRetries = 3
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			logrus.WithFields(logx.Pairs("attempt", attempt+1, "series_id", seriesID, "backoff", backoff.String())).Warn("Gamma HTTP：重试前等待")
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		events, err := doFetchGammaEvents(ctx, httpProxy, seriesID)
		if err == nil {
			return events, nil
		}

		lastErr = err
		if !isRetryableError(err) {
			return nil, err
		}
		logrus.WithFields(logx.Pairs("series_id", seriesID, "attempt", attempt+1, "err", err.Error())).Warn("Gamma HTTP：可重试错误")
	}

	return nil, lastErr
}

func isRetryableError(err error) bool {
	s := err.Error()
	return strings.Contains(s, "EOF") ||
		strings.Contains(s, "timeout") ||
		strings.Contains(s, "context deadline") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "no such host") ||
		strings.Contains(s, "temporary failure")
}

func doFetchGammaEvents(ctx context.Context, httpProxy string, seriesID int) ([]gammaEvent, error) {
	var all []gammaEvent
	offset := 0
	const limit = 500
	tr := http.DefaultTransport
	if strings.TrimSpace(httpProxy) != "" {
		pu, err := url.Parse(httpProxy)
		if err != nil {
			return nil, err
		}
		tr = &http.Transport{Proxy: http.ProxyURL(pu)}
	}
	client := &http.Client{Transport: tr, Timeout: 60 * time.Second}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		q := url.Values{}
		q.Set("closed", "false")
		q.Set("limit", strconv.Itoa(limit))
		q.Set("offset", strconv.Itoa(offset))
		q.Set("series_id", strconv.Itoa(seriesID))
		u := gammaAPI + "/events?" + q.Encode()
		if offset == 0 {
			logrus.WithFields(logx.Pairs("url", u)).Debug("Gamma HTTP：请求")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		res, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if err != nil {
			return nil, err
		}
		if res.StatusCode != http.StatusOK {
			logrus.WithFields(logx.Pairs("status", res.StatusCode, "series_id", seriesID, "offset", offset, "body_prefix", truncateForLog(string(body), 240))).Warn("Gamma HTTP：非 200 响应")
			return nil, fmt.Errorf("gamma %d: %s", res.StatusCode, string(body))
		}
		var page []gammaEvent
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, err
		}
		if len(page) == 0 {
			break
		}
		all = append(all, page...)
		if len(page) < limit {
			break
		}
		offset += limit
	}
	return all, nil
}

func truncateForLog(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func parseJSONStringArray(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var arr []string
	if err := json.Unmarshal([]byte(raw), &arr); err != nil {
		return nil
	}
	return arr
}

func parseJSONFloatArray(raw string) []float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var arr []float64
	if err := json.Unmarshal([]byte(raw), &arr); err != nil {
		return nil
	}
	return arr
}

// parseJSONNumberArray unmarshals a JSON array of numbers or numeric strings (Gamma often uses strings).
func parseJSONNumberArray(raw string) []float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var elems []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &elems); err != nil {
		return nil
	}
	out := make([]float64, 0, len(elems))
	for _, el := range elems {
		var f float64
		if err := json.Unmarshal(el, &f); err == nil {
			out = append(out, f)
			continue
		}
		var s string
		if err := json.Unmarshal(el, &s); err == nil {
			if v, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
				out = append(out, v)
			}
		}
	}
	return out
}

func parseLiquidity(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}
