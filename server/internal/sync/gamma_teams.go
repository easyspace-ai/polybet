package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/easyspace-ai/polybet/internal/logx"
)

// GammaTeam is one row from https://gamma-api.polymarket.com/teams
type GammaTeam struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Abbreviation string `json:"abbreviation"`
	League       string `json:"league"`
}

// TeamsCache caches Gamma /teams per league with TTL.
type TeamsCache struct {
	mu        sync.RWMutex
	byLeague  map[string]teamsCacheEntry
	ttlSec    int64
	httpProxy string
}

type teamsCacheEntry struct {
	items   []GammaTeam
	expires int64
}

// NewTeamsCache creates a per-league teams cache.
func NewTeamsCache(httpProxy string, ttl time.Duration) *TeamsCache {
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &TeamsCache{
		byLeague:  map[string]teamsCacheEntry{},
		ttlSec:    int64(ttl.Seconds()),
		httpProxy: httpProxy,
	}
}

func (c *TeamsCache) Get(ctx context.Context, league string) ([]GammaTeam, error) {
	league = strings.ToLower(strings.TrimSpace(league))
	if league == "" {
		return nil, fmt.Errorf("league required")
	}
	c.mu.RLock()
	ent, ok := c.byLeague[league]
	c.mu.RUnlock()
	if ok && time.Now().Unix() <= ent.expires {
		return ent.items, nil
	}
	return c.Refresh(ctx, league)
}

func (c *TeamsCache) Refresh(ctx context.Context, league string) ([]GammaTeam, error) {
	league = strings.ToLower(strings.TrimSpace(league))
	c.mu.Lock()
	defer c.mu.Unlock()
	if ent, ok := c.byLeague[league]; ok && time.Now().Unix() <= ent.expires {
		return ent.items, nil
	}
	items, err := fetchGammaTeams(ctx, c.httpProxy, league)
	if err != nil {
		if ent, ok := c.byLeague[league]; ok && len(ent.items) > 0 {
			logrus.WithFields(logx.Pairs("league", league, "err", err.Error())).Warn("球队缓存：刷新失败，使用旧数据")
			return ent.items, nil
		}
		return nil, err
	}
	c.byLeague[league] = teamsCacheEntry{items: items, expires: time.Now().Unix() + c.ttlSec}
	logrus.WithFields(logx.Pairs("league", league, "count", len(items))).Info("球队缓存：已刷新 Gamma /teams")
	return items, nil
}

func fetchGammaTeams(ctx context.Context, httpProxy, league string) ([]GammaTeam, error) {
	tr := http.DefaultTransport
	if strings.TrimSpace(httpProxy) != "" {
		pu, err := url.Parse(httpProxy)
		if err != nil {
			return nil, err
		}
		tr = &http.Transport{Proxy: http.ProxyURL(pu)}
	}
	client := &http.Client{Transport: tr, Timeout: 30 * time.Second}
	q := url.Values{}
	q.Set("league", league)
	u := gammaAPI + "/teams?" + q.Encode()
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
		return nil, fmt.Errorf("gamma teams %d: %s", res.StatusCode, string(body))
	}
	var raw []GammaTeam
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	out := make([]GammaTeam, 0, len(raw))
	for _, t := range raw {
		if strings.TrimSpace(t.Name) == "" {
			continue
		}
		t.League = strings.ToLower(strings.TrimSpace(league))
		t.Name = strings.TrimSpace(t.Name)
		t.Abbreviation = strings.TrimSpace(t.Abbreviation)
		out = append(out, t)
	}
	return out, nil
}
