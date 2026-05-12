package app

import (
	"context"
	"fmt"
	polymarket "github.com/easyspace-ai/polysdk"
	"time"

	"github.com/easyspace-ai/polybet/internal/rediska"
	"github.com/easyspace-ai/polybet/internal/service/balancesvc"
	"github.com/easyspace-ai/polybet/internal/service/polysession"
	"github.com/easyspace-ai/polybet/internal/service/risksvc"
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
			if a.LogService != nil {
				a.LogService.Warn("风控", fmt.Sprintf("WS会话解析失败: %s", err.Error()))
			}
			time.Sleep(5 * time.Second)
			continue
		}
		if cl.APIKey == nil {
			a.Log.Warn("poly_user_ws_missing_api_key")
			a.Risk.SetUserWSState(false, false, "missing_api_key")
			if a.LogService != nil {
				a.LogService.Warn("风控", "WS会话缺少API Key")
			}
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
		a.Log.Info("poly_user_ws_client_created",
			"base_urls", fmt.Sprintf("clob=%s ws=%s", pc.BaseURLs.CLOB, pc.BaseURLs.CLOBWS),
			"proxy", a.Cfg.HTTPPlatformProxy,
			"proxy_set", a.Cfg.HTTPPlatformProxy != "")
		authRoot := root.WithAuth(cl.Signer, cl.APIKey)
		subCtx, cancel := context.WithCancel(ctx)
		a.Risk.SetUserWSState(true, false, "")
		a.Log.Info("poly_user_ws_subscribing",
			"clob_ws", pc.BaseURLs.CLOBWS,
			"api_key_id", cl.APIKey.Key,
			"api_key_has_secret", cl.APIKey.Secret != "",
			"signer_type", fmt.Sprintf("%T", cl.Signer))
		ch, err := authRoot.CLOBWS.SubscribeUserTrades(subCtx, nil)
		a.Log.Info("poly_user_ws_subscribe_returned",
			"err", func() string { if err != nil { return err.Error() }; return "nil" }(),
			"ch_nil", ch == nil,
			"ch_open", func() string {
				if ch == nil { return "N/A" }
				select {
				case _, ok := <-ch:
					if ok { return "has_data" }
					return "closed"
				default:
					return "blocking"
				}
			}())
		if err != nil {
			cancel()
			a.Log.Warn("poly_user_ws_subscribe_failed",
				"err", err.Error(),
				"ws_url", pc.BaseURLs.CLOBWS,
				"proxy", a.Cfg.HTTPPlatformProxy)
			a.Risk.SetUserWSState(false, false, err.Error())
			time.Sleep(3 * time.Second)
			continue
		}
		a.Risk.SetUserWSState(false, true, "")
		a.Log.Info("poly_user_ws_connected")
		if a.LogService != nil {
			a.LogService.Info("WebSocket", "用户 CLOB 连接成功")
		}
		for {
			select {
			case <-subCtx.Done():
				cancel()
				a.Log.Info("poly_user_ws_ctx_done", "reason", "disconnected")
				a.Risk.SetUserWSState(false, false, "disconnected")
				if a.LogService != nil {
					a.LogService.Warn("WebSocket", "用户 CLOB 断开")
				}
				goto reconnect
			case ev, ok := <-ch:
				if !ok {
					cancel()
					a.Log.Warn("poly_user_ws_channel_closed")
					a.Risk.SetUserWSState(false, false, "channel_closed")
					if a.LogService != nil {
						a.LogService.Warn("WebSocket", "用户 CLOB 通道关闭, 正在重连...")
					}
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
					if a.LogService != nil {
						a.LogService.Error("风控", fmt.Sprintf("应用 CLOB 交易失败: %s", err.Error()))
					}
				} else if applied {
					a.Log.Info("poly_user_ws_trade_applied", "trade_id", ev.ID, "asset_id", ev.AssetID, "side", ev.Side, "size", ev.Size, "price", ev.Price, "status", ev.Status)
					if a.LogService != nil {
						a.LogService.Info("交易", fmt.Sprintf("CLOB 成交: %s %s $%s @ $%s", ev.Side, ev.AssetID, ev.Size, ev.Price))
					}
					a.rebuildAndBroadcastCache()
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

func (a *App) rebuildAndBroadcastCache() {
	meta := risksvc.Meta{OutboundProxyConfigured: a.Cfg.HTTPPlatformProxy != ""}

	rows, enrichedMeta, err := a.Risk.ListRiskPositionsEnriched(context.Background(), meta)
	if err == nil {
		_ = a.RiskCache.Set(context.Background(), rediska.RiskFetchResult{Positions: rows, Meta: rediska.RiskMeta{
			UserWsConnected:         enrichedMeta.UserWsConnected,
			UserWsConnecting:        enrichedMeta.UserWsConnecting,
			OutboundProxyConfigured: enrichedMeta.OutboundProxyConfigured,
			MinOpenRiskShares:       enrichedMeta.MinOpenRiskShares,
		}})
		a.Hub.BroadcastJSON(map[string]any{"type": "position_update", "data": rows})
	}

	if summary, err := balancesvc.Fetch(context.Background(), a.Cfg, a.Store); err == nil {
		_ = a.BalanceCache.Set(context.Background(), summary)
		a.Hub.BroadcastJSON(map[string]any{"type": "balance_update", "data": summary})
	}
}

func (a *App) InvalidateAndRebuildCache() {
	meta := risksvc.Meta{OutboundProxyConfigured: a.Cfg.HTTPPlatformProxy != ""}

	a.BalanceCache.Invalidate(context.Background())
	a.RiskCache.Invalidate(context.Background())

	// Force reconcile before reading from DB so stale positions are closed.
	if err := a.Risk.ReconcileOpenRiskPositionsWithClobBalances(context.Background()); err != nil {
		a.Log.Warn("invalidate_reconcile_err", "err", err.Error())
	}

	rows, enrichedMeta, err := a.Risk.ListRiskPositionsEnriched(context.Background(), meta)
	if err == nil {
		_ = a.RiskCache.Set(context.Background(), rediska.RiskFetchResult{Positions: rows, Meta: rediska.RiskMeta{
			UserWsConnected:         enrichedMeta.UserWsConnected,
			UserWsConnecting:        enrichedMeta.UserWsConnecting,
			OutboundProxyConfigured: enrichedMeta.OutboundProxyConfigured,
			MinOpenRiskShares:       enrichedMeta.MinOpenRiskShares,
		}})
		a.Hub.BroadcastJSON(map[string]any{"type": "position_update", "data": rows})
	}

	if summary, err := balancesvc.Fetch(context.Background(), a.Cfg, a.Store); err == nil {
		_ = a.BalanceCache.Set(context.Background(), summary)
		a.Hub.BroadcastJSON(map[string]any{"type": "balance_update", "data": summary})
	}
}
