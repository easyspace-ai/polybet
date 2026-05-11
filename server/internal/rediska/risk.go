package rediska

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const riskCacheTTL = 1 * time.Hour
const riskPositionsKey = "risk:positions"

type RiskMeta struct {
	UserWsConnected         bool    `json:"userWsConnected"`
	UserWsConnecting        bool    `json:"userWsConnecting"`
	OutboundProxyConfigured bool    `json:"outboundProxyConfigured"`
	MinOpenRiskShares       float64 `json:"minOpenRiskShares"`
}

type RiskCache struct {
	cache *Cache
	log   *slog.Logger
	mu    sync.Mutex
}

func NewRiskCache(db *Cache, log *slog.Logger) *RiskCache {
	if log == nil {
		log = slog.Default()
	}
	return &RiskCache{
		cache: db,
		log:   log,
	}
}

type riskCacheData struct {
	Positions []map[string]any `json:"positions"`
	Meta       RiskMeta         `json:"meta"`
}

type RiskFetchResult struct {
	Positions []map[string]any
	Meta      RiskMeta
}

func (r *RiskCache) Get(ctx context.Context) ([]map[string]any, RiskMeta, bool, error) {
	if r == nil || r.cache == nil {
		return nil, RiskMeta{}, false, nil
	}
	var data riskCacheData
	found, err := r.cache.Get(ctx, riskPositionsKey, &data)
	if err != nil && err.Error() != "key not found" {
		r.log.Warn("risk_cache_get", "err", err)
	}
	if found && err == nil {
		return data.Positions, data.Meta, true, nil
	}
	return nil, RiskMeta{}, false, err
}

func (r *RiskCache) Invalidate(ctx context.Context) {
	if r == nil || r.cache == nil {
		return
	}
	if err := r.cache.Delete(ctx, riskPositionsKey); err != nil {
		r.log.Warn("risk_cache_invalidate", "err", err)
	} else {
		r.log.Info("risk_cache_invalidated")
	}
}

func (r *RiskCache) Set(ctx context.Context, result RiskFetchResult) error {
	if r == nil || r.cache == nil {
		return nil
	}
	return r.cache.Set(ctx, riskPositionsKey, riskCacheData{
		Positions: result.Positions,
		Meta:      result.Meta,
	}, riskCacheTTL)
}

func (r *RiskCache) RefreshAsync(ctx context.Context, fetch func() (RiskFetchResult, error)) {
	if r == nil || r.cache == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	go func() {
		result, err := fetch()
		if err != nil {
			r.log.Warn("risk_cache_refresh", "err", err)
			return
		}
		if err := r.cache.Set(context.Background(), riskPositionsKey, riskCacheData{
			Positions: result.Positions,
			Meta:      result.Meta,
		}, riskCacheTTL); err != nil {
			r.log.Warn("risk_cache_set", "err", err)
		}
		r.log.Info("risk_cache_refreshed", "count", len(result.Positions))
	}()
}

func (r *RiskCache) GetWithRefresh(ctx context.Context, fetch func() (RiskFetchResult, error)) ([]map[string]any, RiskMeta, bool, error) {
	positions, meta, found, err := r.Get(ctx)
	if found && err == nil {
		r.RefreshAsync(ctx, fetch)
		return positions, meta, true, nil
	}

	result, err := fetch()
	if err != nil {
		return nil, RiskMeta{}, false, err
	}
	if r.cache != nil {
		_ = r.cache.Set(ctx, riskPositionsKey, riskCacheData{
			Positions: result.Positions,
			Meta:      result.Meta,
		}, riskCacheTTL)
	}
	r.RefreshAsync(ctx, fetch)
	return result.Positions, result.Meta, false, nil
}