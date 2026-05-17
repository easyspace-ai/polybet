package app

import (
	"context"
	"time"

	"github.com/easyspace-ai/polybet/internal/logx"
	"github.com/easyspace-ai/polybet/internal/memcache"
	"github.com/easyspace-ai/polybet/internal/service/balancesvc"
	"github.com/easyspace-ai/polybet/internal/service/risksvc"
	"github.com/easyspace-ai/polybet/internal/workqueue"
)

func (a *App) initWorkQueue() {
	if a == nil {
		return
	}
	if a.jobs == nil {
		a.jobs = workqueue.New()
	}
}

// ScheduleInvalidateAndRebuildCache rebuilds caches on a debounced background
// worker. Old snapshots stay served until the rebuild completes (stale-while-revalidate).
func (a *App) ScheduleInvalidateAndRebuildCache() {
	if a == nil {
		return
	}
	a.initWorkQueue()
	a.ScheduleRiskCacheRebuild()
	a.scheduleBalanceRebuild()
}

func (a *App) ScheduleRiskCacheRebuild() {
	if a == nil {
		return
	}
	a.initWorkQueue()
	a.Debounce.Trigger("risk_cache_rebuild", func() {
		a.jobs.Run("risk_cache_rebuild", func(ctx context.Context) error {
			a.rebuildRiskCacheSync(ctx)
			return nil
		})
	})
}

func (a *App) scheduleBalanceRebuild() {
	if a == nil {
		return
	}
	a.Debounce.Trigger("balance_rebuild", func() {
		a.jobs.Run("balance_rebuild", func(ctx context.Context) error {
			runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			if summary, err := balancesvc.Fetch(runCtx, a.Cfg, a.Store); err == nil {
				_ = a.BalanceCache.Set(runCtx, summary)
				a.broadcastBalanceUpdateIfChanged(runCtx, summary)
			}
			return nil
		})
	})
}

// InvalidateAndRebuildCache is kept for compatibility; it schedules async work.
func (a *App) InvalidateAndRebuildCache() {
	a.ScheduleInvalidateAndRebuildCache()
}

func (a *App) rebuildCachesSync(ctx context.Context) {
	a.rebuildRiskCacheSync(ctx)
	a.scheduleBalanceRebuild()
}

func (a *App) rebuildRiskCacheSync(ctx context.Context) {
	if a == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	meta := risksvc.Meta{OutboundProxyConfigured: a.Cfg.HTTPPlatformProxy != ""}
	acct, _ := a.Store.GetActivePolymarketAccount(runCtx)
	accountID := ""
	if acct != nil {
		accountID = acct.ID
	}

	rows, enrichedMeta, err := a.Risk.ListRiskPositionsEnriched(runCtx, meta, accountID)
	if err != nil {
		a.Log.WithFields(logx.Pairs("err", err.Error())).Warn("缓存重建：风控列表失败")
		return
	}
	oldRows, _, found, _ := a.RiskCache.Get(runCtx)
	shouldBroadcast := !found || !positionsStructurallyEqual(oldRows, rows)
	_ = a.RiskCache.Set(runCtx, memcache.RiskFetchResult{Positions: rows, Meta: memcache.RiskMeta{
		UserWsConnected:         enrichedMeta.UserWsConnected,
		UserWsConnecting:        enrichedMeta.UserWsConnecting,
		OutboundProxyConfigured: enrichedMeta.OutboundProxyConfigured,
		MinOpenRiskShares:       enrichedMeta.MinOpenRiskShares,
		RiskCloseExecutionMode:  enrichedMeta.RiskCloseExecutionMode,
		RiskCloseFakWorstPrice:  enrichedMeta.RiskCloseFakWorstPrice,
		RiskHedgeBuySizing:      enrichedMeta.RiskHedgeBuySizing,
	}})
	if shouldBroadcast {
		payload := map[string]any{"type": "position_update", "data": rows}
		a.Hub.BroadcastJSONAsync(payload)
		a.RiskHub.BroadcastJSONAsync(payload)
	}
}

// ScheduleRiskOfficialRefresh pulls official positions and rebuilds caches in the
// background. Returns false when a refresh is already running.
func (a *App) ScheduleRiskOfficialRefresh() bool {
	if a == nil {
		return true
	}
	a.initWorkQueue()
	return a.jobs.Run("risk_official_refresh", func(ctx context.Context) error {
		runCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		a.Log.Info("风控刷新：后台官方同步开始")
		if err := a.Risk.SyncRiskFromRESTTrades(runCtx); err != nil {
			a.Log.WithFields(logx.Pairs("err", err.Error())).Warn("风控刷新：后台官方同步失败")
		} else {
			a.Log.Info("风控刷新：后台官方同步完成")
		}
		a.rebuildCachesSync(runCtx)
		return nil
	})
}

// broadcastPositionSnapshotFast pushes a read-only position list over WS and
// updates RiskCache without waiting for REST orderbook / full enrich. Used on
// the hot path (CLOB trade applied) so the dashboard shows new rows immediately.
func (a *App) broadcastPositionSnapshotFast() {
	if a == nil || a.Risk == nil {
		return
	}
	meta := risksvc.Meta{OutboundProxyConfigured: a.Cfg.HTTPPlatformProxy != ""}
	acct, _ := a.Store.GetActivePolymarketAccount(context.Background())
	accountID := ""
	if acct != nil {
		accountID = acct.ID
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	rows, enrichedMeta, err := a.Risk.ListRiskPositionsEnrichedReadOnly(ctx, meta, accountID)
	if err != nil {
		a.Log.WithFields(logx.Pairs("err", err.Error())).Debug("持仓快照：快速广播失败")
		return
	}
	_ = a.RiskCache.Set(ctx, memcache.RiskFetchResult{Positions: rows, Meta: memcache.RiskMeta{
		UserWsConnected:         enrichedMeta.UserWsConnected,
		UserWsConnecting:        enrichedMeta.UserWsConnecting,
		OutboundProxyConfigured: enrichedMeta.OutboundProxyConfigured,
		MinOpenRiskShares:       enrichedMeta.MinOpenRiskShares,
		RiskCloseExecutionMode:  enrichedMeta.RiskCloseExecutionMode,
		RiskCloseFakWorstPrice:  enrichedMeta.RiskCloseFakWorstPrice,
		RiskHedgeBuySizing:      enrichedMeta.RiskHedgeBuySizing,
	}})
	payload := map[string]any{"type": "position_update", "data": rows}
	a.Hub.BroadcastJSONAsync(payload)
	a.RiskHub.BroadcastJSONAsync(payload)
}

// ScheduleMarketsFullRefresh invalidates risk/balance memcache and schedules a
// forced Gamma sync (same semantics as POST /api/cache/refresh then
// POST /api/markets/refresh?force=1). Cache rebuild is debounced; market sync
// runs on a separate worker. Returns false when a market refresh is already running.
func (a *App) ScheduleMarketsFullRefresh() bool {
	if a == nil {
		return true
	}
	a.ScheduleInvalidateAndRebuildCache()
	return a.ScheduleMarketsRefresh(true)
}

// ScheduleMarketsRefresh runs Gamma sync in the background. Returns false when
// another market refresh is already in flight.
func (a *App) ScheduleMarketsRefresh(force bool) bool {
	if a == nil {
		return true
	}
	a.initWorkQueue()
	key := "markets_refresh"
	if force {
		key = "markets_refresh_force"
	}
	return a.jobs.Run(key, func(ctx context.Context) error {
		runCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		a.Log.Info("市场刷新：后台同步开始")
		if err := a.SyncAndBroadcastMarkets(runCtx, force); err != nil {
			a.Log.WithFields(logx.Pairs("err", err.Error())).Warn("市场刷新：后台同步失败")
			return err
		}
		a.Log.Info("市场刷新：后台同步完成")
		return nil
	})
}
