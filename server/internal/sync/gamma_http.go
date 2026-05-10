package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const gammaAPI = "https://gamma-api.polymarket.com"

type gammaMarket struct {
	ConditionID     string  `json:"conditionId"`
	Question        string  `json:"question"`
	ClobTokenIDs    string  `json:"clobTokenIds"`
	Outcomes        string  `json:"outcomes"`
	OutcomePrices   string  `json:"outcomePrices"`
	Active          bool    `json:"active"`
	Closed          bool    `json:"closed"`
	Liquidity       string  `json:"liquidity"`
	SportsMarketType *string `json:"sportsMarketType"`
	Line            *float64 `json:"line"`
	GameStartTime   *string `json:"gameStartTime"`
}

type gammaEvent struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	StartDate string        `json:"startDate"`
	EndDate   string        `json:"endDate"`
	Markets   []gammaMarket `json:"markets"`
}

func fetchGammaEvents(ctx context.Context, httpProxy string, seriesID int) ([]gammaEvent, error) {
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
		q := url.Values{}
		q.Set("active", "true")
		q.Set("closed", "false")
		q.Set("limit", strconv.Itoa(limit))
		q.Set("offset", strconv.Itoa(offset))
		q.Set("series_id", strconv.Itoa(seriesID))
		u := gammaAPI + "/events?" + q.Encode()
		if offset == 0 {
			slog.Debug("gamma_http_request", "url", u)
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
			slog.Warn("gamma_http_non_200", "status", res.StatusCode, "series_id", seriesID, "offset", offset, "body_prefix", truncateForLog(string(body), 240))
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
