package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"

	"github.com/easyspace-ai/polybet/internal/bookcache"
	"github.com/easyspace-ai/polybet/internal/config"
	"github.com/easyspace-ai/polybet/internal/logx"
	"github.com/easyspace-ai/polybet/internal/memcache"
	"github.com/easyspace-ai/polybet/internal/polyexec"
	"github.com/easyspace-ai/polybet/internal/polywarm"
	"github.com/easyspace-ai/polybet/internal/service/marketsvc"
	"github.com/easyspace-ai/polybet/internal/service/risksvc"
	"github.com/easyspace-ai/polybet/internal/wsrelay"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type wsReadEndReason string

const (
	wsReadEndClientClosed wsReadEndReason = "client_closed"
	wsReadEndIdleTimeout  wsReadEndReason = "idle_timeout"
	wsReadEndError        wsReadEndReason = "error"
)

func classifyWSReadErr(err error) wsReadEndReason {
	if err == nil {
		return ""
	}
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) {
		return wsReadEndClientClosed
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return wsReadEndIdleTimeout
	}
	return wsReadEndError
}

func logWSReadLoopEnd(label, rid, channel, ra string, err error) {
	reason := classifyWSReadErr(err)
	fields := logx.Pairs("request_id", rid, "ws_channel", channel, "remote_addr", ra, "reason", string(reason))
	switch reason {
	case wsReadEndClientClosed:
		logrus.WithFields(fields).Debug(label + "：读循环结束")
	case wsReadEndIdleTimeout:
		logrus.WithFields(fields).Debug(label + "：读循环结束（空闲读超时）")
	default:
		if err != nil {
			fields["err"] = err.Error()
		}
		logrus.WithFields(fields).Warn(label + "：读循环异常结束")
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

const polyBookCacheFreshMaxAge = 30 * time.Second

func bookCacheHasData(cache *bookcache.Cache, tokenID string) bool {
	bids, asks := cache.GetBidsAsks(tokenID, 1)
	if len(bids) > 0 || len(asks) > 0 {
		return true
	}
	bb, ba, ok := cache.TopOfBook(tokenID)
	return ok && (bb > 0 || ba > 0)
}

func bookCacheNeedsRESTWarm(cache *bookcache.Cache, tokenID string) bool {
	if !bookCacheHasData(cache, tokenID) {
		return true
	}
	if age, ok := cache.BookAge(tokenID); !ok || age > polyBookCacheFreshMaxAge {
		return true
	}
	bb, ba, ok := cache.TopOfBook(tokenID)
	if !ok {
		return true
	}
	if bb <= 0 && ba > 0 && ba <= 0.02 {
		return true
	}
	bids, asks := cache.GetBidsAsks(tokenID, 2)
	return len(bids) == 0 && len(asks) == 0
}

func warmBookCacheFromREST(ctx context.Context, cfg *config.Config, cache *bookcache.Cache, tokenID string) (source string, ageMs int64, bestBid, bestAsk float64) {
	source = "rest"
	if err := polywarm.RefreshFromREST(ctx, cfg.PolymarketAPIURL, cfg.HTTPPlatformProxy, tokenID, cache); err != nil {
		source = "rest_error"
		logrus.WithFields(logx.Pairs("token_id", tokenID, "err", err.Error())).Debug("WebSocket：REST 预热订单簿失败")
	}
	if age, ok := cache.BookAge(tokenID); ok {
		ageMs = age.Milliseconds()
	}
	bestBid, bestAsk, _ = cache.TopOfBook(tokenID)
	return source, ageMs, bestBid, bestAsk
}

func logPolyBookSubscribe(rid, channel, tokenID, originalID string, cache *bookcache.Cache, subscribed bool) {
	bb, ba, _ := cache.TopOfBook(tokenID)
	ageMs := int64(-1)
	if age, ok := cache.BookAge(tokenID); ok {
		ageMs = age.Milliseconds()
	}
	fields := logx.Pairs(
		"request_id", rid,
		"channel", channel,
		"token_id", tokenID,
		"clob_token_dec", polyexec.CLOBAssetIDForAPI(tokenID),
		"has_data", bookCacheHasData(cache, tokenID),
		"needs_rest_warm", bookCacheNeedsRESTWarm(cache, tokenID),
		"best_bid", bb,
		"best_ask", ba,
		"book_age_ms", ageMs,
		"ensure_ob_subscribed", subscribed,
	)
	if originalID != "" && originalID != tokenID {
		fields["original_id"] = originalID
	}
	logrus.WithFields(fields).Info("WebSocket：订阅订单簿")
}

type polyBookApp interface {
	PolyBookClientSubscribe(tokenID string)
	PolyBookClientUnsubscribe(tokenID string)
}

func handlePolyBookSubscribe(app polyBookApp, connBooks map[string]struct{}, tid string) {
	if tid == "" || app == nil {
		return
	}
	if _, ok := connBooks[tid]; ok {
		return
	}
	connBooks[tid] = struct{}{}
	app.PolyBookClientSubscribe(tid)
}

func handlePolyBookUnsubscribe(app polyBookApp, connBooks map[string]struct{}, tid string) {
	if tid == "" || app == nil {
		return
	}
	if _, ok := connBooks[tid]; !ok {
		return
	}
	delete(connBooks, tid)
	app.PolyBookClientUnsubscribe(tid)
}

func releaseConnPolyBooks(app polyBookApp, connBooks map[string]struct{}) {
	if app == nil {
		return
	}
	for tid := range connBooks {
		app.PolyBookClientUnsubscribe(tid)
	}
}

// asyncSeedBookAndPushSnapshot avoids blocking the WS read loop on CLOB REST; pushes a second snapshot after warm.
func asyncSeedBookAndPushSnapshot(ctx context.Context, cfg *config.Config, cache *bookcache.Cache, hub *wsrelay.Hub, conn *websocket.Conn, tokenID string, app bookSummaryPublisher) {
	if !bookCacheNeedsRESTWarm(cache, tokenID) {
		return
	}
	go func() {
		bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		src, ageMs, bb, ba := warmBookCacheFromREST(bg, cfg, cache, tokenID)
		if !bookCacheHasData(cache, tokenID) {
			return
		}
		logrus.WithFields(logx.Pairs(
			"token_id", tokenID,
			"book_source", src,
			"book_age_ms", ageMs,
			"best_bid", bb,
			"best_ask", ba,
		)).Info("WebSocket：订单簿 REST 预热完成")
		_ = sendPolyBookSnapshot(conn, hub, cache, tokenID)
		afterPolyBookSnapshot(app, tokenID)
	}()
}

func sendPolyBookSnapshot(conn *websocket.Conn, hub *wsrelay.Hub, cache *bookcache.Cache, tokenID string) error {
	bids, asks := cache.GetBidsAsks(tokenID, 5)
	bestBid, bestAsk, _ := cache.TopOfBook(tokenID)
	bidCents := 0.0
	askCents := 0.0
	if bestBid > 0 {
		bidCents = polyexec.CentsFromPrice01(bestBid)
	}
	if bestAsk > 0 {
		askCents = polyexec.CentsFromPrice01(bestAsk)
	}
	msg := map[string]any{
		"type":    "polyBookSnapshot",
		"tokenId": tokenID,
		"bids":    bids,
		"asks":    asks,
		"bestBid": bidCents,
		"bestAsk": askCents,
	}
	if conn != nil {
		return hub.WriteJSON(conn, msg)
	}
	return nil
}

func afterPolyBookSnapshot(app bookSummaryPublisher, tokenID string) {
	if app == nil || tokenID == "" {
		return
	}
	app.PublishBookSummaryTick(tokenID)
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
	connPolyBooks := make(map[string]struct{})
	defer func() {
		releaseConnPolyBooks(h.app, connPolyBooks)
		h.riskHub.Unregister(conn)
		logrus.WithFields(logx.Pairs("request_id", rid, "remote_addr", ra)).Info("WebSocket 风控：客户端已断开")
		if h.logService != nil {
			h.logService.Info("WebSocket", "Risk 监控断开: "+ra)
		}
	}()

	if h.conn != nil {
		h.conn.SetRelayClients(h.hub.ClientCount() + h.riskHub.ClientCount())
		if snap := h.conn.Snapshot(); snap.ConnectivitySnapshotJSON() != nil {
			_ = h.riskHub.WriteJSON(conn, snap.ConnectivitySnapshotJSON())
		}
	}
	// Legacy poly_status for older clients
	_ = h.riskHub.WriteJSON(conn, map[string]any{
		"type":                    "poly_status",
		"polyOrderbookConnected":  h.risk.OrderbookWSConnected(),
		"polyOrderbookConnecting": h.risk.OrderbookWSConnecting(),
		"polyUserConnected":       h.risk.UserWSConnected(),
		"polyUserConnecting":      h.risk.UserWSConnecting(),
	})

	// 发送初始仓位快照（优先读缓存，避免 WS 连接阻塞在 enrich 上）
	if rows, _, found, _ := h.riskCache.Get(c); found {
		_ = h.riskHub.WriteJSON(conn, map[string]any{"type": "position_update", "data": rows})
	} else {
		meta := risksvc.Meta{OutboundProxyConfigured: h.cfg.HTTPPlatformProxy != ""}
		acct, _ := h.st.GetActivePolymarketAccount(c)
		accountID := ""
		if acct != nil {
			accountID = acct.ID
		}
		fetch := func(ctx context.Context) (memcache.RiskFetchResult, error) {
			return riskPositionsFetchResult(ctx, rid, h.risk, accountID, meta)
		}
		h.riskCache.RefreshAsync(fetch)
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
			logWSReadLoopEnd("WebSocket 风控", rid, "risk_ws", ra, err)
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
			_ = h.riskHub.WriteJSON(conn, map[string]any{"type": "pong"})
			continue
		case "subscribePolyBook":
			tid, _ := m["tokenId"].(string)
			originalTid := tid
			tid = normalizeTokenID(tid)
			if tid != "" {
				handlePolyBookSubscribe(h.app, connPolyBooks, tid)
				logPolyBookSubscribe(rid, "risk_ws", tid, originalTid, h.cache, h.app != nil)
				_ = sendPolyBookSnapshot(conn, h.riskHub, h.cache, tid)
				afterPolyBookSnapshot(h.app, tid)
				asyncSeedBookAndPushSnapshot(c.Request.Context(), h.cfg, h.cache, h.riskHub, conn, tid, h.app)
			}
		case "unsubscribePolyBook":
			tid, _ := m["tokenId"].(string)
			tid = normalizeTokenID(tid)
			if tid != "" {
				handlePolyBookUnsubscribe(h.app, connPolyBooks, tid)
				logrus.WithFields(logx.Pairs("request_id", rid, "channel", "risk_ws", "token_id", tid)).Info("WebSocket：取消订阅订单簿")
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
		connPolyBooks := make(map[string]struct{})
		defer func() {
			releaseConnPolyBooks(d.App, connPolyBooks)
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
				logWSReadLoopEnd("WebSocket Dashboard", rid, "dashboard", ra, err)
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
				_ = d.Hub.WriteJSON(conn, map[string]any{"type": "pong"})
				continue
			case "subscribePolyBook":
				tid, _ := m["tokenId"].(string)
				tid = normalizeTokenID(tid)
				if tid != "" {
					handlePolyBookSubscribe(d.App, connPolyBooks, tid)
					logPolyBookSubscribe(rid, "dashboard_ws", tid, "", d.Cache, d.App != nil)
					_ = sendPolyBookSnapshot(conn, d.Hub, d.Cache, tid)
					afterPolyBookSnapshot(d.App, tid)
					asyncSeedBookAndPushSnapshot(c.Request.Context(), d.Cfg, d.Cache, d.Hub, conn, tid, d.App)
				}
			case "unsubscribePolyBook":
				tid, _ := m["tokenId"].(string)
				tid = normalizeTokenID(tid)
				if tid != "" {
					handlePolyBookUnsubscribe(d.App, connPolyBooks, tid)
					logrus.WithFields(logx.Pairs("request_id", rid, "channel", "dashboard_ws", "token_id", tid)).Info("WebSocket：取消订阅订单簿")
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
