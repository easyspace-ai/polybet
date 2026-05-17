package memcache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/easyspace-ai/polybet/internal/config"
	"github.com/easyspace-ai/polybet/internal/logx"
	"github.com/easyspace-ai/polybet/internal/service/balancesvc"
	"github.com/easyspace-ai/polybet/internal/storage"
)

const balanceCacheTTL = 1 * time.Hour

// BalanceCache stores the last balancesvc.Summary in process memory with TTL.
// UpdatedAt is set whenever a non-nil summary is stored (for observability / API).
type BalanceCache struct {
	st  *storage.Backend
	cfg *config.Config
	log *logrus.Logger

	mu        sync.RWMutex
	summary   *balancesvc.Summary
	expiresAt time.Time
	updatedAt time.Time

	// lastBalanceBroadcastDigest is cleared on Invalidate so the next balance_update still fires.
	lastBalanceBroadcastDigest [sha256.Size]byte

	// lastAcctUSD keeps last successful per-account USD across invalidations / nil fetches.
	lastAcctUSD map[string]float64

	refreshMu sync.Mutex
}

func NewBalanceCache(st *storage.Backend, cfg *config.Config, log *logrus.Logger) *BalanceCache {
	if log == nil {
		log = logrus.StandardLogger()
	}
	return &BalanceCache{st: st, cfg: cfg, log: log}
}

// UpdatedAt returns the last successful Set time, or zero if never set.
func (b *BalanceCache) UpdatedAt() time.Time {
	if b == nil {
		return time.Time{}
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.updatedAt
}

func (b *BalanceCache) Get(ctx context.Context) (*balancesvc.Summary, bool, error) {
	if b == nil {
		return nil, false, nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.summary == nil || time.Now().After(b.expiresAt) {
		return nil, false, nil
	}
	return b.summary, true, nil
}

func (b *BalanceCache) Invalidate(ctx context.Context) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.summary = nil
	b.expiresAt = time.Time{}
	b.lastBalanceBroadcastDigest = [sha256.Size]byte{}
	b.mu.Unlock()
	b.log.Info("余额缓存：已失效")
}

func balanceUpdateBroadcastDigest(activeAccountID string, summary *balancesvc.Summary) ([sha256.Size]byte, error) {
	var out [sha256.Size]byte
	if summary == nil {
		return out, nil
	}
	body, err := json.Marshal(summary)
	if err != nil {
		return out, err
	}
	var buf bytes.Buffer
	buf.WriteString(activeAccountID)
	buf.WriteByte('\n')
	buf.Write(body)
	return sha256.Sum256(buf.Bytes()), nil
}

// MarkBalanceBroadcastIfChanged records a new balance_update fingerprint when the payload
// differs from the last successful broadcast (or there was none). Returns false when the
// wire JSON for summary plus activeAccountID is unchanged — callers should skip Hub broadcast.
// activeAccountID should be empty when no Polymarket account is active.
func (b *BalanceCache) MarkBalanceBroadcastIfChanged(activeAccountID string, summary *balancesvc.Summary) bool {
	if b == nil || summary == nil {
		return false
	}
	d, err := balanceUpdateBroadcastDigest(activeAccountID, summary)
	if err != nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if d == b.lastBalanceBroadcastDigest {
		return false
	}
	b.lastBalanceBroadcastDigest = d
	return true
}

func (b *BalanceCache) Set(ctx context.Context, summary *balancesvc.Summary) error {
	if b == nil || summary == nil {
		return nil
	}
	merged := mergeSummaryWithLastKnown(summary, b.peekLastAcctUSD())
	b.mu.Lock()
	if b.lastAcctUSD == nil {
		b.lastAcctUSD = make(map[string]float64)
	}
	for _, row := range merged.PolymarketAccounts {
		if row.Polymarket != nil {
			b.lastAcctUSD[row.ID] = *row.Polymarket
		}
	}
	b.summary = merged
	b.expiresAt = time.Now().Add(balanceCacheTTL)
	b.updatedAt = time.Now()
	b.mu.Unlock()
	return nil
}

func (b *BalanceCache) peekLastAcctUSD() map[string]float64 {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if len(b.lastAcctUSD) == 0 {
		return nil
	}
	cp := make(map[string]float64, len(b.lastAcctUSD))
	for k, v := range b.lastAcctUSD {
		cp[k] = v
	}
	return cp
}

// mergeSummaryWithLastKnown fills nil per-account balances from last-known values and
// recomputes top-level polymarket from the active row when possible.
func mergeSummaryWithLastKnown(in *balancesvc.Summary, last map[string]float64) *balancesvc.Summary {
	if in == nil {
		return nil
	}
	if len(last) == 0 {
		return in
	}
	out := *in
	out.PolymarketAccounts = append([]balancesvc.AccountRow(nil), in.PolymarketAccounts...)
	for i := range out.PolymarketAccounts {
		if out.PolymarketAccounts[i].Polymarket != nil {
			continue
		}
		if v, ok := last[out.PolymarketAccounts[i].ID]; ok {
			vv := v
			out.PolymarketAccounts[i].Polymarket = &vv
		}
	}
	var top *float64
	for _, ar := range out.PolymarketAccounts {
		if ar.IsActive && ar.Polymarket != nil {
			v := *ar.Polymarket
			top = &v
			break
		}
	}
	if top != nil {
		out.Polymarket = top
	}
	return &out
}

func (b *BalanceCache) RefreshAsync(ctx context.Context) {
	if b == nil {
		return
	}
	b.refreshMu.Lock()
	defer b.refreshMu.Unlock()

	go func() {
		summary, err := balancesvc.Fetch(context.Background(), b.cfg, b.st)
		if err != nil {
			b.log.WithFields(logx.Pairs("err", err)).Warn("余额缓存：后台刷新失败")
			return
		}
		if err := b.Set(context.Background(), summary); err != nil {
			b.log.WithFields(logx.Pairs("err", err)).Warn("余额缓存：写入失败")
		}
		b.log.Info("余额缓存：已刷新")
	}()
}

func (b *BalanceCache) GetWithRefresh(ctx context.Context) (*balancesvc.Summary, bool, error) {
	if b == nil {
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
	_ = b.Set(ctx, summary)
	b.RefreshAsync(ctx)
	return summary, false, nil
}
