package httpserver

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/easyspace-ai/polybet/internal/service/marketsvc"
	"github.com/easyspace-ai/polybet/internal/service/risksvc"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool { return true },
}

func registerWS(r *gin.Engine, d Deps) {
	r.GET("/ws", func(c *gin.Context) {
		rid := c.GetString("request_id")
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			slog.Warn("ws_dash_upgrade_failed", "request_id", rid, "err", err.Error())
			return
		}
		ra := ""
		if conn.RemoteAddr() != nil {
			ra = conn.RemoteAddr().String()
		}
		slog.Info("ws_dash_open", "request_id", rid, "remote_addr", ra)
		d.Hub.Register(conn)
		defer func() {
			d.Hub.Unregister(conn)
			slog.Info("ws_dash_close", "request_id", rid, "remote_addr", ra)
		}()

		meta := risksvc.Meta{OutboundProxyConfigured: d.Cfg.HTTPPlatformProxy != ""}
		if markets, err := marketsvc.BuildMarketsPayload(c, d.Store, d.Cache); err == nil {
			_ = conn.WriteJSON(map[string]any{"type": "marketsSnapshot", "data": markets})
			slog.Info("ws_dash_handshake_sent", "request_id", rid, "markets_snapshot", len(markets))
		} else {
			slog.Warn("ws_dash_markets_snapshot_build_failed", "request_id", rid, "err", err.Error())
		}
		_ = conn.WriteJSON(map[string]any{"type": "snapshot", "data": []any{}})
		_, _, _ = d.Risk.ListRiskPositionsEnriched(c, meta)

		conn.SetReadDeadline(time.Now().Add(120 * time.Second))
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				slog.Info("ws_dash_read_end", "request_id", rid, "remote_addr", ra, "err", err.Error())
				return
			}
			var m map[string]any
			if err := json.Unmarshal(msg, &m); err != nil {
				slog.Debug("ws_dash_bad_frame", "request_id", rid, "err", err.Error())
				continue
			}
			ty, _ := m["type"].(string)
			switch ty {
			case "subscribePolyBook":
				tid, _ := m["tokenId"].(string)
				if tid != "" {
					slog.Info("ws_dash_subscribe_poly_book", "request_id", rid, "token_id", tid)
					levels := d.Cache.SnapshotLevels(tid)
					_ = conn.WriteJSON(map[string]any{"type": "polyBookSnapshot", "tokenId": tid, "levels": levels})
				}
			case "subscribePolyOdds":
				if raw, ok := m["tokenIds"].([]any); ok {
					slog.Info("ws_dash_subscribe_poly_odds", "request_id", rid, "token_count", len(raw))
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
						_ = conn.WriteJSON(map[string]any{"type": "polyOddsSnapshot", "data": entries})
					}
				}
			default:
				if ty != "" {
					slog.Debug("ws_dash_client_msg", "request_id", rid, "type", ty)
				}
			}
			conn.SetReadDeadline(time.Now().Add(120 * time.Second))
		}
	})
}
