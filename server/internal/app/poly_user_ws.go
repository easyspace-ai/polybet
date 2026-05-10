package app

import (
	"context"
	polymarket "github.com/easyspace-ai/polysdk"
	"time"

	"github.com/easyspace-ai/polybet/internal/service/polysession"
)

func (a *App) polyUserWSLoop(ctx context.Context) {
	defer a.wg.Done()
	for {
		if ctx.Err() != nil {
			return
		}
		cl, err := polysession.ResolveAuthedCLOB(context.Background(), a.Cfg, a.Store)
		if err != nil {
			a.Log.Warn("poly_user_ws_session", "err", err.Error())
			a.Risk.SetUserWSState(false, false, err.Error())
			time.Sleep(5 * time.Second)
			continue
		}
		if cl.APIKey == nil {
			a.Log.Warn("poly_user_ws_missing_api_key")
			a.Risk.SetUserWSState(false, false, "missing_api_key")
			time.Sleep(5 * time.Second)
			continue
		}
		pc := polymarket.DefaultConfig()
		pc.BaseURLs.CLOB = a.Cfg.PolymarketAPIURL
		if a.Cfg.PolymarketCLOBWS != "" {
			pc.BaseURLs.CLOBWS = a.Cfg.PolymarketCLOBWS
		}
		opts := []polymarket.Option{polymarket.WithConfig(pc)}
		if a.Cfg.HTTPPlatformProxy != "" {
			opts = append(opts, polymarket.WithProxyURL(a.Cfg.HTTPPlatformProxy))
		}
		root, err := polymarket.NewClientE(opts...)
		if err != nil {
			a.Log.Warn("poly_user_ws_client", "err", err.Error())
			a.Risk.SetUserWSState(false, false, err.Error())
			time.Sleep(3 * time.Second)
			continue
		}
		authRoot := root.WithAuth(cl.Signer, cl.APIKey)
		subCtx, cancel := context.WithCancel(ctx)
		a.Risk.SetUserWSState(true, false, "")
		a.Log.Info("poly_user_ws_subscribing")
		ch, err := authRoot.CLOBWS.SubscribeUserTrades(subCtx, nil)
		if err != nil {
			cancel()
			a.Log.Warn("poly_user_ws_subscribe_failed", "err", err.Error())
			a.Risk.SetUserWSState(false, false, err.Error())
			time.Sleep(3 * time.Second)
			continue
		}
		a.Risk.SetUserWSState(false, true, "")
		a.Log.Info("poly_user_ws_connected")
		for {
			select {
			case <-subCtx.Done():
				cancel()
				a.Log.Info("poly_user_ws_ctx_done", "reason", "disconnected")
				a.Risk.SetUserWSState(false, false, "disconnected")
				goto reconnect
			case ev, ok := <-ch:
				if !ok {
					cancel()
					a.Log.Warn("poly_user_ws_channel_closed")
					a.Risk.SetUserWSState(false, false, "channel_closed")
					goto reconnect
				}
				a.Risk.TouchUserWSMessage()
				applied, err := a.Risk.ApplyClobTradeIfNew(context.Background(), struct {
					ID, AssetID, Side, Size, Price, Status string
					Market, Outcome                        string
				}{
					ID: ev.ID, AssetID: ev.AssetID, Side: ev.Side, Size: ev.Size, Price: ev.Price,
					Status: ev.Status, Market: ev.Market, Outcome: "",
				})
				if err != nil {
					a.Log.Warn("poly_user_ws_trade_apply_err", "trade_id", ev.ID, "asset_id", ev.AssetID, "status", ev.Status, "err", err.Error())
				} else if applied {
					a.Log.Info("poly_user_ws_trade_applied", "trade_id", ev.ID, "asset_id", ev.AssetID, "side", ev.Side, "size", ev.Size, "price", ev.Price, "status", ev.Status)
				} else {
					a.Log.Debug("poly_user_ws_trade_skip", "trade_id", ev.ID, "status", ev.Status)
				}
			}
		}
	reconnect:
		a.Log.Info("poly_user_ws_reconnect_wait", "delay_sec", 2)
		time.Sleep(2 * time.Second)
	}
}
