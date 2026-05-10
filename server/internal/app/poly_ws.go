package app

import (
	"context"
	polymarket "github.com/easyspace-ai/polysdk"
	"strconv"
	"time"

	"github.com/easyspace-ai/polysdk/pkg/clob/ws"
)

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
		ids, err := a.Store.ListPolymarketOutcomeTokenIDs(context.Background())
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
			ids = ids[:50]
		}
		subCtx, cancel := context.WithCancel(ctx)
		ch, err := root.CLOBWS.SubscribeOrderbook(subCtx, ids)
		if err != nil {
			a.Log.Warn("poly_ws_subscribe", "token_count", len(ids), "err", err)
			cancel()
			time.Sleep(3 * time.Second)
			continue
		}
		a.Log.Info("poly_ws_subscribed", "token_count", len(ids))
		for {
			select {
			case <-subCtx.Done():
				cancel()
				goto reconnect
			case ev, ok := <-ch:
				if !ok {
					cancel()
					goto reconnect
				}
				ts := parseWSTS(ev.Timestamp)
				a.Cache.ReplaceBook(ev.AssetID, toWSLevels(ev.Bids), toWSLevels(ev.Asks), ts)
				levels := a.Cache.SnapshotLevels(ev.AssetID)
				a.Hub.BroadcastJSON(map[string]any{"type": "polyBookUpdate", "tokenId": ev.AssetID, "levels": levels})
				tid := ev.AssetID
				a.Debounce.Trigger(tid, func() {
					_ = a.Risk.RiskEvaluateTokenAfterBookUpdate(context.Background(), tid)
				})
			}
		}
	reconnect:
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
