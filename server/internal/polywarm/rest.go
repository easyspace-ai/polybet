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
	"github.com/easyspace-ai/polybet/internal/polyexec"
	"github.com/easyspace-ai/polybet/internal/store"
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
// tokenID may be hex or decimal; cache is keyed by normalized 0x + 64 hex.
func RefreshFromREST(ctx context.Context, clobBaseURL, httpProxy, tokenID string, cache *bookcache.Cache) error {
	cacheKey := store.NormalizeRiskCLOBTokenID(tokenID)
	if cacheKey == "" {
		return fmt.Errorf("empty token id")
	}
	apiID := polyexec.CLOBAssetIDForAPI(tokenID)
	if apiID == "" {
		apiID = strings.TrimSpace(tokenID)
	}
	u := strings.TrimRight(clobBaseURL, "/") + "/book?token_id=" + url.QueryEscape(apiID)
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
	cache.ReplaceBook(cacheKey, bids, asks, ts)
	return nil
}

// BestBidCents returns best bid in cents from REST (fallback when WS empty).
func BestBidCents(ctx context.Context, clobBaseURL, httpProxy, tokenID string) (float64, error) {
	apiID := polyexec.CLOBAssetIDForAPI(tokenID)
	if apiID == "" {
		apiID = strings.TrimSpace(tokenID)
	}
	u := strings.TrimRight(clobBaseURL, "/") + "/book?token_id=" + url.QueryEscape(apiID)
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
	return polyexec.CentsFromPrice01(best), nil
}

// BestBidAskCents returns best bid and best ask in cents (probability × 100) from one /book fetch.
// Best bid = highest buy price; best ask = lowest sell price.
func BestBidAskCents(ctx context.Context, clobBaseURL, httpProxy, tokenID string) (bidCents, askCents float64, err error) {
	apiID := polyexec.CLOBAssetIDForAPI(tokenID)
	if apiID == "" {
		apiID = strings.TrimSpace(tokenID)
	}
	u := strings.TrimRight(clobBaseURL, "/") + "/book?token_id=" + url.QueryEscape(apiID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, 0, err
	}
	tr := http.DefaultTransport
	if strings.TrimSpace(httpProxy) != "" {
		pu, _ := url.Parse(httpProxy)
		tr = &http.Transport{Proxy: http.ProxyURL(pu)}
	}
	client := &http.Client{Transport: tr, Timeout: 12 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("book %d", res.StatusCode)
	}
	var book clobBook
	if err := json.NewDecoder(res.Body).Decode(&book); err != nil {
		return 0, 0, err
	}
	var bestBid, bestAsk float64
	for _, b := range book.Bids {
		p, err := strconv.ParseFloat(strings.TrimSpace(b.Price), 64)
		if err != nil || p <= 0 {
			continue
		}
		if bestBid == 0 || p > bestBid {
			bestBid = p
		}
	}
	for _, a := range book.Asks {
		p, err := strconv.ParseFloat(strings.TrimSpace(a.Price), 64)
		if err != nil || p <= 0 {
			continue
		}
		if bestAsk == 0 || p < bestAsk {
			bestAsk = p
		}
	}
	if bestBid <= 0 && bestAsk <= 0 {
		return 0, 0, fmt.Errorf("empty_book")
	}
	return polyexec.CentsFromPrice01(bestBid), polyexec.CentsFromPrice01(bestAsk), nil
}

// BookJSONHTTPOK reports whether CLOB GET /book returned HTTP 200 and the body decoded as a book payload.
// It returns true even when bids and asks are empty (token is recognized by the book endpoint).
func BookJSONHTTPOK(ctx context.Context, clobBaseURL, httpProxy, tokenID string) bool {
	tid := strings.TrimSpace(tokenID)
	if tid == "" {
		return false
	}
	apiID := polyexec.CLOBAssetIDForAPI(tid)
	if apiID == "" {
		apiID = tid
	}
	u := strings.TrimRight(clobBaseURL, "/") + "/book?token_id=" + url.QueryEscape(apiID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false
	}
	tr := http.DefaultTransport
	if strings.TrimSpace(httpProxy) != "" {
		pu, _ := url.Parse(httpProxy)
		tr = &http.Transport{Proxy: http.ProxyURL(pu)}
	}
	client := &http.Client{Transport: tr, Timeout: 4 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return false
	}
	var book clobBook
	if err := json.NewDecoder(res.Body).Decode(&book); err != nil {
		return false
	}
	return true
}
