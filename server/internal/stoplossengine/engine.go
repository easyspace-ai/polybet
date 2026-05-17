// Package stoplossengine runs Polymarket mobile stop-loss: position-scoped market
// WebSocket subscriptions, bookcache updates, and risksvc evaluation/close tasks.
package stoplossengine

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/easyspace-ai/polybet/internal/bookcache"
	"github.com/easyspace-ai/polybet/internal/config"
	"github.com/easyspace-ai/polybet/internal/debounce"
	"github.com/easyspace-ai/polybet/internal/logx"
	"github.com/easyspace-ai/polybet/internal/marketstream"
	"github.com/easyspace-ai/polybet/internal/polyexec"
	"github.com/easyspace-ai/polybet/internal/riskruntime"
	"github.com/easyspace-ai/polybet/internal/service/risksvc"
	"github.com/easyspace-ai/polybet/internal/store"
	"github.com/easyspace-ai/polybet/internal/wsconfig"
	"github.com/easyspace-ai/polybet/internal/wsrelay"
)

const maxClobSubscribeBatch = 50

// Engine coordinates market WS subscriptions with open risk positions for the
// active Polymarket account.
type Engine struct {
	cfg      *config.Config
	st       *store.Store
	cache    *bookcache.Cache
	risk     *risksvc.Service
	debounce *debounce.Debouncer
	hub      *wsrelay.Hub
	riskHub  *wsrelay.Hub
	runtime  *riskruntime.Bus
	log      *logrus.Logger

	// Called after risk evaluation (typically app.rebuildAndBroadcastCache).
	onAfterRiskEval func()
	// Called when poly_status should be rebroadcast (reconnect schedule, OB state).
	broadcastPolyStatus func()

	mu            sync.Mutex
	market        *marketstream.MarketStream
	subscribed    map[string]struct{} // CLOB decimal asset id -> subscribed
	lastAccountID string

	bump chan struct{}
}

// New constructs an engine. onAfterRiskEval may be nil.
func New(cfg *config.Config, st *store.Store, cache *bookcache.Cache, risk *risksvc.Service, deb *debounce.Debouncer, hub, riskHub *wsrelay.Hub, rt *riskruntime.Bus, log *logrus.Logger, onAfterRiskEval func(), broadcastPolyStatus func()) *Engine {
	if log == nil {
		log = logrus.StandardLogger()
	}
	return &Engine{
		cfg: cfg, st: st, cache: cache, risk: risk, debounce: deb,
		hub: hub, riskHub: riskHub, runtime: rt, log: log, onAfterRiskEval: onAfterRiskEval,
		broadcastPolyStatus: broadcastPolyStatus,
		subscribed:          make(map[string]struct{}),
		bump:                make(chan struct{}, 1),
	}
}

// Shutdown stops the market upstream WebSocket (idempotent). Run also stops on exit.
func (e *Engine) Shutdown() {
	e.mu.Lock()
	ms := e.market
	e.mu.Unlock()
	if ms != nil {
		ms.Stop()
	}
}

// ClearSubscriptions removes all market WS asset subscriptions (best-effort).
func (e *Engine) ClearSubscriptions() {
	e.mu.Lock()
	ms := e.market
	e.mu.Unlock()
	if ms != nil {
		e.clearSubscriptions(ms)
	}
}

// ForceMarketReconnect closes the market upstream WS and reconnects.
func (e *Engine) ForceMarketReconnect() {
	e.mu.Lock()
	ms := e.market
	e.mu.Unlock()
	if ms != nil {
		ms.ForceReconnect()
	}
}

// NotifyPositionsChanged wakes the reconcile loop (account switch, new trade, refresh).
func (e *Engine) NotifyPositionsChanged() {
	select {
	case e.bump <- struct{}{}:
	default:
	}
}

// EnsureTokenSubscribed adds one asset to the market WS when a client requests
// orderbook data (e.g. risk dashboard subscribePolyBook before reconcile runs).
func (e *Engine) EnsureTokenSubscribed(tokenID string) {
	tid := normalizeTokenID(tokenID)
	if tid == "" {
		return
	}
	dec := polyexec.CLOBAssetIDForAPI(tid)
	if dec == "" {
		return
	}
	e.mu.Lock()
	ms := e.market
	_, already := e.subscribed[dec]
	e.mu.Unlock()
	if already || ms == nil {
		return
	}
	if err := ms.Subscribe(dec); err != nil {
		fields := logx.Pairs("token_id", tid, "clob_token_dec", dec, "err", err.Error())
		e.log.WithFields(fields).Warn("止损引擎：按需订阅失败")
		logx.StopLoss().WithFields(fields).Warn("止损引擎：按需订阅失败")
		return
	}
	e.mu.Lock()
	e.subscribed[dec] = struct{}{}
	n := len(e.subscribed)
	e.mu.Unlock()
	fields := logx.Pairs("token_id", tid, "clob_token_dec", dec, "subscribed_assets", n)
	e.log.WithFields(fields).Info("止损引擎：CLOB 订单簿已按需订阅")
	logx.StopLoss().WithFields(fields).Info("止损引擎：CLOB 订单簿已按需订阅")
	e.risk.SetOrderbookWSState(true, true)
	e.broadcastOrderbookStatus(true)
	if e.runtime != nil {
		e.runtime.Publish("market_sub", "info", "market.subscription.started", e.accountID(), tid, "", "", map[string]any{
			"channel": "clob_market", "addedCount": 1, "reason": "client_subscribe",
		})
	}
}

// Run owns the market WebSocket until ctx is cancelled. The upstream stream is
// supervised: when MarketStream surfaces a fatal error (max reconnect attempts
// exhausted), we mark the risk layer degraded, sleep with backoff, and rebuild
// the stream from scratch so the bot does not silently go market-blind.
func (e *Engine) Run(ctx context.Context) {
	const (
		streamRestartBaseDelay = 2 * time.Second
		streamRestartMaxDelay  = 60 * time.Second
	)
	restartAttempts := 0
	for {
		if ctx.Err() != nil {
			return
		}
		stopped := e.runStreamOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		// Surface degraded state and back off before rebuilding.
		reason := "market stream supervisor: rebuilding upstream"
		if stopped != "" {
			reason = stopped
		}
		if e.risk != nil {
			e.risk.SetWSMarketDown(reason)
		}
		restartAttempts++
		delay := streamRestartBaseDelay << uint(restartAttempts-1)
		if delay > streamRestartMaxDelay || delay <= 0 {
			delay = streamRestartMaxDelay
		}
		fields := logx.Pairs("attempt", restartAttempts, "delay_ms", delay.Milliseconds(), "reason", reason)
		e.log.WithFields(fields).Warn("止损引擎：行情上游受损，准备重建 MarketStream")
		logx.StopLoss().WithFields(fields).Warn("止损引擎：行情上游受损，准备重建 MarketStream")
		if e.runtime != nil {
			e.runtime.Publish("transport", "warn", "ws.market.supervisor_restart", e.accountID(), "", "", "", map[string]any{
				"attempt": restartAttempts, "delayMs": delay.Milliseconds(), "reason": reason,
			})
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		// Reset attempts when we've kept the stream alive for a stable window.
		// runStreamOnce sets degraded immediately; ClearWSMarketDown happens on first tick.
	}
}

// runStreamOnce builds and runs a single MarketStream. It returns when the
// stream stops (context cancel or fatal upstream error). The string is a
// human-readable termination cause; empty means clean shutdown via ctx.
func (e *Engine) runStreamOnce(ctx context.Context) string {
	msCfg := marketstream.DefaultConfig()
	if p := strings.TrimSpace(e.cfg.HTTPPlatformProxy); p != "" {
		msCfg.ProxyURL = p
	}
	marketURL, _ := marketstream.ResolveCLOBWSEndpoints(e.cfg.PolymarketCLOBWS)
	msCfg.MarketWSURL = marketURL
	ws := wsconfig.Load(ctx, e.st)
	msCfg = ws.ToMarketstreamConfig(msCfg)
	msCfg.OnReconnectScheduled = func(attempt int, nextRetryAt time.Time) {
		if e.risk.WSMeta != nil {
			e.risk.WSMeta.SetReconnectSchedule("orderbook", attempt, nextRetryAt)
			e.risk.WSMeta.Record("orderbook", "info", "reconnect scheduled")
		}
		if e.broadcastPolyStatus != nil {
			e.broadcastPolyStatus()
		}
	}

	e.mu.Lock()
	// Reset the local subscription set so reconcile can re-subscribe everything
	// against the fresh underlying stream.
	e.subscribed = make(map[string]struct{})
	e.market = marketstream.NewMarketStreamWithConfig(msCfg)
	ms := e.market
	e.mu.Unlock()

	ms.OnBook(e.handleBook)
	ms.OnPriceChange(e.handlePriceChange)
	ms.OnBestBidAsk(e.handleBestBidAsk)
	ms.OnNewMarket(e.handleNewMarket)
	ms.OnTickSizeChange(e.handleTickSizeChange)
	ms.OnMarketResolved(e.handleMarketResolved)

	if err := ms.Start(ctx); err != nil {
		fields := logx.Pairs("err", err.Error())
		e.log.WithFields(fields).Error("止损引擎：MarketStream 启动失败")
		logx.StopLoss().WithFields(fields).Error("止损引擎：MarketStream 启动失败")
		return "start_failed: " + err.Error()
	}
	defer ms.Stop()

	// Fatal-error channel from the upstream stream (e.g. max reconnect attempts).
	fatalCh := make(chan string, 1)
	go func() {
		for err := range ms.Errors() {
			fields := logx.Pairs("err", err.Error())
			e.log.WithFields(fields).Warn("止损引擎：MarketStream 报错")
			logx.StopLoss().WithFields(fields).Warn("止损引擎：MarketStream 报错")
			if e.runtime != nil {
				e.runtime.Publish("transport", "warn", "ws.market.error", e.accountID(), "", "", "", map[string]any{"err": err.Error()})
			}
			// "max reconnect attempts" is fatal: surface and trigger supervisor.
			if strings.Contains(err.Error(), "max reconnect attempts") {
				select {
				case fatalCh <- err.Error():
				default:
				}
			}
		}
	}()

	reconcileSec := wsconfig.Load(ctx, e.st).StoplossReconcileSec
	ticker := time.NewTicker(time.Duration(reconcileSec) * time.Second)
	defer ticker.Stop()

	e.reconcile(ctx, ms)

	for {
		select {
		case <-ctx.Done():
			return ""
		case fatal := <-fatalCh:
			return "fatal: " + fatal
		case <-e.bump:
			e.reconcileBounded(ctx, ms)
		case <-ticker.C:
			if ctx.Err() != nil {
				return ""
			}
			ws = wsconfig.Load(ctx, e.st)
			reconcileSec = ws.StoplossReconcileSec
			ticker.Reset(time.Duration(reconcileSec) * time.Second)
			e.reconcileBounded(ctx, ms)
		}
	}
}

// reconcileBounded runs reconcile but returns promptly when ctx is cancelled.
func (e *Engine) reconcileBounded(ctx context.Context, ms *marketstream.MarketStream) {
	if ctx.Err() != nil {
		return
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		e.reconcile(ctx, ms)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

func (e *Engine) clearSubscriptions(ms *marketstream.MarketStream) {
	e.mu.Lock()
	ids := make([]string, 0, len(e.subscribed))
	for id := range e.subscribed {
		ids = append(ids, id)
	}
	n := len(ids)
	e.subscribed = make(map[string]struct{})
	e.mu.Unlock()
	for _, chunk := range chunkStrings(ids, maxClobSubscribeBatch) {
		_ = ms.Unsubscribe(chunk...)
	}
	if n > 0 && e.runtime != nil {
		e.runtime.Publish("market_sub", "info", "market.subscription.stopped", e.accountID(), "", "", "", map[string]any{
			"reason": "shutdown_or_clear", "removedCount": n,
		})
	}
	e.risk.SetOrderbookWSState(false, false)
	e.broadcastOrderbookStatus(false)
}

func (e *Engine) reconcile(ctx context.Context, ms *marketstream.MarketStream) {
	if ctx.Err() != nil {
		return
	}
	acct, err := e.st.GetActivePolymarketAccount(ctx)
	if err != nil || acct == nil {
		e.clearSubscriptions(ms)
		e.mu.Lock()
		e.lastAccountID = ""
		e.mu.Unlock()
		e.risk.SetOrderbookWSState(false, false)
		e.broadcastOrderbookStatus(false)
		return
	}

	e.mu.Lock()
	prevAcct := e.lastAccountID
	e.mu.Unlock()

	if acct.ID != prevAcct {
		fields := logx.Pairs("from", prevAcct, "to", acct.ID)
		e.log.WithFields(fields).Info("止损引擎：活跃账户已切换")
		logx.StopLoss().WithFields(fields).Info("止损引擎：活跃账户已切换")
		if e.runtime != nil {
			e.runtime.Publish("transport", "info", "risk.account_switched", acct.ID, "", "", "", map[string]any{"from": prevAcct, "to": acct.ID})
		}
		e.clearSubscriptions(ms)
		if ctx.Err() != nil {
			return
		}
		if err := e.risk.SyncPositionsFromDataAPI(ctx, acct.ID); err != nil {
			fields := logx.Pairs("err", err.Error())
			e.log.WithFields(fields).Warn("止损引擎：切换账户后同步持仓失败")
			logx.Position().WithFields(fields).Warn("止损引擎：切换账户后同步持仓失败")
		}
		e.mu.Lock()
		e.lastAccountID = acct.ID
		e.mu.Unlock()
		if e.onAfterRiskEval != nil {
			e.onAfterRiskEval()
		}
	}

	min := e.st.GetBotConfigFloat(ctx, "minOpenRiskShares", 1)
	rows, err := e.st.ListOpenRiskPositionsMinShares(ctx, min, acct.ID)
	if err != nil {
		fields := logx.Pairs("err", err.Error())
		e.log.WithFields(fields).Warn("止损引擎：列举持仓失败")
		logx.StopLoss().WithFields(fields).Warn("止损引擎：列举持仓失败")
		return
	}

	want := make(map[string]struct{})
	seen := make(map[string]struct{})
	for _, p := range rows {
		tid := normalizeTokenID(p.TokenID)
		if tid == "" {
			continue
		}
		if _, ok := seen[tid]; ok {
			continue
		}
		seen[tid] = struct{}{}
		dec := polyexec.CLOBAssetIDForAPI(tid)
		if dec != "" {
			want[dec] = struct{}{}
		}
	}

	e.mu.Lock()
	toAdd := make([]string, 0)
	toRemove := make([]string, 0)
	for id := range e.subscribed {
		if _, ok := want[id]; !ok {
			toRemove = append(toRemove, id)
		}
	}
	for id := range want {
		if _, ok := e.subscribed[id]; !ok {
			toAdd = append(toAdd, id)
		}
	}
	e.mu.Unlock()

	if len(toRemove) > 0 {
		for _, chunk := range chunkStrings(toRemove, maxClobSubscribeBatch) {
			if ctx.Err() != nil {
				return
			}
			if err := ms.Unsubscribe(chunk...); err != nil {
				fields := logx.Pairs("err", err.Error())
				e.log.WithFields(fields).Warn("止损引擎：取消订阅失败")
				logx.StopLoss().WithFields(fields).Warn("止损引擎：取消订阅失败")
			}
		}
		e.mu.Lock()
		for _, id := range toRemove {
			delete(e.subscribed, id)
		}
		e.mu.Unlock()
		if e.runtime != nil {
			e.runtime.Publish("market_sub", "info", "market.subscription.stopped", acct.ID, "", "", "", map[string]any{
				"reason": "reconcile", "removedCount": len(toRemove),
			})
		}
	}

	if len(toAdd) > 0 {
		e.risk.SetOrderbookWSState(true, false)
		for _, chunk := range chunkStrings(toAdd, maxClobSubscribeBatch) {
			if ctx.Err() != nil {
				return
			}
			if err := ms.Subscribe(chunk...); err != nil {
				fields := logx.Pairs("err", err.Error())
				e.log.WithFields(fields).Warn("止损引擎：订阅失败")
				logx.StopLoss().WithFields(fields).Warn("止损引擎：订阅失败")
			}
		}
		e.mu.Lock()
		for _, id := range toAdd {
			e.subscribed[id] = struct{}{}
		}
		e.mu.Unlock()
		if e.runtime != nil {
			e.runtime.Publish("market_sub", "info", "market.subscription.started", acct.ID, "", "", "", map[string]any{
				"channel": "clob_market", "addedCount": len(toAdd),
			})
		}
	}

	if len(e.snapshotSubscribed()) == 0 {
		e.risk.SetOrderbookWSState(false, false)
		e.broadcastOrderbookStatus(false)
	} else {
		e.risk.SetOrderbookWSState(true, true)
		e.broadcastOrderbookStatus(true)
	}
}

func (e *Engine) accountID() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastAccountID
}

func (e *Engine) snapshotSubscribed() map[string]struct{} {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make(map[string]struct{}, len(e.subscribed))
	for k := range e.subscribed {
		out[k] = struct{}{}
	}
	return out
}

func (e *Engine) broadcastOrderbookStatus(connected bool) {
	if connected && e.risk.WSMeta != nil {
		e.risk.WSMeta.ClearReconnectSchedule("orderbook")
		e.risk.WSMeta.Record("orderbook", "info", "connected")
	} else if !connected && e.risk.WSMeta != nil {
		e.risk.WSMeta.Record("orderbook", "warn", "disconnected")
	}
	if e.broadcastPolyStatus != nil {
		e.broadcastPolyStatus()
		return
	}
	if e.hub != nil {
		e.hub.BroadcastJSON(map[string]any{"type": "poly_status", "polyOrderbookConnected": connected})
	}
	if e.riskHub != nil {
		e.riskHub.BroadcastJSON(map[string]any{"type": "poly_status", "polyOrderbookConnected": connected})
	}
}

func (e *Engine) handleBook(ev marketstream.BookEvent) {
	bid := 0.0
	ask := 0.0
	if len(ev.Bids) > 0 {
		bid, _ = strconv.ParseFloat(ev.Bids[0].Price, 64)
	}
	if len(ev.Asks) > 0 {
		ask, _ = strconv.ParseFloat(ev.Asks[0].Price, 64)
	}
	e.applyBookUpdate(ev.AssetID, bid, ask, len(ev.Bids) > 0 || len(ev.Asks) > 0, ev.Bids, ev.Asks, ev.Timestamp)
}

func (e *Engine) handlePriceChange(ev marketstream.PriceChangeEvent) {
	for _, pc := range ev.PriceChanges {
		bid, _ := strconv.ParseFloat(pc.BestBid, 64)
		ask, _ := strconv.ParseFloat(pc.BestAsk, 64)
		e.applyBookUpdate(pc.AssetID, bid, ask, false, nil, nil, ev.Timestamp)
	}
}

func (e *Engine) handleBestBidAsk(ev marketstream.BestBidAskEvent) {
	bid, _ := strconv.ParseFloat(ev.BestBid, 64)
	ask, _ := strconv.ParseFloat(ev.BestAsk, 64)
	e.applyBookUpdate(ev.AssetID, bid, ask, false, nil, nil, ev.Timestamp)
}

func (e *Engine) applyBookUpdate(assetIDRaw string, bid, ask float64, hasBook bool, bids, asks []marketstream.OrderLevel, tsRaw string) {
	assetID := normalizeTokenID(assetIDRaw)
	if assetID == "" {
		return
	}
	// Any inbound book event proves the upstream is healthy: clear degraded
	// flag and refresh the last-tick timestamp used by the trade gate.
	if e.risk != nil {
		e.risk.MarkBookTick()
		e.risk.ClearWSMarketDown()
	}
	ts := parseWSTS(tsRaw)
	if hasBook {
		lvB := make([]struct{ Price, Size string }, len(bids))
		for i, b := range bids {
			lvB[i] = struct{ Price, Size string }{Price: b.Price, Size: b.Size}
		}
		lvA := make([]struct{ Price, Size string }, len(asks))
		for i, a := range asks {
			lvA[i] = struct{ Price, Size string }{Price: a.Price, Size: a.Size}
		}
		e.cache.ReplaceBook(assetID, lvB, lvA, ts)
	} else if bid > 0 || ask > 0 {
		e.cache.ApplyTopOfBook(assetID, bid, ask, ts)
	}
	bidsOut, asksOut := e.cache.GetBidsAsks(assetID, 5)
	bestBid, bestAsk, _ := e.cache.TopOfBook(assetID)
	if bestBid == 0 && bid > 0 {
		bestBid = bid
	}
	if bestAsk == 0 && ask > 0 {
		bestAsk = ask
	}
	bidCents := bestBid * 100
	askCents := bestAsk * 100
	wsMsg := map[string]any{
		"type":    "polyBookUpdate",
		"tokenId": assetID,
		"bids":    bidsOut,
		"asks":    asksOut,
		"bestBid": bidCents,
		"bestAsk": askCents,
	}
	if e.hub != nil {
		e.hub.BroadcastJSON(wsMsg)
	}
	if e.riskHub != nil {
		e.riskHub.BroadcastJSON(wsMsg)
	}
	if e.runtime != nil {
		e.runtime.MaybePublishMarketBookSummary(assetID, e.accountID(), bidCents, askCents)
	}
	e.debounce.Trigger(assetID, func() {
		_ = e.risk.RiskEvaluateTokenAfterBookUpdate(context.Background(), assetID)
		if e.onAfterRiskEval != nil {
			e.onAfterRiskEval()
		}
	})
}

// handleNewMarket pushes the per-market taker fee surfaced by the CLOB WS
// into bookcache so price-adjusted ladder lookups stay aligned with what
// the exchange is actually charging — without waiting for the next Gamma
// sync cycle. Called for every clob_token_id in the new_market payload.
func (e *Engine) handleNewMarket(ev marketstream.NewMarketEvent) {
	feeRate, ok := marketstream.NewMarketEventFeeRate(&ev)
	if !ok {
		return
	}
	for _, tok := range ev.ClobTokenIds {
		tid := normalizeTokenID(tok)
		if tid == "" {
			continue
		}
		if e.cache != nil {
			e.cache.SetFeeRate(tid, feeRate)
		}
	}
	if e.log != nil {
		e.log.WithFields(logx.Pairs(
			"market", ev.Market, "condition_id", ev.ConditionID, "tokens", len(ev.ClobTokenIds),
			"fee_rate", feeRate,
		)).Info("止损引擎：CLOB new_market 已更新 bookcache 费率")
	}
}

// handleTickSizeChange logs tick size changes for forensics. The bookcache
// does not store tick size separately — order construction reads it
// fresh from the REST /book before signing — but operators benefit from
// seeing the change in the structured log timeline.
func (e *Engine) handleTickSizeChange(ev marketstream.TickSizeChangeEvent) {
	if e.log == nil {
		return
	}
	e.log.WithFields(logx.Pairs(
		"asset_id", ev.AssetID, "old_tick_size", ev.OldTickSize, "new_tick_size", ev.NewTickSize,
	)).Info("止损引擎：CLOB tick_size_change")
}

// handleMarketResolved drops cached fee + book state for the resolved
// market so a stale book doesn't satisfy the trade-gate freshness check
// after the underlying market is settled.
func (e *Engine) handleMarketResolved(ev marketstream.MarketResolvedEvent) {
	if e.cache == nil {
		return
	}
	for _, tok := range ev.AssetsIDs {
		tid := normalizeTokenID(tok)
		if tid == "" {
			continue
		}
		// Set fee rate to 0 so any future stale ladder lookup is at least
		// not double-charged for fees on a market that no longer exists.
		// PruneIdle eventually evicts the token's book entirely.
		e.cache.SetFeeRate(tid, 0)
	}
	if e.log != nil {
		e.log.WithFields(logx.Pairs(
			"market", ev.Market, "winning_outcome", ev.WinningOutcome, "tokens", len(ev.AssetsIDs),
		)).Info("止损引擎：CLOB market_resolved")
	}
}

func normalizeTokenID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if strings.HasPrefix(id, "0x") {
		id = strings.ToLower(id)
	} else {
		if n, ok := new(big.Int).SetString(id, 10); ok {
			id = "0x" + fmt.Sprintf("%064x", n)
		} else {
			id = "0x" + strings.ToLower(strings.TrimPrefix(id, "0x"))
		}
	}
	if len(id) < 66 && strings.HasPrefix(id, "0x") {
		id = "0x" + strings.Repeat("0", 66-len(id)) + id[2:]
	}
	return id
}

func chunkStrings(ids []string, size int) [][]string {
	if size <= 0 {
		size = 50
	}
	var out [][]string
	for i := 0; i < len(ids); i += size {
		j := i + size
		if j > len(ids) {
			j = len(ids)
		}
		out = append(out, ids[i:j])
	}
	return out
}

func parseWSTS(raw string) int64 {
	if raw == "" {
		return time.Now().UnixMilli()
	}
	if ms, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return ms
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t.UnixMilli()
	}
	return time.Now().UnixMilli()
}
