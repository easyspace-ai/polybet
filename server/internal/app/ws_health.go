package app

import (
	"context"
	"time"

	"github.com/easyspace-ai/polybet/internal/logx"
	"github.com/easyspace-ai/polybet/internal/polyexec"
	"github.com/easyspace-ai/polybet/internal/polywarm"
	"github.com/easyspace-ai/polybet/internal/wsconfig"
)

func (a *App) wsHealthTicker(ctx context.Context) {
	defer a.wg.Done()
	for {
		ws := wsconfig.Load(ctx, a.Store)
		t := time.NewTimer(time.Duration(ws.HealthCheckIntervalSec) * time.Second)
		select {
		case <-ctx.Done():
			if !t.Stop() {
				<-t.C
			}
			return
		case <-t.C:
		}
		a.runWSHealthCheck(ctx, ws)
	}
}

func (a *App) runWSHealthCheck(ctx context.Context, ws wsconfig.Settings) {
	if ctx.Err() != nil {
		return
	}
	if !a.Risk.OrderbookWSConnected() {
		a.Log.Warn("WS 健康检查：盘口上游未连接，触发重连")
		if a.Risk.WSMeta != nil {
			a.Risk.WSMeta.Record("orderbook", "warn", "health check: disconnected")
		}
		if a.StopLoss != nil {
			a.StopLoss.ForceMarketReconnect()
		}
	}
	if !a.Risk.UserWSConnected() {
		a.Log.Warn("WS 健康检查：用户通道未连接，触发重连与持仓同步")
		if a.Risk.WSMeta != nil {
			a.Risk.WSMeta.Record("user", "warn", "health check: disconnected")
		}
		a.ForceWSReconnect("user")
		acct, _ := a.Store.GetActivePolymarketAccount(ctx)
		if acct != nil {
			if err := a.Risk.SyncPositionsFromDataAPI(ctx, acct.ID); err != nil {
				a.Log.WithFields(logx.Pairs("err", err.Error())).Warn("WS 健康检查：持仓同步失败")
			} else {
				a.rebuildAndBroadcastCache()
			}
		}
	}

	staleThreshold := time.Duration(ws.BookStaleThresholdSec) * time.Second
	acct, _ := a.Store.GetActivePolymarketAccount(ctx)
	if acct == nil {
		a.broadcastPolyStatus()
		return
	}
	minShares := a.Store.GetBotConfigFloat(ctx, "minOpenRiskShares", 1)
	rows, err := a.Store.ListOpenRiskPositionsMinShares(ctx, minShares, acct.ID)
	if err != nil {
		return
	}
	for _, p := range rows {
		if ctx.Err() != nil {
			return
		}
		tid := normalizeTokenID(p.TokenID)
		if tid == "" {
			continue
		}
		age, ok := a.Cache.BookAge(tid)
		if !ok || age <= staleThreshold {
			continue
		}
		a.Log.WithFields(logx.Pairs("token_id", tid, "age_sec", age.Seconds())).Warn("WS 健康检查：盘口数据过期，REST 兜底")
		dec := polyexec.CLOBAssetIDForAPI(tid)
		if dec == "" {
			dec = tid
		}
		cents, err := polywarm.BestBidCents(ctx, a.Cfg.PolymarketAPIURL, a.Cfg.HTTPPlatformProxy, dec)
		if err != nil {
			continue
		}
		_ = a.Risk.RiskEvaluateTokenAfterBookUpdate(ctx, tid)
		fields := logx.Pairs("token_id", tid, "bid_cents", cents)
		a.Log.WithFields(fields).Info("WS 健康检查：已用 REST 价评估止损")
		logx.StopLoss().WithFields(fields).Info("WS 健康检查：已用 REST 价评估止损")
	}
	a.broadcastPolyStatus()
}

// OpenRiskPositionCount returns open risk positions for the active account.
func (a *App) OpenRiskPositionCount(ctx context.Context) int {
	acct, _ := a.Store.GetActivePolymarketAccount(ctx)
	if acct == nil {
		return 0
	}
	minShares := a.Store.GetBotConfigFloat(ctx, "minOpenRiskShares", 1)
	n, err := a.Store.CountOpenRiskPositionsMinShares(ctx, minShares, acct.ID)
	if err != nil {
		return 0
	}
	return int(n)
}
