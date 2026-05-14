package app

import (
	"context"
	"strings"
	polymarket "github.com/easyspace-ai/polysdk"
	"strconv"
	"time"

	"github.com/easyspace-ai/polysdk/pkg/clob/ws"
)

type tokenIDStore interface {
	ListPolymarketOutcomeTokenIDs(ctx context.Context) ([]string, error)
	ListOpenRiskPositionTokenIDs(ctx context.Context) ([]string, error)
}

func buildTokenIDs(st tokenIDStore) ([]string, []string, error) {
	ids, err := st.ListPolymarketOutcomeTokenIDs(context.Background())
	if err != nil {
		return nil, nil, err
	}
	riskIds, err := st.ListOpenRiskPositionTokenIDs(context.Background())
	if err != nil {
		return ids, nil, err
	}
	if len(riskIds) == 0 {
		return ids, nil, nil
	}
	set := make(map[string]struct{}, len(ids)+len(riskIds))
	merged := make([]string, 0, len(ids)+len(riskIds))
	for _, id := range riskIds {
		if _, ok := set[id]; !ok {
			set[id] = struct{}{}
			merged = append(merged, id)
		}
	}
	for _, id := range ids {
		if _, ok := set[id]; !ok {
			set[id] = struct{}{}
			merged = append(merged, id)
		}
	}
	return merged, riskIds, nil
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
	pc := polymarket.DefaultConfig()
	pc.BaseURLs.CLOB = a.Cfg.PolymarketAPIURL
	opts := []polymarket.Option{polymarket.WithConfig(pc)}
	if a.Cfg.HTTPPlatformProxy != "" {
		opts = append(opts, polymarket.WithProxyURL(a.Cfg.HTTPPlatformProxy))
	}
	root, err := polymarket.NewClientE(opts...)
	if err != nil {
		a.Log.Error("polymarket client", "err", err)
		return
	}
	for {
		if ctx.Err() != nil {
			return
		}
		ids, riskIds, err := buildTokenIDs(a.Store)
		if err != nil {
			a.Log.Warn("poly_ws_list_token_ids", "err", err)
			time.Sleep(5 * time.Second)
			continue
		}
		if len(ids) == 0 {
			a.Log.Debug("poly_ws_idle", "reason", "no polymarket outcome tokens in DB yet")
			time.Sleep(5 * time.Second)
			continue
		}
		if len(ids) > 50 {
			a.Log.Warn("poly_ws_truncated", "total", len(ids), "kept", 50, "risk_count", len(riskIds))
			ids = ids[:50]
		}
		a.Log.Info("poly_ws_subscribe", "token_count", len(ids), "risk_count", len(riskIds), "risk_tokens", strings.Join(riskIds, ","))
		subCtx, cancel := context.WithCancel(ctx)
		// Polymarket orderbook WS expects token IDs without 0x prefix.
		subIds := make([]string, len(ids))
		for i, id := range ids {
			subIds[i] = strings.TrimPrefix(id, "0x")
		}
		ch, err := root.CLOBWS.SubscribeOrderbook(subCtx, subIds)
		if err != nil {
			a.Log.Warn("poly_ws_subscribe", "token_count", len(ids), "err", err)
			a.Risk.SetOrderbookWSState(false, false)
			a.Hub.BroadcastJSON(map[string]any{"type": "poly_status", "polyOrderbookConnected": false})
			cancel()
			time.Sleep(3 * time.Second)
			continue
		}
		a.Log.Info("poly_ws_subscribed", "token_count", len(ids))
		a.Risk.SetOrderbookWSState(false, true)
		a.Hub.BroadcastJSON(map[string]any{"type": "poly_status", "polyOrderbookConnected": true})
		if a.LogService != nil {
			a.LogService.Info("WebSocket", "Polymarket 盘口连接成功")
		}

		ticker := time.NewTicker(30 * time.Second)
		for {
			select {
			case <-subCtx.Done():
				cancel()
				ticker.Stop()
				goto reconnect
			case <-ticker.C:
				newIds, newRiskIds, err := buildTokenIDs(a.Store)
				if err != nil {
					a.Log.Warn("poly_ws_refresh_ids_err", "err", err)
					continue
				}
				if len(newIds) > 50 {
					newIds = newIds[:50]
				}
				if !sameStringSlice(ids, newIds) {
					a.Log.Info("poly_ws_ids_changed", "old", len(ids), "new", len(newIds), "new_risk", len(newRiskIds))
					cancel()
					ticker.Stop()
					goto reconnect
				}
			case ev, ok := <-ch:
				if !ok {
					cancel()
					ticker.Stop()
					goto reconnect
				}
				ts := parseWSTS(ev.Timestamp)
				assetID := strings.ToLower(ev.AssetID)
				if !strings.HasPrefix(assetID, "0x") {
					assetID = "0x" + assetID
				}
				if len(assetID) < 66 {
					assetID = "0x" + strings.Repeat("0", 66-len(assetID)) + assetID[2:]
				}
				a.Cache.ReplaceBook(assetID, toWSLevels(ev.Bids), toWSLevels(ev.Asks), ts)
				levels := a.Cache.SnapshotLevels(assetID)
				bestBid, bestAsk, _ := a.Cache.TopOfBook(assetID)
				bidCents := 0.0
				askCents := 0.0
				if bestBid > 0 {
					bidCents = bestBid * 100
				}
				if bestAsk > 0 {
					askCents = bestAsk * 100
				}
				a.Hub.BroadcastJSON(map[string]any{
					"type":     "polyBookUpdate",
					"tokenId":  assetID,
					"levels":   levels,
					"bestBid":  bidCents,
					"bestAsk":  askCents,
				})
				a.Debounce.Trigger(assetID, func() {
					err := a.Risk.RiskEvaluateTokenAfterBookUpdate(context.Background(), assetID)
					if err != nil {
						a.Log.Warn("risk_eval_after_book", "token", assetID, "err", err)
					}
					a.rebuildAndBroadcastCache()
				})
			}
		}
	reconnect:
		a.Risk.SetOrderbookWSState(true, false)
		a.Hub.BroadcastJSON(map[string]any{"type": "poly_status", "polyOrderbookConnected": false})
		if a.LogService != nil {
			a.LogService.Warn("WebSocket", "Polymarket 盘口断开, 正在重连...")
		}
		time.Sleep(2 * time.Second)
	}
}

func toWSLevels(in []ws.OrderbookLevel) []struct{ Price, Size string } {
	out := make([]struct{ Price, Size string }, 0, len(in))
	for _, x := range in {
		out = append(out, struct{ Price, Size string }{Price: x.Price, Size: x.Size})
	}
	return out
}

func parseWSTS(raw string) int64 {
	if raw == "" {
		return time.Now().UnixMilli()
	}
	// numeric string ms
	if ms, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return ms
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t.UnixMilli()
	}
	return time.Now().UnixMilli()
}
