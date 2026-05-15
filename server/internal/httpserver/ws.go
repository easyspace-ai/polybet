package httpserver

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"

	"github.com/easyspace-ai/polybet/internal/logx"
	"github.com/easyspace-ai/polybet/internal/service/marketsvc"
	"github.com/easyspace-ai/polybet/internal/service/risksvc"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
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

func (h *Handler) handleWSRisk(c *gin.Context) {
	rid := c.GetString("request_id")
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logrus.WithFields(logx.Pairs("request_id", rid, "err", err.Error())).Warn("WebSocket 风控：升级协议失败")
		return
	}
	ra := ""
	if conn.RemoteAddr() != nil {
		ra = conn.RemoteAddr().String()
	}
	logrus.WithFields(logx.Pairs("request_id", rid, "remote_addr", ra)).Info("WebSocket 风控：客户端已连接")
	if h.logService != nil {
		h.logService.Info("WebSocket", "Risk 监控连接: "+ra)
	}
	h.riskHub.Register(conn)
	defer func() {
		h.riskHub.Unregister(conn)
		logrus.WithFields(logx.Pairs("request_id", rid, "remote_addr", ra)).Info("WebSocket 风控：客户端已断开")
		if h.logService != nil {
			h.logService.Info("WebSocket", "Risk 监控断开: "+ra)
		}
	}()

	// 发送初始连接状态
	_ = h.riskHub.WriteJSON(conn, map[string]any{
		"type":                   "poly_status",
		"polyOrderbookConnected": h.risk.OrderbookWSConnected(),
		"polyUserConnected":      h.risk.UserWSConnected(),
	})

	// 发送初始仓位快照
	meta := risksvc.Meta{OutboundProxyConfigured: h.cfg.HTTPPlatformProxy != ""}
	acct, _ := h.st.GetActivePolymarketAccount(c)
	accountID := ""
	if acct != nil {
		accountID = acct.ID
	}
	rows, _, err := h.risk.ListRiskPositionsEnriched(c, meta, accountID)
	if err == nil {
		_ = h.riskHub.WriteJSON(conn, map[string]any{"type": "position_update", "data": rows})
	}

	if h.riskRuntime != nil {
		if snap := h.riskRuntime.ListChronological(500); len(snap) > 0 {
			_ = h.riskHub.WriteJSON(conn, map[string]any{"type": "risk_runtime_log_snapshot", "data": snap})
		}
	}

	conn.SetReadDeadline(time.Now().Add(120 * time.Second))
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			logrus.WithFields(logx.Pairs("request_id", rid, "ws_channel", "risk_ws", "remote_addr", ra, "err", err.Error())).Info("WebSocket 风控：读循环结束")
			return
		}
		var m map[string]any
		if err := json.Unmarshal(msg, &m); err != nil {
			continue
		}
		ty, _ := m["type"].(string)
		switch ty {
		case "ping":
			conn.SetReadDeadline(time.Now().Add(120 * time.Second))
			continue
		case "subscribePolyBook":
			tid, _ := m["tokenId"].(string)
			originalTid := tid
			tid = normalizeTokenID(tid)
			if tid != "" {
				bids, asks := h.cache.GetBidsAsks(tid, 5)
				bestBid, bestAsk, _ := h.cache.TopOfBook(tid)
				logrus.WithFields(logx.Pairs("request_id", rid, "token_id", tid, "original_id", originalTid, "has_data", len(bids) > 0 || len(asks) > 0)).Info("WebSocket 风控：订阅订单簿")
				bidCents := 0.0
				askCents := 0.0
				if bestBid > 0 {
					bidCents = bestBid * 100
				}
				if bestAsk > 0 {
					askCents = bestAsk * 100
				}
				_ = h.riskHub.WriteJSON(conn, map[string]any{
					"type":    "polyBookSnapshot",
					"tokenId": tid,
					"bids":    bids,
					"asks":    asks,
					"bestBid": bidCents,
					"bestAsk": askCents,
				})
			}
		}
	}
}

func registerWS(r *gin.Engine, d Deps) {
	r.GET("/ws", func(c *gin.Context) {
		rid := c.GetString("request_id")
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			logrus.WithFields(logx.Pairs("request_id", rid, "err", err.Error())).Warn("WebSocket Dashboard：升级协议失败")
			return
		}
		ra := ""
		if conn.RemoteAddr() != nil {
			ra = conn.RemoteAddr().String()
		}
		logrus.WithFields(logx.Pairs("request_id", rid, "remote_addr", ra)).Info("WebSocket Dashboard：客户端已连接")
		if d.LogService != nil {
			d.LogService.Info("WebSocket", "Dashboard 连接: "+ra)
		}
		d.Hub.Register(conn)
		defer func() {
			d.Hub.Unregister(conn)
			logrus.WithFields(logx.Pairs("request_id", rid, "remote_addr", ra)).Info("WebSocket Dashboard：客户端已断开")
			if d.LogService != nil {
				d.LogService.Info("WebSocket", "Dashboard 断开: "+ra)
			}
		}()

		var sportIcons map[string]string
		if sports, err := d.SportsCache.Get(c); err == nil {
			sportIcons = marketsvc.BuildSportIconMap(sports)
		}
		meta := risksvc.Meta{OutboundProxyConfigured: d.Cfg.HTTPPlatformProxy != ""}
		if markets, err := marketsvc.BuildMarketsPayload(c, d.Store, d.Cache, sportIcons); err == nil {
			_ = d.Hub.WriteJSON(conn, map[string]any{"type": "marketsSnapshot", "data": markets})
			logrus.WithFields(logx.Pairs("request_id", rid, "markets_snapshot", len(markets))).Info("WebSocket Dashboard：已发送市场握手快照")
		} else {
			logrus.WithFields(logx.Pairs("request_id", rid, "err", err.Error())).Warn("WebSocket Dashboard：市场快照构建失败")
		}
		_ = d.Hub.WriteJSON(conn, map[string]any{"type": "snapshot", "data": []any{}})
		acct, _ := d.Store.GetActivePolymarketAccount(c)
		accountID := ""
		if acct != nil {
			accountID = acct.ID
		}
		_, _, _ = d.Risk.ListRiskPositionsEnriched(c, meta, accountID)

		conn.SetReadDeadline(time.Now().Add(120 * time.Second))
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				logrus.WithFields(logx.Pairs("request_id", rid, "ws_channel", "dashboard", "remote_addr", ra, "err", err.Error())).Info("WebSocket Dashboard：读循环结束")
				return
			}
			var m map[string]any
			if err := json.Unmarshal(msg, &m); err != nil {
				logrus.WithFields(logx.Pairs("request_id", rid, "err", err.Error())).Debug("WebSocket Dashboard：无法解析的帧")
				continue
			}
			ty, _ := m["type"].(string)
			switch ty {
			case "ping":
				conn.SetReadDeadline(time.Now().Add(120 * time.Second))
				continue
			case "subscribePolyBook":
				tid, _ := m["tokenId"].(string)
				tid = strings.ToLower(strings.TrimSpace(tid))
				if tid != "" {
					logrus.WithFields(logx.Pairs("request_id", rid, "token_id", tid)).Info("WebSocket Dashboard：订阅订单簿")
					bids, asks := d.Cache.GetBidsAsks(tid, 5)
					bestBid, bestAsk, _ := d.Cache.TopOfBook(tid)
					bidCents := 0.0
					askCents := 0.0
					if bestBid > 0 {
						bidCents = bestBid * 100
					}
					if bestAsk > 0 {
						askCents = bestAsk * 100
					}
					_ = d.Hub.WriteJSON(conn, map[string]any{
						"type":    "polyBookSnapshot",
						"tokenId": tid,
						"bids":    bids,
						"asks":    asks,
						"bestBid": bidCents,
						"bestAsk": askCents,
					})
				}
			case "subscribePolyOdds":
				if raw, ok := m["tokenIds"].([]any); ok {
					logrus.WithFields(logx.Pairs("request_id", rid, "token_count", len(raw))).Info("WebSocket Dashboard：订阅隐含赔率")
					var entries []map[string]any
					for _, x := range raw {
						tid, _ := x.(string)
						if tid == "" {
							continue
						}
						if o, ok := d.Cache.TakerOdds(tid); ok {
							entries = append(entries, map[string]any{"tokenId": tid, "takerOdds": o, "updatedAt": time.Now().UnixMilli()})
						}
					}
					if len(entries) > 0 {
						_ = d.Hub.WriteJSON(conn, map[string]any{"type": "polyOddsSnapshot", "data": entries})
					}
				}
			default:
				if ty != "" {
					logrus.WithFields(logx.Pairs("request_id", rid, "type", ty)).Debug("WebSocket Dashboard：客户端消息")
				}
			}
			conn.SetReadDeadline(time.Now().Add(120 * time.Second))
		}
	})
}
