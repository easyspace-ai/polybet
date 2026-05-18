package memcache

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/sync/singleflight"

	"github.com/easyspace-ai/polybet/internal/logx"
)

const (
	riskCacheTTL          = 1 * time.Hour
	riskBackgroundRefresh = 90 * time.Second
)

// RiskMeta is dashboard-facing metadata bundled with cached positions.
type RiskMeta struct {
	UserWsConnected         bool    `json:"userWsConnected"`
	UserWsConnecting        bool    `json:"userWsConnecting"`
	OutboundProxyConfigured bool    `json:"outboundProxyConfigured"`
	MinOpenRiskShares       float64 `json:"minOpenRiskShares"`
	RiskCloseExecutionMode  string  `json:"riskCloseExecutionMode"`
	RiskCloseFakWorstPrice  float64 `json:"riskCloseFakWorstPrice"`
	RiskHedgeBuySizing      string  `json:"riskHedgeBuySizing"`
}

// RiskFetchResult is the payload produced by risksvc and stored in-process.
type RiskFetchResult struct {
	Positions []map[string]any
	Meta      RiskMeta
}

// RiskFetchFunc loads a fresh snapshot on a background worker goroutine.
type RiskFetchFunc func(ctx context.Context) (RiskFetchResult, error)

type riskCacheData struct {
	Positions []map[string]any `json:"positions"`
	Meta      RiskMeta         `json:"meta"`
	ExpiresAt time.Time        `json:"-"`
}

// RiskCache holds a single snapshot of enriched risk positions in memory.
// HTTP handlers must call Snapshot() only — never block on enrichment.
type RiskCache struct {
	log *logrus.Logger

	dataMu sync.RWMutex
	data   *riskCacheData

	refreshMu sync.Mutex
	sf        singleflight.Group
}

func NewRiskCache(log *logrus.Logger) *RiskCache {
	if log == nil {
		log = logrus.StandardLogger()
	}
	return &RiskCache{log: log}
}

func (r *RiskCache) Get(ctx context.Context) ([]map[string]any, RiskMeta, bool, error) {
	if r == nil {
		return nil, RiskMeta{}, false, nil
	}
	r.dataMu.RLock()
	defer r.dataMu.RUnlock()
	if r.data == nil || time.Now().After(r.data.ExpiresAt) {
		return nil, RiskMeta{}, false, nil
	}
	return r.data.Positions, r.data.Meta, true, nil
}

// Snapshot returns the in-memory copy instantly. The bool is false when empty/expired.
func (r *RiskCache) Snapshot() ([]map[string]any, RiskMeta, bool) {
	positions, meta, found, _ := r.Get(context.Background())
	return positions, meta, found
}

func (r *RiskCache) Invalidate(ctx context.Context) {
	if r == nil {
		return
	}
	r.dataMu.Lock()
	r.data = nil
	r.dataMu.Unlock()
	r.log.Info("风控缓存：已失效")
}

func (r *RiskCache) Set(ctx context.Context, result RiskFetchResult) error {
	if r == nil {
		return nil
	}
	r.dataMu.Lock()
	r.data = &riskCacheData{
		Positions: result.Positions,
		Meta:      result.Meta,
		ExpiresAt: time.Now().Add(riskCacheTTL),
	}
	r.dataMu.Unlock()
	return nil
}

// RequestBackgroundRefresh schedules enrichment on a worker goroutine.
// Never blocks the caller; duplicate requests are deduplicated via singleflight.
func (r *RiskCache) RequestBackgroundRefresh(fetch RiskFetchFunc) {
	if r == nil || fetch == nil {
		return
	}
	go func() {
		_, err, _ := r.sf.Do("refresh", func() (any, error) {
			ctx, cancel := context.WithTimeout(context.Background(), riskBackgroundRefresh)
			defer cancel()
			result, err := fetch(ctx)
			if err != nil {
				return nil, err
			}
			if err := r.Set(context.Background(), result); err != nil {
				return nil, err
			}
			r.log.WithFields(logx.Pairs("count", len(result.Positions))).Info("风控缓存：已刷新")
			return result, nil
		})
		if err != nil {
			r.log.WithFields(logx.Pairs("err", err)).Warn("风控缓存：后台刷新失败")
		}
	}()
}

// RefreshAsync is an alias for RequestBackgroundRefresh.
func (r *RiskCache) RefreshAsync(fetch RiskFetchFunc) {
	r.RequestBackgroundRefresh(fetch)
}

// PatchPositionFields merges fields into a cached position row by id.
// Used after PATCH /api/risk/positions/:id so GET/WS snapshots stay consistent
// until the next background enrich rebuild.
func (r *RiskCache) PatchPositionFields(id string, fields map[string]any) bool {
	if r == nil || id == "" || len(fields) == 0 {
		return false
	}
	r.dataMu.Lock()
	defer r.dataMu.Unlock()
	if r.data == nil {
		return false
	}
	for i, row := range r.data.Positions {
		rid, _ := row["id"].(string)
		if rid != id {
			continue
		}
		patched := make(map[string]any, len(row)+len(fields))
		for k, v := range row {
			patched[k] = v
		}
		for k, v := range fields {
			patched[k] = v
		}
		r.data.Positions[i] = patched
		return true
	}
	return false
}
