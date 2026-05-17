package gammaclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const gammaAPIHost = "https://gamma-api.polymarket.com"

// gammaAPIBase is the Gamma REST root (mutable in tests).
var gammaAPIBase = gammaAPIHost

// TokenMarketDisplay is a small slice of Gamma market JSON for operator UI.
type TokenMarketDisplay struct {
	TokenID     string
	Question    string
	Slug        string // market-level slug when present
	EventSlug   string // first linked event slug
	ConditionID string
	Image       string // Gamma `image` (market or nested event)
	Icon        string // Gamma `icon` (often same URL as Image)
	Category    string // e.g. sports league bucket when present
	Active      bool
	Closed      bool
}

type gammaMarketJSON struct {
	Question      string          `json:"question"`
	Slug          string          `json:"slug"`
	ConditionID   string          `json:"conditionId"`
	Image         string          `json:"image"`
	Icon          string          `json:"icon"`
	Category      string          `json:"category"`
	Active        bool            `json:"active"`
	Closed        bool            `json:"closed"`
	ClobTokenIDs  json.RawMessage `json:"clobTokenIds"`
	Events        json.RawMessage `json:"events"`
}

type gammaEventMini struct {
	Slug  string `json:"slug"`
	Image string `json:"image"`
	Icon  string `json:"icon"`
}

func httpClient(proxy string) *http.Client {
	tr := &http.Transport{}
	if p := strings.TrimSpace(proxy); p != "" {
		if pu, err := url.Parse(p); err == nil {
			tr.Proxy = http.ProxyURL(pu)
		}
	}
	return &http.Client{Transport: tr, Timeout: 15 * time.Second}
}

func parseStringArray(raw json.RawMessage) []string {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil
	}
	return trimStringSlice(arr)
}

// parseClobTokenIDsField parses Gamma "clobTokenIds" which may be a JSON array
// or a JSON string containing a serialized JSON array (common on older payloads).
func parseClobTokenIDsField(raw json.RawMessage) []string {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil && len(arr) > 0 {
		return trimStringSlice(arr)
	}
	var inner string
	if err := json.Unmarshal(raw, &inner); err == nil && strings.TrimSpace(inner) != "" {
		if err2 := json.Unmarshal([]byte(inner), &arr); err2 == nil {
			return trimStringSlice(arr)
		}
	}
	return nil
}

func trimStringSlice(arr []string) []string {
	out := make([]string, 0, len(arr))
	for _, s := range arr {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func firstEventSlug(events json.RawMessage) string {
	events = json.RawMessage(strings.TrimSpace(string(events)))
	if len(events) == 0 || string(events) == "null" {
		return ""
	}
	var evs []gammaEventMini
	if err := json.Unmarshal(events, &evs); err == nil {
		for _, e := range evs {
			if s := strings.TrimSpace(e.Slug); s != "" {
				return s
			}
		}
		return ""
	}
	var one gammaEventMini
	if err := json.Unmarshal(events, &one); err == nil {
		return strings.TrimSpace(one.Slug)
	}
	return ""
}

// firstEventImageIcon returns image/icon URLs from the first linked event that carries either.
func firstEventImageIcon(events json.RawMessage) (image, icon string) {
	events = json.RawMessage(strings.TrimSpace(string(events)))
	if len(events) == 0 || string(events) == "null" {
		return "", ""
	}
	var evs []gammaEventMini
	if err := json.Unmarshal(events, &evs); err != nil {
		return "", ""
	}
	for _, e := range evs {
		img, ic := strings.TrimSpace(e.Image), strings.TrimSpace(e.Icon)
		if img != "" || ic != "" {
			return img, ic
		}
	}
	return "", ""
}

func marketOrEventURL(market, fromEvent string) string {
	if s := strings.TrimSpace(market); s != "" {
		return s
	}
	return strings.TrimSpace(fromEvent)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func unmarshalMarketsArray(body []byte) ([]gammaMarketJSON, error) {
	var markets []gammaMarketJSON
	if err := json.Unmarshal(body, &markets); err == nil {
		return markets, nil
	}
	var wrap struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &wrap); err == nil && len(strings.TrimSpace(string(wrap.Data))) > 2 {
		var inner []gammaMarketJSON
		if err2 := json.Unmarshal(wrap.Data, &inner); err2 == nil {
			return inner, nil
		}
	}
	var wrap2 struct {
		Markets []gammaMarketJSON `json:"markets"`
	}
	if err := json.Unmarshal(body, &wrap2); err == nil {
		return wrap2.Markets, nil
	}
	return nil, fmt.Errorf("gamma markets: unknown JSON envelope")
}

func doMarketsGET(ctx context.Context, client *http.Client, query url.Values) ([]gammaMarketJSON, error) {
	u := gammaAPIBase + "/markets?" + query.Encode()
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
		return nil, fmt.Errorf("gamma markets %d: %s", res.StatusCode, truncate(string(body), 200))
	}
	return unmarshalMarketsArray(body)
}

func fillOutFromMarkets(markets []gammaMarketJSON, want map[string]struct{}, out map[string]TokenMarketDisplay) {
	for _, m := range markets {
		toks := parseClobTokenIDsField(m.ClobTokenIDs)
		if len(toks) == 0 {
			toks = parseStringArray(m.ClobTokenIDs)
		}
		evSlug := firstEventSlug(m.Events)
		evImg, evIc := firstEventImageIcon(m.Events)
		img := marketOrEventURL(m.Image, evImg)
		icn := marketOrEventURL(m.Icon, evIc)
		if icn == "" {
			icn = img
		}
		if img == "" {
			img = icn
		}
		for _, tid := range toks {
			if _, ok := want[tid]; !ok {
				continue
			}
			out[tid] = TokenMarketDisplay{
				TokenID:     tid,
				Question:    strings.TrimSpace(m.Question),
				Slug:        strings.TrimSpace(m.Slug),
				EventSlug:   evSlug,
				ConditionID: strings.TrimSpace(m.ConditionID),
				Image:       img,
				Icon:        icn,
				Category:    strings.TrimSpace(strings.ToLower(m.Category)),
				Active:      m.Active,
				Closed:      m.Closed,
			}
		}
	}
}

// FetchMarketsByCLOBTokenIDs calls Gamma GET /markets?clob_token_ids=… for one or more CLOB token ids.
// Returns a map keyed by token id (best-effort; unknown tokens are omitted).
func FetchMarketsByCLOBTokenIDs(ctx context.Context, httpProxy string, tokenIDs []string) (map[string]TokenMarketDisplay, error) {
	out := make(map[string]TokenMarketDisplay)
	want := make(map[string]struct{})
	for _, id := range tokenIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			want[id] = struct{}{}
		}
	}
	if len(want) == 0 {
		return out, nil
	}
	ids := make([]string, 0, len(want))
	for id := range want {
		ids = append(ids, id)
	}
	const maxPerReq = 20
	client := httpClient(httpProxy)

	for i := 0; i < len(ids); i += maxPerReq {
		end := i + maxPerReq
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[i:end]
		q := url.Values{}
		for _, id := range chunk {
			q.Add("clob_token_ids", id)
		}
		q.Set("limit", fmt.Sprintf("%d", len(chunk)+8))
		markets, err := doMarketsGET(ctx, client, q)
		if err != nil {
			return out, err
		}
		fillOutFromMarkets(markets, want, out)
	}

	// Per-token retry: batch responses sometimes omit markets; weather/non-sport tokens hit this more often.
	for _, id := range ids {
		if _, ok := out[id]; ok && strings.TrimSpace(out[id].Question) != "" {
			continue
		}
		q := url.Values{}
		q.Set("clob_token_ids", id)
		q.Set("limit", "5")
		markets, err := doMarketsGET(ctx, client, q)
		if err != nil {
			continue
		}
		fillOutFromMarkets(markets, want, out)
	}

	return out, nil
}

// OppositeCLOBTokenID returns the other CLOB outcome token in a binary market (exactly two clobTokenIds).
// heldTokenID must match one entry; otherwise an error is returned (including when the market is not binary).
func OppositeCLOBTokenID(ctx context.Context, httpProxy, heldTokenID string) (string, error) {
	held := strings.TrimSpace(heldTokenID)
	if held == "" {
		return "", fmt.Errorf("empty held token id")
	}
	q := url.Values{}
	q.Set("clob_token_ids", held)
	q.Set("limit", "8")
	markets, err := doMarketsGET(ctx, httpClient(httpProxy), q)
	if err != nil {
		return "", err
	}
	for _, m := range markets {
		toks := parseClobTokenIDsField(m.ClobTokenIDs)
		if len(toks) == 0 {
			toks = parseStringArray(m.ClobTokenIDs)
		}
		if len(toks) != 2 {
			continue
		}
		var heldIdx = -1
		for i, t := range toks {
			if strings.EqualFold(strings.TrimSpace(t), held) {
				heldIdx = i
				break
			}
		}
		if heldIdx < 0 {
			continue
		}
		other := toks[1-heldIdx]
		if strings.TrimSpace(other) == "" {
			return "", fmt.Errorf("hedge_opposite_token: empty sibling in binary market")
		}
		return strings.TrimSpace(other), nil
	}
	return "", fmt.Errorf("hedge_opposite_token: need binary market (2 clob tokens) containing %s", held)
}
