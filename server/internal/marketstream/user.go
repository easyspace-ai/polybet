package marketstream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"

	"github.com/easyspace-ai/polybet/internal/logx"
)

// UserStream is an authenticated WebSocket to the Polymarket user channel.
type UserStream struct {
	url    string
	config *Config
	creds  *APICreds

	conn      *websocket.Conn
	connMu    sync.Mutex
	connCtx   context.Context
	cancel    context.CancelFunc
	stopCh    chan struct{}
	doneCh    chan struct{}
	running   bool
	runningMu sync.RWMutex

	markets map[string]bool
	subMu   sync.RWMutex
	// subscribeAll means auth-only subscription (all user activity); excludes explicit condition IDs.
	subscribeAll bool

	reconnectC        chan struct{}
	reconnectAttempts int
	reconnectMu       sync.Mutex

	lastPong   time.Time
	lastPongMu sync.RWMutex

	onUserTrade UserTradeHandler
	onUserOrder UserOrderHandler
	onPosition  PositionHandler
	handlerMu   sync.RWMutex

	msgChan chan UserMessage
	errChan chan error
}

func effectiveUserWSURL(cfg *Config) string {
	if cfg != nil && cfg.UserWSURL != "" {
		return cfg.UserWSURL
	}
	return wsUserURL
}

// NewUserStream creates a UserStream with default configuration.
func NewUserStream(creds *APICreds) *UserStream {
	return NewUserStreamWithConfig(creds, DefaultConfig())
}

// NewUserStreamWithConfig creates a UserStream with custom configuration.
func NewUserStreamWithConfig(creds *APICreds, config *Config) *UserStream {
	if config == nil {
		config = DefaultConfig()
	}
	if creds == nil {
		panic("APICreds cannot be nil")
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &UserStream{
		url:        effectiveUserWSURL(config),
		config:     config,
		creds:      creds,
		markets:    make(map[string]bool),
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
		reconnectC: make(chan struct{}, 1),
		lastPong:   time.Now(),
		msgChan:    make(chan UserMessage, config.UserMsgBufferSize),
		errChan:    make(chan error, config.ErrorBufferSize),
		connCtx:    ctx,
		cancel:     cancel,
	}
}

// Start connects and begins processing.
func (s *UserStream) Start(ctx context.Context) error {
	s.runningMu.Lock()
	if s.running {
		s.runningMu.Unlock()
		return fmt.Errorf("user stream already running")
	}
	s.running = true
	s.runningMu.Unlock()

	if ctx != nil {
		s.connCtx = ctx
	}

	if err := s.connect(); err != nil {
		s.runningMu.Lock()
		s.running = false
		s.runningMu.Unlock()
		return fmt.Errorf("initial connection failed: %w", err)
	}

	go s.readLoop()
	go s.pingLoop()
	go s.reconnector()

	logrus.WithField("url", s.url).Info("用户行情 WebSocket：已启动")
	return nil
}

// Stop shuts down the user stream.
func (s *UserStream) Stop() {
	s.runningMu.Lock()
	if !s.running {
		s.runningMu.Unlock()
		return
	}
	s.running = false
	s.runningMu.Unlock()

	s.cancel()
	close(s.stopCh)

	s.connMu.Lock()
	if s.conn != nil {
		_ = s.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		s.conn.Close()
		s.conn = nil
	}
	s.connMu.Unlock()

	select {
	case <-s.doneCh:
	case <-time.After(5 * time.Second):
		logrus.Printf("%s shutdown timeout", clobLogUser)
	}

	logrus.Printf("%s stopped", clobLogUser)
}

// IsRunning reports whether the stream is running.
func (s *UserStream) IsRunning() bool {
	s.runningMu.RLock()
	defer s.runningMu.RUnlock()
	return s.running
}

// SubscribeAll sends a user subscribe with auth only (all markets / global feed).
func (s *UserStream) SubscribeAll() error {
	s.subMu.Lock()
	s.subscribeAll = true
	s.markets = make(map[string]bool)
	s.subMu.Unlock()
	return s.sendSubscription(nil)
}

// SubscribeMarkets adds condition IDs and sends subscribe (clears subscribe-all mode).
func (s *UserStream) SubscribeMarkets(conditionIDs ...string) error {
	if len(conditionIDs) == 0 {
		return nil
	}

	s.subMu.Lock()
	s.subscribeAll = false
	newSubs := make([]string, 0, len(conditionIDs))
	for _, id := range conditionIDs {
		if !s.markets[id] {
			s.markets[id] = true
			newSubs = append(newSubs, id)
		}
	}
	s.subMu.Unlock()

	if len(newSubs) == 0 {
		return nil
	}

	return s.sendSubscription(newSubs)
}

// UnsubscribeMarkets removes condition IDs from local tracking (no server unsubscribe frame).
func (s *UserStream) UnsubscribeMarkets(conditionIDs ...string) error {
	if len(conditionIDs) == 0 {
		return nil
	}

	s.subMu.Lock()
	for _, id := range conditionIDs {
		delete(s.markets, id)
	}
	s.subMu.Unlock()

	logrus.Printf("%s removed %d markets from subscription list", clobLogUser, len(conditionIDs))
	return nil
}

// Subscriptions returns subscribed condition IDs (empty when subscribe-all mode).
func (s *UserStream) Subscriptions() []string {
	s.subMu.RLock()
	defer s.subMu.RUnlock()
	out := make([]string, 0, len(s.markets))
	for id := range s.markets {
		out = append(out, id)
	}
	return out
}

// SubscriptionCount returns the number of tracked condition IDs.
func (s *UserStream) SubscriptionCount() int {
	s.subMu.RLock()
	defer s.subMu.RUnlock()
	return len(s.markets)
}

func (s *UserStream) OnUserTrade(h UserTradeHandler) {
	s.handlerMu.Lock()
	s.onUserTrade = h
	s.handlerMu.Unlock()
}
func (s *UserStream) OnUserOrder(h UserOrderHandler) {
	s.handlerMu.Lock()
	s.onUserOrder = h
	s.handlerMu.Unlock()
}
func (s *UserStream) OnPosition(h PositionHandler) {
	s.handlerMu.Lock()
	s.onPosition = h
	s.handlerMu.Unlock()
}

// Messages returns parsed user messages.
func (s *UserStream) Messages() <-chan UserMessage { return s.msgChan }

// Errors returns asynchronous errors.
func (s *UserStream) Errors() <-chan error { return s.errChan }

func (s *UserStream) connect() error {
	s.connMu.Lock()
	defer s.connMu.Unlock()

	if s.conn != nil {
		s.conn.Close()
	}

	dialer := websocket.Dialer{
		ReadBufferSize:   s.config.ReadBufferSize,
		WriteBufferSize:  s.config.WriteBufferSize,
		HandshakeTimeout: s.config.HandshakeTimeout,
	}

	if s.config.ProxyURL != "" {
		proxyURL, err := url.Parse(s.config.ProxyURL)
		if err == nil {
			dialer.Proxy = http.ProxyURL(proxyURL)
		}
	}

	headers := make(http.Header)
	headers.Set("User-Agent", "polymarket-marketstream/1.0")
	headers.Set("Origin", "https://polymarket.com")

	var conn *websocket.Conn
	var err error
	for i := 0; i < 3; i++ {
		conn, _, err = dialer.Dial(s.url, headers)
		if err == nil {
			break
		}
		if i < 2 {
			logrus.Printf("%s dial attempt %d/3 failed: %v, retrying...", clobLogUser, i+1, err)
			time.Sleep(time.Duration(i+1) * time.Second)
		}
	}
	if err != nil {
		return fmt.Errorf("dial failed: %w", err)
	}

	s.conn = conn
	s.reconnectMu.Lock()
	s.reconnectAttempts = 0
	s.reconnectMu.Unlock()

	_ = conn.SetReadDeadline(time.Now().Add(s.config.PongTimeout))

	return nil
}

// WriteJSON writes JSON (thread-safe).
func (s *UserStream) WriteJSON(v interface{}) error {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	if s.conn == nil {
		return fmt.Errorf("not connected")
	}
	return s.conn.WriteJSON(v)
}

// WriteMessage writes a frame (thread-safe).
func (s *UserStream) WriteMessage(messageType int, data []byte) error {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	if s.conn == nil {
		return fmt.Errorf("not connected")
	}
	return s.conn.WriteMessage(messageType, data)
}

func (s *UserStream) sendSubscription(conditionIDs []string) error {
	msg := map[string]interface{}{
		"type":         "subscribe",
		"operation":    "subscribe",
		"initial_dump": true,
		"auth": map[string]string{
			"apiKey":     s.creds.APIKey,
			"secret":     s.creds.APISecret,
			"passphrase": s.creds.APIPassphrase,
		},
	}

	if len(conditionIDs) > 0 {
		msg["markets"] = conditionIDs
	}

	if err := s.WriteJSON(msg); err != nil {
		return fmt.Errorf("send subscription failed: %w", err)
	}
	logrus.Printf("%s subscription message sent (markets=%d, subscribe_all=%v)", clobLogUser, len(conditionIDs), len(conditionIDs) == 0)
	return nil
}

func (s *UserStream) resubscribe() error {
	s.subMu.RLock()
	all := s.subscribeAll
	conditionIDs := make([]string, 0, len(s.markets))
	for id := range s.markets {
		conditionIDs = append(conditionIDs, id)
	}
	s.subMu.RUnlock()

	if all {
		return s.sendSubscription(nil)
	}
	if len(conditionIDs) == 0 {
		return nil
	}
	return s.sendSubscription(conditionIDs)
}

func (s *UserStream) readLoop() {
	defer close(s.doneCh)

	for {
		select {
		case <-s.connCtx.Done():
			return
		case <-s.stopCh:
			return
		default:
		}

		s.connMu.Lock()
		conn := s.conn
		s.connMu.Unlock()

		if conn == nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			s.connMu.Lock()
			if s.conn == conn {
				s.conn.Close()
				s.conn = nil
				if s.config.ReconnectEnabled {
					s.triggerReconnect()
				}
			}
			s.connMu.Unlock()

			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				logrus.Printf("%s connection closed normally", clobLogUser)
				return
			}

			if nerr, ok := err.(interface{ Timeout() bool }); ok && nerr.Timeout() {
				logrus.WithFields(logx.Pairs(
					"ws_channel", "clob_user",
					"err", err.Error(),
					"pong_timeout", s.config.PongTimeout.String(),
				)).Warn("CLOB WebSocket 读超时")
			} else {
				logrus.WithFields(logx.Pairs("ws_channel", "clob_user", "err", err.Error())).Warn("CLOB WebSocket 读错误")
			}
			continue
		}

		_ = conn.SetReadDeadline(time.Now().Add(s.config.PongTimeout))

		s.handleMessage(message)
	}
}

func (s *UserStream) pingLoop() {
	ticker := time.NewTicker(s.config.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.connCtx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.connMu.Lock()
			conn := s.conn
			s.connMu.Unlock()

			if conn == nil {
				continue
			}

			if err := s.WriteMessage(websocket.TextMessage, []byte("PING")); err != nil {
				logrus.Printf("%s ping failed: %v", clobLogUser, err)
				if s.config.ReconnectEnabled {
					s.triggerReconnect()
				}
			}
		}
	}
}

func (s *UserStream) triggerReconnect() {
	select {
	case s.reconnectC <- struct{}{}:
	default:
	}
}

func (s *UserStream) reconnector() {
	for {
		select {
		case <-s.connCtx.Done():
			return
		case <-s.stopCh:
			return
		case <-s.reconnectC:
			s.reconnectMu.Lock()
			s.reconnectAttempts++
			attempts := s.reconnectAttempts
			s.reconnectMu.Unlock()

			if attempts > s.config.MaxReconnectAttempts {
				select {
				case s.errChan <- fmt.Errorf("max reconnect attempts reached (%d)", s.config.MaxReconnectAttempts):
				default:
				}
				continue
			}

			delay := s.config.ReconnectDelay * time.Duration(attempts)
			if delay > s.config.MaxReconnectDelay {
				delay = s.config.MaxReconnectDelay
			}
			jitter := time.Duration(float64(time.Second) * (float64(time.Now().UnixNano()%1000) / 1000.0))
			delay += jitter

			logrus.Printf("%s reconnecting in %v (attempt %d/%d)...", clobLogUser, delay, attempts, s.config.MaxReconnectAttempts)

			select {
			case <-s.connCtx.Done():
				return
			case <-s.stopCh:
				return
			case <-time.After(delay):
			}

			if err := s.connect(); err != nil {
				logrus.Printf("%s reconnect attempt %d failed: %v", clobLogUser, attempts, err)
				s.triggerReconnect()
				continue
			}

			s.reconnectMu.Lock()
			s.reconnectAttempts = 0
			s.reconnectMu.Unlock()

			if err := s.resubscribe(); err != nil {
				logrus.Printf("%s resubscribe failed: %v", clobLogUser, err)
			}

			logrus.Printf("%s reconnected successfully (attempts=%d)", clobLogUser, attempts)
		}
	}
}

func (s *UserStream) handleMessage(data []byte) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return
	}

	if len(trimmed) > 0 && trimmed[0] != '{' && trimmed[0] != '[' {
		text := string(trimmed)
		if text == "PONG" || text == "pong" {
			s.lastPongMu.Lock()
			s.lastPong = time.Now()
			s.lastPongMu.Unlock()
			s.connMu.Lock()
			if s.conn != nil {
				_ = s.conn.SetReadDeadline(time.Now().Add(s.config.PongTimeout))
			}
			s.connMu.Unlock()
		} else if text == "PING" || text == "ping" {
			s.connMu.Lock()
			if s.conn != nil {
				_ = s.conn.WriteMessage(websocket.TextMessage, []byte("pong"))
				_ = s.conn.SetReadDeadline(time.Now().Add(s.config.PongTimeout))
			}
			s.connMu.Unlock()
		} else {
			logrus.Printf("%s received text message: %s", clobLogUser, text)
		}
		return
	}

	if trimmed[0] == '[' {
		var arr []json.RawMessage
		if err := json.Unmarshal(trimmed, &arr); err == nil {
			for _, raw := range arr {
				s.handleSingleMessage(raw)
			}
			return
		}
	}

	s.handleSingleMessage(trimmed)
}

func (s *UserStream) handleSingleMessage(data []byte) {
	var envelope struct {
		EventType string `json:"event_type"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		logrus.Printf("%s unmarshal failed for message: %s", clobLogUser, string(data))
		select {
		case s.errChan <- fmt.Errorf("unmarshal event_type: %w", err):
		default:
		}
		return
	}

	eventType := EventType(envelope.EventType)
	if eventType == "" {
		logrus.Printf("%s received message without event_type: %s", clobLogUser, string(data))
	}

	msg := UserMessage{
		EventType: eventType,
		Raw:       data,
	}
	select {
	case s.msgChan <- msg:
	default:
	}

	switch eventType {
	case EventUserTrade, EventType("trades"):
		var ev UserTradeEvent
		if err := json.Unmarshal(data, &ev); err == nil {
			ev.EventType = "trade"
			s.handlerMu.RLock()
			h := s.onUserTrade
			s.handlerMu.RUnlock()
			if h != nil {
				h(ev)
			}
		}
	case EventUserOrder, EventType("orders"):
		var ev UserOrderEvent
		if err := json.Unmarshal(data, &ev); err == nil {
			ev.EventType = "order"
			s.handlerMu.RLock()
			h := s.onUserOrder
			s.handlerMu.RUnlock()
			if h != nil {
				h(ev)
			}
		}
	case EventPosition:
		var ev PositionEvent
		if err := json.Unmarshal(data, &ev); err == nil {
			ev.EventType = "position"
			s.handlerMu.RLock()
			h := s.onPosition
			s.handlerMu.RUnlock()
			if h != nil {
				h(ev)
			}
		}
	case EventSubscribed:
		logrus.Printf("%s subscription confirmed", clobLogUser)
	}
}
