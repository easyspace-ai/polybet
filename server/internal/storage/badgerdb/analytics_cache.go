package badgerdb

import (
	"context"
	"strings"
	"sync"
	"time"
)

type analyticsCacheStore struct {
	mu   sync.RWMutex
	byID map[string]analyticsCacheEntry
}

var analyticsCache analyticsCacheStore

// InvalidateAnalyticsCache drops cached closed-position analytics rows.
// Pass empty accountID to clear all accounts.
func InvalidateAnalyticsCache(accountID string) {
	accountID = strings.TrimSpace(accountID)
	analyticsCache.mu.Lock()
	defer analyticsCache.mu.Unlock()
	if accountID == "" {
		analyticsCache.byID = nil
		return
	}
	if analyticsCache.byID == nil {
		return
	}
	delete(analyticsCache.byID, accountID)
}

// ListClosedPositionsForAnalyticsCached scans closed positions once per account until invalidated.
func (d *DB) ListClosedPositionsForAnalyticsCached(ctx context.Context, accountID string) ([]AnalyticsTradeRow, error) {
	accountID = strings.TrimSpace(accountID)
	analyticsCache.mu.RLock()
	if ent, ok := analyticsCache.byID[accountID]; ok {
		rows := cloneAnalyticsRows(ent.rows)
		analyticsCache.mu.RUnlock()
		return rows, nil
	}
	analyticsCache.mu.RUnlock()

	rows, err := d.ListClosedPositionsForAnalytics(ctx, accountID)
	if err != nil {
		return nil, err
	}
	analyticsCache.mu.Lock()
	if analyticsCache.byID == nil {
		analyticsCache.byID = make(map[string]analyticsCacheEntry)
	}
	analyticsCache.byID[accountID] = analyticsCacheEntry{rows: cloneAnalyticsRows(rows), builtAt: time.Now()}
	analyticsCache.mu.Unlock()
	return rows, nil
}

func cloneAnalyticsRows(rows []AnalyticsTradeRow) []AnalyticsTradeRow {
	if len(rows) == 0 {
		return nil
	}
	out := make([]AnalyticsTradeRow, len(rows))
	copy(out, rows)
	return out
}
