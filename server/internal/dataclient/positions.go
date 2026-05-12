package dataclient

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

const dataAPIBase = "https://data-api.polymarket.com"

func httpClient(proxy string) *http.Client {
	tr := &http.Transport{}
	if p := strings.TrimSpace(proxy); p != "" {
		if pu, err := url.Parse(p); err == nil {
			tr.Proxy = http.ProxyURL(pu)
		}
	}
	return &http.Client{Transport: tr, Timeout: 25 * time.Second}
}

type positionRow struct {
	Asset string  `json:"asset"`
	Size  float64 `json:"size"`
}

// FetchPositivePositionSizes returns CLOB asset id (token id) → size for positions the Data API
// reports with positive size (same family of data as the Polymarket website portfolio).
// Missing keys mean the user has no active position for that asset.
func FetchPositivePositionSizes(ctx context.Context, httpProxy, userAddress string) (map[string]float64, error) {
	addr := strings.TrimSpace(strings.ToLower(userAddress))
	if !strings.HasPrefix(addr, "0x") || len(addr) != 42 {
		return nil, fmt.Errorf("dataclient: invalid user address")
	}
	if addr == "0x0000000000000000000000000000000000000000" {
		return nil, fmt.Errorf("dataclient: zero user address")
	}

	client := httpClient(httpProxy)
	out := make(map[string]float64)
	const page = 500
	for offset := 0; offset <= 10000; offset += page {
		select {
		case <-ctx.Done():
			return out, ctx.Err()
		default:
		}
		q := url.Values{}
		q.Set("user", addr)
		q.Set("limit", fmt.Sprintf("%d", page))
		q.Set("offset", fmt.Sprintf("%d", offset))
		q.Set("sizeThreshold", "0")

		u := dataAPIBase + "/positions?" + q.Encode()
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
			return nil, fmt.Errorf("dataclient positions %d: %s", res.StatusCode, truncate(string(body), 200))
		}
		var rows []positionRow
		if err := json.Unmarshal(body, &rows); err != nil {
			return nil, fmt.Errorf("dataclient positions json: %w", err)
		}
		if len(rows) == 0 {
			break
		}
		for _, r := range rows {
			a := strings.TrimSpace(r.Asset)
			if a == "" || r.Size <= 0 {
				continue
			}
			out[a] += r.Size
		}
		if len(rows) < page {
			break
		}
	}
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
