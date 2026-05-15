package app

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/easyspace-ai/polybet/internal/logx"
	"github.com/easyspace-ai/polybet/internal/marketstream"
)

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

type tokenIDStore interface {
	ListPolymarketOutcomeTokenIDs(ctx context.Context) ([]string, error)
	ListOpenRiskPositionTokenIDs(ctx context.Context) ([]string, error)
}

func buildTokenIDs(st tokenIDStore) ([]string, []string, error) {
	riskIds, err := st.ListOpenRiskPositionTokenIDs(context.Background())
	if err != nil {
		return nil, nil, err
	}
	if len(riskIds) == 0 {
		return nil, nil, nil
	}
	merged := make([]string, 0, len(riskIds))
	normalizedRiskIds := make([]string, 0, len(riskIds))
	set := make(map[string]struct{}, len(riskIds))
	for _, id := range riskIds {
		id = normalizeTokenID(id)
		if id == "" {
			continue
		}
		if _, ok := set[id]; !ok {
			set[id] = struct{}{}
			merged = append(merged, id)
			normalizedRiskIds = append(normalizedRiskIds, id)
		}
	}
	return merged, normalizedRiskIds, nil
}

func sameStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[string]struct{}, len(a))
	for _, v := range a {
		m[v] = struct{}{}
	}
	for _, v := range b {
		if _, ok := m[v]; !ok {
			return false
		}
	}
	return true
}

func (a *App) polyWSLoop(ctx context.Context) {
	defer a.wg.Done()

	for {
		if ctx.Err() != nil {
			return
		}

		_, normalizedRiskIds, err := buildTokenIDs(a.Store)
		if err != nil {
			a.Log.WithFields(logx.Pairs("err", err)).Warn("风控订单簿 WS：列举 token 失败")
			time.Sleep(5 * time.Second)
			continue
		}

		if len(normalizedRiskIds) == 0 {
			a.Log.WithFields(logx.Pairs("reason", "no active risk positions")).Debug("风控订单簿 WS：无持仓，空闲等待")
			a.Risk.SetOrderbookWSState(false, false)
			time.Sleep(10 * time.Second)
			continue
		}

		chunks := chunkSlice(normalizedRiskIds, 50)
		a.Log.WithFields(logx.Pairs("connections", len(chunks), "total_tokens", len(normalizedRiskIds))).Info("风控订单簿 WS：开始守护连接")

		subCtx, cancel := context.WithCancel(ctx)
		var subWg sync.WaitGroup

		for i, chunk := range chunks {
			subWg.Add(1)
			go func(id int, tokens []string) {
				defer subWg.Done()
				a.runRiskMarketStream(subCtx, id, tokens)
			}(i, chunk)
		}

		ticker := time.NewTicker(30 * time.Second)
		shouldRestart := false
		for !shouldRestart {
			select {
			case <-ctx.Done():
				cancel()
				ticker.Stop()
				return
			case <-ticker.C:
				_, currentRiskIds, err := buildTokenIDs(a.Store)
				if err == nil && !sameStringSlice(normalizedRiskIds, currentRiskIds) {
					a.Log.WithFields(logx.Pairs("old_count", len(normalizedRiskIds), "new_count", len(currentRiskIds))).Info("风控订单簿 WS：持仓 token 集合已变化，将重连")
					shouldRestart = true
				}
			}
		}

		cancel()
		ticker.Stop()
		subWg.Wait()
		a.Log.Info("风控订单簿 WS：正在重启守护循环")
		time.Sleep(2 * time.Second)
	}
}

func (a *App) polymarketMarketstreamConfig() *marketstream.Config {
	cfg := marketstream.DefaultConfig()
	cfg.ProxyURL = a.Cfg.HTTPPlatformProxy
	marketURL, userURL := marketstream.ResolveCLOBWSEndpoints(a.Cfg.PolymarketCLOBWS)
	cfg.MarketWSURL = marketURL
	cfg.UserWSURL = userURL
	cfg.HandshakeTimeout = 45 * time.Second
	cfg.PingInterval = 20 * time.Second
	cfg.PongTimeout = 60 * time.Second
	return cfg
}

func firstBookBid(levels []marketstream.OrderLevel) float64 {
	if len(levels) == 0 {
		return 0
	}
	bid, _ := strconv.ParseFloat(levels[0].Price, 64)
	return bid
}

func firstBookAsk(levels []marketstream.OrderLevel) float64 {
	if len(levels) == 0 {
		return 0
	}
	ask, _ := strconv.ParseFloat(levels[0].Price, 64)
	return ask
}

func (a *App) applyRiskPolyBookUpdate(assetIDRaw string, bid, ask float64, hasBook bool, bids []marketstream.OrderLevel, asks []marketstream.OrderLevel, tsRaw string) {
	assetID := normalizeTokenID(assetIDRaw)
	ts := parseWSTS(tsRaw)

	if hasBook {
		bb := make([]struct{ Price, Size string }, 0, len(bids))
		for _, x := range bids {
			bb = append(bb, struct{ Price, Size string }{Price: x.Price, Size: x.Size})
		}
		aa := make([]struct{ Price, Size string }, 0, len(asks))
		for _, x := range asks {
			aa = append(aa, struct{ Price, Size string }{Price: x.Price, Size: x.Size})
		}
		a.Cache.ReplaceBook(assetID, bb, aa, ts)
	}

	bidsOut, asksOut := a.Cache.GetBidsAsks(assetID, 5)
	bestBid, bestAsk, _ := a.Cache.TopOfBook(assetID)
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
	a.Hub.BroadcastJSON(wsMsg)
	a.RiskHub.BroadcastJSON(wsMsg)

	a.Debounce.Trigger(assetID, func() {
		_ = a.Risk.RiskEvaluateTokenAfterBookUpdate(context.Background(), assetID)
		a.rebuildAndBroadcastCache()
	})
}

func (a *App) runRiskMarketStream(ctx context.Context, subID int, tokens []string) {
	cfg := a.polymarketMarketstreamConfig()
	ms := marketstream.NewMarketStreamWithConfig(cfg)

	var assetIDs []string
	for _, id := range tokens {
		if n, ok := new(big.Int).SetString(strings.TrimPrefix(id, "0x"), 16); ok {
			assetIDs = append(assetIDs, n.String())
		}
	}

	onDisconnect := func() {
		a.Risk.SetOrderbookWSState(true, false)
		a.Hub.BroadcastJSON(map[string]any{"type": "poly_status", "polyOrderbookConnected": false})
		a.RiskHub.BroadcastJSON(map[string]any{"type": "poly_status", "polyOrderbookConnected": false})
	}

	ms.OnBook(func(ev marketstream.BookEvent) {
		if ctx.Err() != nil {
			return
		}
		bid := firstBookBid(ev.Bids)
		ask := firstBookAsk(ev.Asks)
		a.applyRiskPolyBookUpdate(ev.AssetID, bid, ask, true, ev.Bids, ev.Asks, ev.Timestamp)
	})
	ms.OnBestBidAsk(func(ev marketstream.BestBidAskEvent) {
		if ctx.Err() != nil {
			return
		}
		bid, _ := strconv.ParseFloat(ev.BestBid, 64)
		ask, _ := strconv.ParseFloat(ev.BestAsk, 64)
		a.applyRiskPolyBookUpdate(ev.AssetID, bid, ask, false, nil, nil, ev.Timestamp)
	})
	ms.OnPriceChange(func(ev marketstream.PriceChangeEvent) {
		if ctx.Err() != nil {
			return
		}
		for _, pc := range ev.PriceChanges {
			bid, _ := strconv.ParseFloat(pc.BestBid, 64)
			ask, _ := strconv.ParseFloat(pc.BestAsk, 64)
			a.applyRiskPolyBookUpdate(pc.AssetID, bid, ask, false, nil, nil, ev.Timestamp)
		}
	})

	if err := ms.Start(ctx); err != nil {
		a.Log.WithFields(logx.Pairs("sub_id", subID, "err", err)).Error("风控订单簿 WS：MarketStream 启动失败")
		return
	}
	defer ms.Stop()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case err := <-ms.Errors():
				if err != nil {
					a.Log.WithFields(logx.Pairs("sub_id", subID, "err", err.Error())).Warn("风控订单簿 WS：MarketStream 报错")
				}
			}
		}
	}()

	if len(assetIDs) == 0 {
		a.Log.WithFields(logx.Pairs("sub_id", subID, "tokens", len(tokens))).Warn("风控订单簿 WS：无可订阅的 asset id")
		onDisconnect()
		return
	}

	if err := ms.Subscribe(assetIDs...); err != nil {
		a.Log.WithFields(logx.Pairs("sub_id", subID, "err", err)).Warn("风控订单簿 WS：订阅失败")
		onDisconnect()
		return
	}

	a.Log.WithFields(logx.Pairs("sub_id", subID, "tokens", len(tokens))).Info("风控订单簿 WS：连接已激活并完成订阅")
	a.Risk.SetOrderbookWSState(true, true)
	a.Hub.BroadcastJSON(map[string]any{"type": "poly_status", "polyOrderbookConnected": true})
	a.RiskHub.BroadcastJSON(map[string]any{"type": "poly_status", "polyOrderbookConnected": true})

	<-ctx.Done()
	onDisconnect()
	time.Sleep(2 * time.Second)
}

func chunkSlice(slice []string, chunkSize int) [][]string {
	var chunks [][]string
	for i := 0; i < len(slice); i += chunkSize {
		end := i + chunkSize
		if end > len(slice) {
			end = len(slice)
		}
		chunks = append(chunks, slice[i:end])
	}
	return chunks
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
