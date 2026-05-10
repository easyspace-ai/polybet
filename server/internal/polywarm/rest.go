package polywarm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/easyspace-ai/polybet/internal/bookcache"
)

type clobBook struct {
	Bids []struct {
		Price string `json:"price"`
		Size  string `json:"size"`
	} `json:"bids"`
	Asks []struct {
		Price string `json:"price"`
		Size  string `json:"size"`
	} `json:"asks"`
}

// RefreshFromREST pulls /book and replaces in-memory cache (matches Node warmPolyBook).
func RefreshFromREST(ctx context.Context, clobBaseURL, httpProxy, tokenID string, cache *bookcache.Cache) error {
	u := strings.TrimRight(clobBaseURL, "/") + "/book?token_id=" + url.QueryEscape(tokenID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	tr := http.DefaultTransport
	if strings.TrimSpace(httpProxy) != "" {
		pu, err := url.Parse(httpProxy)
		if err != nil {
			return err
		}
		tr = &http.Transport{Proxy: http.ProxyURL(pu)}
	}
	client := &http.Client{Transport: tr, Timeout: 15 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("book http %d: %s", res.StatusCode, string(b))
	}
	var book clobBook
	if err := json.NewDecoder(res.Body).Decode(&book); err != nil {
		return err
	}
	ts := time.Now().UnixMilli()
	bids := make([]struct{ Price, Size string }, len(book.Bids))
	for i := range book.Bids {
		bids[i] = struct{ Price, Size string }{Price: book.Bids[i].Price, Size: book.Bids[i].Size}
	}
	asks := make([]struct{ Price, Size string }, len(book.Asks))
	for i := range book.Asks {
		asks[i] = struct{ Price, Size string }{Price: book.Asks[i].Price, Size: book.Asks[i].Size}
	}
	cache.ReplaceBook(tokenID, bids, asks, ts)
	return nil
}

// BestBidCents returns best bid in cents from REST (fallback when WS empty).
func BestBidCents(ctx context.Context, clobBaseURL, httpProxy, tokenID string) (float64, error) {
	u := strings.TrimRight(clobBaseURL, "/") + "/book?token_id=" + url.QueryEscape(tokenID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	tr := http.DefaultTransport
	if strings.TrimSpace(httpProxy) != "" {
		pu, _ := url.Parse(httpProxy)
		tr = &http.Transport{Proxy: http.ProxyURL(pu)}
	}
	client := &http.Client{Transport: tr, Timeout: 12 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("book %d", res.StatusCode)
	}
	var book clobBook
	if err := json.NewDecoder(res.Body).Decode(&book); err != nil {
		return 0, err
	}
	var best float64
	for _, b := range book.Bids {
		p, err := strconv.ParseFloat(strings.TrimSpace(b.Price), 64)
		if err != nil || p <= 0 {
			continue
		}
		if best == 0 || p > best {
			best = p
		}
	}
	if best <= 0 {
		return 0, fmt.Errorf("no_bid")
	}
	return best * 100, nil
}
