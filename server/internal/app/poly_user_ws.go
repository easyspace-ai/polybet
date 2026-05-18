package app

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/easyspace-ai/polybet/internal/logx"
	"github.com/easyspace-ai/polybet/internal/marketstream"
	"github.com/easyspace-ai/polybet/internal/memcache"
	"github.com/easyspace-ai/polybet/internal/service/polysession"
	"github.com/easyspace-ai/polybet/internal/service/risksvc"
	"github.com/easyspace-ai/polybet/internal/wsconfig"
)

func (a *App) polyUserWSLoop(ctx context.Context) {
	defer a.wg.Done()
	for {
		if ctx.Err() != nil {
			return
		}
		cl, err := polysession.ResolveAuthedCLOB(ctx, a.Cfg, a.Store)
		if err != nil {
			a.Log.WithFields(logx.Pairs("err", err.Error())).Warn("用户 CLOB WS：会话解析失败")
			a.Risk.SetUserWSState(false, false, err.Error())
			st := map[string]any{"type": "poly_status", "polyUserConnected": false}
			a.Hub.BroadcastJSON(st)
			a.RiskHub.BroadcastJSON(st)
			if a.RiskRuntime != nil {
				a.RiskRuntime.Publish("transport", "warn", "ws.user.session_error", "", "", "", "", map[string]any{"phase": "resolve_clob", "err": err.Error()})
			}
			if a.LogService != nil {
				a.LogService.Warn("风控", fmt.Sprintf("WS会话解析失败: %s", err.Error()))
			}
			sleepCtx(ctx, 5*time.Second)
			continue
		}
		if cl.APIKey == nil {
			a.Log.Warn("用户 CLOB WS：缺少 API Key")
			a.Risk.SetUserWSState(false, false, "missing_api_key")
			st := map[string]any{"type": "poly_status", "polyUserConnected": false}
			a.Hub.BroadcastJSON(st)
			a.RiskHub.BroadcastJSON(st)
			if a.RiskRuntime != nil {
				a.RiskRuntime.Publish("transport", "warn", "ws.user.session_error", "", "", "", "", map[string]any{"phase": "missing_api_key"})
			}
			sleepCtx(ctx, 5*time.Second)
			continue
		}

		msCfg := a.polymarketMarketstreamConfig()
		msCfg.OnReconnectScheduled = a.clobOnReconnectScheduled("user")
		creds := &marketstream.APICreds{
			APIKey:        cl.APIKey.Key,
			APISecret:     cl.APIKey.Secret,
			APIPassphrase: cl.APIKey.Passphrase,
		}
		user := marketstream.NewUserStreamWithConfig(creds, msCfg)
		a.setActiveUserStream(user)

		subCtx, cancel := context.WithCancel(ctx)

		a.Risk.SetUserWSState(true, false, "")
		acct, _ := a.Store.GetActivePolymarketAccount(context.Background())
		accountID := ""
		if acct != nil {
			accountID = acct.ID
		}
		if a.RiskRuntime != nil {
			a.RiskRuntime.Publish("transport", "info", "ws.user.connecting", accountID, "", "", "", map[string]any{"urlHost": msCfg.UserWSURL})
		}
		a.Log.WithFields(logx.Pairs(
			"ws_url", msCfg.UserWSURL,
			"api_key_id", cl.APIKey.Key,
			"api_key_has_secret", cl.APIKey.Secret != "",
			"proxy", a.Cfg.HTTPPlatformProxy,
			"proxy_set", a.Cfg.HTTPPlatformProxy != "",
		)).Info("用户 CLOB WS：正在连接")

		user.OnUserTrade(func(ev marketstream.UserTradeEvent) {
			if subCtx.Err() != nil {
				return
			}
			a.Risk.TouchUserWSMessage()
			acct, _ := a.Store.GetActivePolymarketAccount(context.Background())
			accountID := ""
			if acct != nil {
				accountID = acct.ID
			}
			rawAsset := strings.TrimSpace(ev.AssetID)
			if rawAsset == "" {
				rawAsset = strings.TrimSpace(ev.Asset)
			}
			assetID := strings.ToLower(rawAsset)
			if assetID != "" && !strings.HasPrefix(assetID, "0x") {
				assetID = "0x" + assetID
			}
			if len(assetID) < 66 && strings.HasPrefix(assetID, "0x") {
				assetID = "0x" + strings.Repeat("0", 66-len(assetID)) + assetID[2:]
			}

			tradeID := strings.TrimSpace(ev.ID)
			if tradeID == "" {
				tradeID = strings.TrimSpace(ev.TransactionHash)
			}

			sizeStr := strconv.FormatFloat(ev.Size.Float64(), 'f', -1, 64)
			priceStr := strconv.FormatFloat(ev.Price.Float64(), 'f', -1, 64)

			applied, err := a.Risk.ApplyClobTradeIfNew(context.Background(), struct {
				ID, AssetID, Side, Size, Price, Status string
				Market, Outcome                        string
			}{
				ID: tradeID, AssetID: assetID, Side: ev.Side, Size: sizeStr, Price: priceStr,
				Status: ev.Status, Market: ev.Market, Outcome: "",
			}, accountID)
			if err != nil {
				a.Log.WithFields(logx.Pairs("trade_id", tradeID, "asset_id", assetID, "status", ev.Status, "err", err.Error())).Warn("用户 CLOB WS：应用成交失败")
				if a.LogService != nil {
					a.LogService.Error("风控", fmt.Sprintf("应用 CLOB 交易失败: %s", err.Error()))
				}
			} else if applied {
				wsFields := logx.Pairs("trade_id", tradeID, "asset_id", assetID, "side", ev.Side, "size", sizeStr, "price", priceStr, "status", ev.Status)
				a.Log.WithFields(wsFields).Info("用户 CLOB WS：成交已入账")
				logx.Trade().WithFields(wsFields).Info("用户 CLOB WS：成交已入账")
				logx.Open().WithFields(wsFields).Info("用户 CLOB WS：成交已入账")
				if a.LogService != nil {
					a.LogService.Info("交易", fmt.Sprintf("CLOB 成交: %s %s $%s @ $%s", ev.Side, assetID, sizeStr, priceStr))
				}
				if a.RiskRuntime != nil && accountID != "" {
					a.RiskRuntime.Publish("position", "info", "order.execution_summary", accountID, "", assetID, tradeID, map[string]any{
						"tradeId": tradeID, "side": ev.Side, "size": sizeStr, "price": priceStr, "status": ev.Status,
					})
				}
				a.broadcastPositionSnapshotFast()
				a.Debounce.Trigger("risk_cache_rebuild", func() {
					if syncErr := a.Risk.SyncPositionsFromDataAPI(context.Background(), accountID); syncErr != nil {
						syncFields := logx.Pairs("err", syncErr.Error(), "trade_id", tradeID)
						a.Log.WithFields(syncFields).Warn("用户 CLOB WS：成交后同步持仓失败")
						logx.Position().WithFields(syncFields).Warn("用户 CLOB WS：成交后同步持仓失败")
					}
					a.rebuildAndBroadcastCache()
				})
			} else {
				a.Log.WithFields(logx.Pairs("trade_id", tradeID, "status", ev.Status)).Debug("用户 CLOB WS：跳过重复或非新成交")
			}
		})

		if err := user.Start(subCtx); err != nil {
			user.Stop()
			cancel()
			a.Log.WithFields(logx.Pairs("err", err.Error())).Warn("用户 CLOB WS：启动失败")
			a.Risk.SetUserWSState(false, false, err.Error())
			st := map[string]any{"type": "poly_status", "polyUserConnected": false}
			a.Hub.BroadcastJSON(st)
			a.RiskHub.BroadcastJSON(st)
			if a.RiskRuntime != nil {
				a.RiskRuntime.Publish("transport", "warn", "ws.user.disconnected", accountID, "", "", "", map[string]any{"phase": "start_failed", "err": err.Error()})
			}
			sleepCtx(ctx, 3*time.Second)
			continue
		}

		if err := user.SubscribeAll(); err != nil {
			user.Stop()
			cancel()
			a.Log.WithFields(logx.Pairs("err", err.Error())).Warn("用户 CLOB WS：订阅失败")
			a.Risk.SetUserWSState(false, false, err.Error())
			st := map[string]any{"type": "poly_status", "polyUserConnected": false}
			a.Hub.BroadcastJSON(st)
			a.RiskHub.BroadcastJSON(st)
			if a.RiskRuntime != nil {
				a.RiskRuntime.Publish("transport", "warn", "ws.user.disconnected", accountID, "", "", "", map[string]any{"phase": "subscribe_failed", "err": err.Error()})
			}
			sleepCtx(ctx, 3*time.Second)
			continue
		}

		go func() {
			for {
				select {
				case <-subCtx.Done():
					return
				case err := <-user.Errors():
					if err == nil {
						continue
					}
					a.Log.WithFields(logx.Pairs("err", err.Error())).Warn("用户 CLOB WS：底层连接报错")
					if a.RiskRuntime != nil {
						a.RiskRuntime.Publish("transport", "warn", "ws.user.error", accountID, "", "", "", map[string]any{"err": err.Error()})
					}
					if a.Risk.WSMeta != nil {
						a.Risk.WSMeta.Record("user", "warn", err.Error())
					}
					a.broadcastPolyStatus()
				}
			}
		}()

		a.Risk.SetUserWSState(false, true, "")
		if a.Risk.WSMeta != nil {
			a.Risk.WSMeta.ClearReconnectSchedule("user")
			a.Risk.WSMeta.Record("user", "info", "connected")
		}
		a.StopLoss.NotifyPositionsChanged()
		a.broadcastPolyStatus()
		if a.RiskRuntime != nil {
			a.RiskRuntime.Publish("transport", "info", "ws.user.connected", accountID, "", "", "", map[string]any{})
		}
		a.Log.Info("用户 CLOB WS：已连接")
		if a.LogService != nil {
			a.LogService.Info("WebSocket", "用户 CLOB 连接成功")
		}

		<-subCtx.Done()
		user.Stop()
		a.clearActiveUserStream()
		cancel()

		a.Log.WithFields(logx.Pairs("reason", "disconnected")).Info("用户 CLOB WS：连接已结束")
		a.Risk.SetUserWSState(false, false, "disconnected")
		if a.Risk.WSMeta != nil {
			a.Risk.WSMeta.Record("user", "warn", "disconnected")
		}
		a.broadcastPolyStatus()
		if a.RiskRuntime != nil {
			a.RiskRuntime.Publish("transport", "info", "ws.user.disconnected", accountID, "", "", "", map[string]any{"reason": "disconnected"})
		}
		if a.LogService != nil {
			a.LogService.Warn("WebSocket", "用户 CLOB 断开")
		}

		if ctx.Err() != nil {
			return
		}

		ws := wsconfig.Load(ctx, a.Store)
		delay := time.Duration(ws.ClobBackoffBaseSec) * time.Second
		if a.RiskRuntime != nil {
			a.RiskRuntime.Publish("transport", "info", "ws.user.reconnect_scheduled", accountID, "", "", "", map[string]any{"backoffMs": delay.Milliseconds()})
		}
		a.Log.WithFields(logx.Pairs("delay_sec", delay.Seconds())).Info("用户 CLOB WS：等待重连")
		sleepCtx(ctx, delay)
	}
}

func (a *App) rebuildAndBroadcastCache() {
	if a == nil {
		return
	}
	a.invalidateOpenPosCount()
	meta := risksvc.Meta{OutboundProxyConfigured: a.Cfg.HTTPPlatformProxy != ""}
	acct, _ := a.Store.GetActivePolymarketAccount(context.Background())
	accountID := ""
	if acct != nil {
		accountID = acct.ID
	}

	rows, enrichedMeta, err := a.Risk.ListRiskPositionsEnriched(context.Background(), meta, accountID)
	if err == nil {
		oldRows, _, found, _ := a.RiskCache.Get(context.Background())
		shouldBroadcast := !found || !positionsStructurallyEqual(oldRows, rows)
		_ = a.RiskCache.Set(context.Background(), memcache.RiskFetchResult{Positions: rows, Meta: memcache.RiskMeta{
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
			if a.RiskRuntime != nil {
				a.RiskRuntime.Publish("position", "info", "position.snapshot_changed", accountID, "", "", "", map[string]any{
					"openCount": countOpenRiskRows(rows),
				})
			}
		}
	}

	a.scheduleBalanceBroadcast()
}

func countOpenRiskRows(rows []map[string]any) int {
	n := 0
	for _, r := range rows {
		if s, _ := r["status"].(string); s == "open" {
			n++
		}
	}
	return n
}

// positionsStructurallyEqual compares two position lists ignoring volatile fields
// like currentCents / trailingStopCents / pnlUsd which are computed from live orderbook.
// highWaterCents and stopLossPct are included so ratcheting high water and PATCH edits
// still emit position_update (dashboard refetches / draft sync).
func positionsStructurallyEqual(a, b []map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	buildKey := func(rows []map[string]any) string {
		parts := make([]string, 0, len(rows))
		for _, r := range rows {
			tid, _ := r["tokenId"].(string)
			parts = append(parts, fmt.Sprintf("%s:%v:%v:%v:%v", tid, r["sizeShares"], r["status"], r["highWaterCents"], r["stopLossPct"]))
		}
		sort.Strings(parts)
		return strings.Join(parts, "|")
	}
	return buildKey(a) == buildKey(b)
}
