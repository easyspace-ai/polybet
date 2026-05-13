package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// GammaSport is a single entry from https://gamma-api.polymarket.com/sports
type GammaSport struct {
	ID         int    `json:"id"`
	Sport      string `json:"sport"`      // slug, e.g. "nba"
	Image      string `json:"image"`
	Resolution string `json:"resolution"`
	Ordering   string `json:"ordering"`   // "home" | "away"
	Tags       string `json:"tags"`       // comma-separated tag ids
	SeriesID   int    `json:"series,string"` // polymarket returns "series" as string
	CreatedAt  string `json:"createdAt"`
}

// SportsCache holds the Gamma /sports list with a TTL.
type SportsCache struct {
	mu       sync.RWMutex
	items    []GammaSport
	expires  int64
	ttlSec   int64
	httpProxy string
}

// NewSportsCache creates a cache with the given TTL (default 1h).
func NewSportsCache(httpProxy string, ttl time.Duration) *SportsCache {
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &SportsCache{ttlSec: int64(ttl.Seconds()), httpProxy: httpProxy}
}

func (c *SportsCache) isExpired() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return time.Now().Unix() > c.expires
}

func (c *SportsCache) Get(ctx context.Context) ([]GammaSport, error) {
	if !c.isExpired() {
		c.mu.RLock()
		defer c.mu.RUnlock()
		return c.items, nil
	}
	return c.Refresh(ctx)
}

func (c *SportsCache) Refresh(ctx context.Context) ([]GammaSport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Double-check after acquiring write lock
	if time.Now().Unix() <= c.expires {
		return c.items, nil
	}
	items, err := fetchGammaSports(ctx, c.httpProxy)
	if err != nil {
		// If we have stale data, return it as fallback
		if len(c.items) > 0 {
			slog.Warn("sports_cache_refresh_failed_fallback", "err", err.Error())
			return c.items, nil
		}
		return nil, err
	}
	c.items = items
	c.expires = time.Now().Unix() + c.ttlSec
	slog.Info("sports_cache_refreshed", "count", len(items))
	return items, nil
}

func fetchGammaSports(ctx context.Context, httpProxy string) ([]GammaSport, error) {
	tr := http.DefaultTransport
	if strings.TrimSpace(httpProxy) != "" {
		pu, err := url.Parse(httpProxy)
		if err != nil {
			return nil, err
		}
		tr = &http.Transport{Proxy: http.ProxyURL(pu)}
	}
	client := &http.Client{Transport: tr, Timeout: 30 * time.Second}
	u := gammaAPI + "/sports"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gamma sports %d: %s", res.StatusCode, string(body))
	}
	var raw []struct {
		ID         int    `json:"id"`
		Sport      string `json:"sport"`
		Image      string `json:"image"`
		Resolution string `json:"resolution"`
		Ordering   string `json:"ordering"`
		Tags       string `json:"tags"`
		Series     string `json:"series"`
		CreatedAt  string `json:"createdAt"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	out := make([]GammaSport, 0, len(raw))
	for _, r := range raw {
		if strings.TrimSpace(r.Sport) == "" {
			continue
		}
		var seriesID int
		if v, err := fmt.Sscanf(r.Series, "%d", &seriesID); err != nil || v != 1 {
			// Try trimming whitespace
			fmt.Sscanf(strings.TrimSpace(r.Series), "%d", &seriesID)
		}
		if seriesID <= 0 {
			continue
		}
		out = append(out, GammaSport{
			ID:         r.ID,
			Sport:      strings.ToLower(strings.TrimSpace(r.Sport)),
			Image:      r.Image,
			Resolution: r.Resolution,
			Ordering:   strings.ToLower(strings.TrimSpace(r.Ordering)),
			Tags:       r.Tags,
			SeriesID:   seriesID,
			CreatedAt:  r.CreatedAt,
		})
	}
	return out, nil
}
