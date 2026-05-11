package rediska

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/easyspace-ai/polybet/internal/config"
	"github.com/easyspace-ai/polybet/internal/service/balancesvc"
	"github.com/easyspace-ai/polybet/internal/store"
)

const balanceCacheTTL = 10 * time.Second
const balanceKey = "balance:summary"

type BalanceCache struct {
	cache *Cache
	st    *store.Store
	cfg   *config.Config
	log   *slog.Logger
	mu    sync.Mutex
}

func NewBalanceCache(db *Cache, st *store.Store, cfg *config.Config, log *slog.Logger) *BalanceCache {
	if log == nil {
		log = slog.Default()
	}
	return &BalanceCache{
		cache: db,
		st:    st,
		cfg:   cfg,
		log:   log,
	}
}

func (b *BalanceCache) Get(ctx context.Context) (*balancesvc.Summary, bool, error) {
	if b == nil || b.cache == nil {
		return nil, false, nil
	}
	var summary balancesvc.Summary
	found, err := b.cache.Get(ctx, balanceKey, &summary)
	if err != nil && err.Error() != "key not found" {
		b.log.Warn("balance_cache_get", "err", err)
	}
	return &summary, found, nil
}

func (b *BalanceCache) RefreshAsync(ctx context.Context) {
	if b == nil || b.cache == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	go func() {
		summary, err := balancesvc.Fetch(context.Background(), b.cfg, b.st)
		if err != nil {
			b.log.Warn("balance_cache_refresh", "err", err)
			return
		}
		if err := b.cache.Set(context.Background(), balanceKey, summary, balanceCacheTTL); err != nil {
			b.log.Warn("balance_cache_set", "err", err)
		}
		b.log.Info("balance_cache_refreshed")
	}()
}

func (b *BalanceCache) GetWithRefresh(ctx context.Context) (*balancesvc.Summary, bool, error) {
	if b == nil || b.cache == nil {
		summary, err := balancesvc.Fetch(ctx, b.cfg, b.st)
		if err != nil {
			return nil, false, err
		}
		return summary, false, nil
	}
	summary, found, err := b.Get(ctx)
	if found && err == nil {
		b.RefreshAsync(ctx)
		return summary, true, nil
	}

	summary, err = balancesvc.Fetch(ctx, b.cfg, b.st)
	if err != nil {
		return nil, false, err
	}
	_ = b.cache.Set(ctx, balanceKey, summary, balanceCacheTTL)
	b.RefreshAsync(ctx)
	return summary, false, nil
}