package wsrelay

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"

	"github.com/easyspace-ai/polybet/internal/logx"
)

// ErrClientNotRegistered is returned by WriteJSON when the connection was not Register'd.
var ErrClientNotRegistered = errors.New("websocket hub: client not registered")

type Hub struct {
	mu      sync.Mutex
	clients map[*websocket.Conn]*sync.Mutex // per-conn write serialization (gorilla/websocket)
}

func NewHub() *Hub {
	return &Hub{clients: make(map[*websocket.Conn]*sync.Mutex)}
}

func (h *Hub) ClientCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

func (h *Hub) Register(c *websocket.Conn) {
	h.mu.Lock()
	if _, ok := h.clients[c]; !ok {
		h.clients[c] = new(sync.Mutex)
	}
	n := len(h.clients)
	h.mu.Unlock()
	ra := ""
	if c != nil && c.RemoteAddr() != nil {
		ra = c.RemoteAddr().String()
	}
	logrus.WithFields(logx.Pairs("channel", "hub", "remote_addr", ra, "client_count", n)).Info("WebSocket Hub：客户端已注册")
}

// CloseAll force-closes every registered client (used during process shutdown so
// HTTP handlers blocked in ReadMessage return promptly).
func (h *Hub) CloseAll() {
	h.mu.Lock()
	clients := make([]*websocket.Conn, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()
	for _, c := range clients {
		_ = c.Close()
	}
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
	logrus.WithFields(logx.Pairs("channel", "hub", "remote_addr", ra, "client_count", n)).Info("WebSocket Hub：客户端已注销")
}

func broadcastMsgType(v any) string {
	if m, ok := v.(map[string]any); ok {
		if t, ok := m["type"].(string); ok {
			return t
		}
	}
	return ""
}

type hubClient struct {
	c  *websocket.Conn
	wm *sync.Mutex
}

func (h *Hub) WriteJSON(c *websocket.Conn, v any) error {
	h.mu.Lock()
	wm, ok := h.clients[c]
	h.mu.Unlock()
	if !ok || wm == nil {
		return ErrClientNotRegistered
	}
	wm.Lock()
	defer wm.Unlock()
	_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return c.WriteJSON(v)
}

// BroadcastJSONAsync fan-out without blocking the caller (e.g. HTTP / worker hot paths).
func (h *Hub) BroadcastJSONAsync(v any) {
	if h == nil {
		return
	}
	go h.BroadcastJSON(v)
}

func (h *Hub) BroadcastJSON(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		logrus.WithFields(logx.Pairs("err", err)).Warn("WebSocket Hub：广播 JSON 序列化失败")
		return
	}
	typ := broadcastMsgType(v)
	h.mu.Lock()
	n := len(h.clients)
	clients := make([]hubClient, 0, n)
	for c, wm := range h.clients {
		clients = append(clients, hubClient{c: c, wm: wm})
	}
	h.mu.Unlock()

	dead := make([]*websocket.Conn, 0)
	for _, cl := range clients {
		cl.wm.Lock()
		_ = cl.c.SetWriteDeadline(time.Now().Add(5 * time.Second))
		writeErr := cl.c.WriteMessage(websocket.TextMessage, b)
		cl.wm.Unlock()
		if writeErr != nil {
			dead = append(dead, cl.c)
		}
	}

	var nAfter int
	h.mu.Lock()
	for _, c := range dead {
		delete(h.clients, c)
		_ = c.Close()
	}
	nAfter = len(h.clients)
	h.mu.Unlock()
	if typ == "polyBookUpdate" {
		logrus.WithFields(logx.Pairs("type", typ, "clients", n, "bytes", len(b), "dropped", len(dead))).Debug("WebSocket Hub：广播")
	} else if typ == "marketsSnapshot" {
		markets := 0
		if m, ok := v.(map[string]any); ok {
			if data, ok := m["data"].([]any); ok {
				markets = len(data)
			}
		}
		logrus.WithFields(logx.Pairs("type", typ, "clients", n, "bytes", len(b), "dropped", len(dead), "markets", markets)).Info("WebSocket Hub：广播")
	} else {
		logrus.WithFields(logx.Pairs("type", typ, "clients", n, "bytes", len(b), "dropped", len(dead))).Info("WebSocket Hub：广播")
	}
	if len(dead) > 0 {
		logrus.WithFields(logx.Pairs("type", typ, "closed_clients", len(dead), "remaining", nAfter)).Warn("WebSocket Hub：部分客户端写入失败已关闭")
	}
}
