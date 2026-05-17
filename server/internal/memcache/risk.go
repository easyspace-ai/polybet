package memcache

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/easyspace-ai/polybet/internal/logx"
)

const riskCacheTTL = 1 * time.Hour

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

type riskCacheData struct {
	Positions []map[string]any `json:"positions"`
	Meta      RiskMeta         `json:"meta"`
	ExpiresAt time.Time        `json:"-"`
}

// RiskCache holds a single snapshot of enriched risk positions in memory.
// Single-writer: RefreshAsync serializes background writes with refreshMu;
// readers use dataMu RWMutex.
type RiskCache struct {
	log *logrus.Logger

	dataMu sync.RWMutex
	data   *riskCacheData

	refreshMu sync.Mutex
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

func (r *RiskCache) RefreshAsync(ctx context.Context, fetch func() (RiskFetchResult, error)) {
	if r == nil {
		return
	}
	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()

	go func() {
		result, err := fetch()
		if err != nil {
			r.log.WithFields(logx.Pairs("err", err)).Warn("风控缓存：后台刷新失败")
			return
		}
		if err := r.Set(context.Background(), result); err != nil {
			r.log.WithFields(logx.Pairs("err", err)).Warn("风控缓存：写入失败")
		}
		r.log.WithFields(logx.Pairs("count", len(result.Positions))).Info("风控缓存：已刷新")
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
	_ = r.Set(ctx, result)
	r.RefreshAsync(ctx, fetch)
	return result.Positions, result.Meta, false, nil
}
