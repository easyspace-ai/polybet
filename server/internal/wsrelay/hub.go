package wsrelay

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Hub struct {
	mu      sync.Mutex
	clients map[*websocket.Conn]struct{}
}

func NewHub() *Hub {
	return &Hub{clients: make(map[*websocket.Conn]struct{})}
}

func (h *Hub) Register(c *websocket.Conn) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	n := len(h.clients)
	h.mu.Unlock()
	ra := ""
	if c != nil && c.RemoteAddr() != nil {
		ra = c.RemoteAddr().String()
	}
	slog.Info("ws_hub_register", "channel", "dashboard", "remote_addr", ra, "client_count", n)
}

func (h *Hub) Unregister(c *websocket.Conn) {
	h.mu.Lock()
	delete(h.clients, c)
	n := len(h.clients)
	h.mu.Unlock()
	ra := ""
	if c != nil && c.RemoteAddr() != nil {
		ra = c.RemoteAddr().String()
	}
	slog.Info("ws_hub_unregister", "channel", "dashboard", "remote_addr", ra, "client_count", n)
}

func broadcastMsgType(v any) string {
	if m, ok := v.(map[string]any); ok {
		if t, ok := m["type"].(string); ok {
			return t
		}
	}
	return ""
}

func (h *Hub) BroadcastJSON(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		slog.Warn("ws_hub_broadcast_marshal", "err", err)
		return
	}
	typ := broadcastMsgType(v)
	h.mu.Lock()
	n := len(h.clients)
	dead := make([]*websocket.Conn, 0)
	for c := range h.clients {
		_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := c.WriteMessage(websocket.TextMessage, b); err != nil {
			dead = append(dead, c)
		}
	}
	for _, c := range dead {
		delete(h.clients, c)
		_ = c.Close()
	}
	nAfter := len(h.clients)
	h.mu.Unlock()
	if typ == "polyBookUpdate" {
		slog.Debug("ws_hub_broadcast", "type", typ, "clients", n, "bytes", len(b), "dropped", len(dead))
	} else if typ == "marketsSnapshot" {
		markets := 0
		if m, ok := v.(map[string]any); ok {
			if data, ok := m["data"].([]any); ok {
				markets = len(data)
			}
		}
		slog.Info("ws_hub_broadcast", "type", typ, "clients", n, "bytes", len(b), "dropped", len(dead), "markets", markets)
	} else {
		slog.Info("ws_hub_broadcast", "type", typ, "clients", n, "bytes", len(b), "dropped", len(dead))
	}
	if len(dead) > 0 {
		slog.Warn("ws_hub_write_errors", "type", typ, "closed_clients", len(dead), "remaining", nAfter)
	}
}
