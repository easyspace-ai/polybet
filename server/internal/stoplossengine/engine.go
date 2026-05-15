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

	mu            sync.Mutex
	market        *marketstream.MarketStream
	subscribed    map[string]struct{} // CLOB decimal asset id -> subscribed
	lastAccountID string

	bump chan struct{}
}

// New constructs an engine. onAfterRiskEval may be nil.
func New(cfg *config.Config, st *store.Store, cache *bookcache.Cache, risk *risksvc.Service, deb *debounce.Debouncer, hub, riskHub *wsrelay.Hub, rt *riskruntime.Bus, log *logrus.Logger, onAfterRiskEval func()) *Engine {
	if log == nil {
		log = logrus.StandardLogger()
	}
	return &Engine{
		cfg: cfg, st: st, cache: cache, risk: risk, debounce: deb,
		hub: hub, riskHub: riskHub, runtime: rt, log: log, onAfterRiskEval: onAfterRiskEval,
		subscribed: make(map[string]struct{}),
		bump:       make(chan struct{}, 1),
	}
}

// NotifyPositionsChanged wakes the reconcile loop (account switch, new trade, refresh).
func (e *Engine) NotifyPositionsChanged() {
	select {
	case e.bump <- struct{}{}:
	default:
	}
}

// Run owns the market WebSocket until ctx is cancelled.
func (e *Engine) Run(ctx context.Context) {
	msCfg := marketstream.DefaultConfig()
	if p := strings.TrimSpace(e.cfg.HTTPPlatformProxy); p != "" {
		msCfg.ProxyURL = p
	}
	marketURL, _ := marketstream.ResolveCLOBWSEndpoints(e.cfg.PolymarketCLOBWS)
	msCfg.MarketWSURL = marketURL

	e.mu.Lock()
	e.market = marketstream.NewMarketStreamWithConfig(msCfg)
	ms := e.market
	e.mu.Unlock()

	ms.OnBook(e.handleBook)
	ms.OnPriceChange(e.handlePriceChange)
	ms.OnBestBidAsk(e.handleBestBidAsk)

	if err := ms.Start(ctx); err != nil {
		e.log.WithFields(logx.Pairs("err", err.Error())).Error("止损引擎：MarketStream 启动失败")
		return
	}
	defer ms.Stop()

	go func() {
		for err := range ms.Errors() {
			e.log.WithFields(logx.Pairs("err", err.Error())).Warn("止损引擎：MarketStream 报错")
			if e.runtime != nil {
				e.runtime.Publish("transport", "warn", "ws.market.error", e.accountID(), "", "", "", map[string]any{"err": err.Error()})
			}
		}
	}()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	e.reconcile(ctx, ms)

	for {
		select {
		case <-ctx.Done():
			e.clearSubscriptions(ms)
			return
		case <-e.bump:
			e.reconcile(ctx, ms)
		case <-ticker.C:
			e.reconcile(ctx, ms)
		}
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
		e.log.WithFields(logx.Pairs("from", prevAcct, "to", acct.ID)).Info("止损引擎：活跃账户已切换")
		if e.runtime != nil {
			e.runtime.Publish("transport", "info", "risk.account_switched", acct.ID, "", "", "", map[string]any{"from": prevAcct, "to": acct.ID})
		}
		e.clearSubscriptions(ms)
		if err := e.risk.SyncPositionsFromDataAPI(ctx, acct.ID); err != nil {
			e.log.WithFields(logx.Pairs("err", err.Error())).Warn("止损引擎：切换账户后同步持仓失败")
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
		e.log.WithFields(logx.Pairs("err", err.Error())).Warn("止损引擎：列举持仓失败")
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
			if err := ms.Unsubscribe(chunk...); err != nil {
				e.log.WithFields(logx.Pairs("err", err.Error())).Warn("止损引擎：取消订阅失败")
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
			if err := ms.Subscribe(chunk...); err != nil {
				e.log.WithFields(logx.Pairs("err", err.Error())).Warn("止损引擎：订阅失败")
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
