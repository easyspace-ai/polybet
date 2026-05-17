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

// MarketStream is one WebSocket connection to the Polymarket market channel.
type MarketStream struct {
	url    string
	config *Config

	conn      *websocket.Conn
	connMu    sync.Mutex
	connCtx   context.Context
	cancel    context.CancelFunc
	stopCh    chan struct{}
	doneCh    chan struct{}
	running   bool
	runningMu sync.RWMutex

	subscriptions map[string]bool
	subMu         sync.RWMutex

	reconnectC        chan struct{}
	reconnectAttempts int
	reconnectMu       sync.Mutex

	lastPong   time.Time
	lastPongMu sync.RWMutex

	connectedAt   time.Time
	connectedAtMu sync.Mutex

	sleepW *sleepWatchdog
	pongW  *sleepWatchdog

	onBook           BookHandler
	onPriceChange    PriceChangeHandler
	onLastTradePrice LastTradePriceHandler
	onBestBidAsk     BestBidAskHandler
	onTickSizeChange TickSizeChangeHandler
	onNewMarket      NewMarketHandler
	onMarketResolved MarketResolvedHandler
	handlerMu        sync.RWMutex

	msgChan chan MarketMessage
	errChan chan error
}

func effectiveMarketWSURL(cfg *Config) string {
	if cfg != nil && cfg.MarketWSURL != "" {
		return cfg.MarketWSURL
	}
	return wsMarketURL
}

// NewMarketStream creates a MarketStream with default configuration.
func NewMarketStream() *MarketStream {
	return NewMarketStreamWithConfig(DefaultConfig())
}

// NewMarketStreamWithConfig creates a MarketStream with custom configuration.
func NewMarketStreamWithConfig(config *Config) *MarketStream {
	if config == nil {
		config = DefaultConfig()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &MarketStream{
		url:           effectiveMarketWSURL(config),
		config:        config,
		subscriptions: make(map[string]bool),
		stopCh:        make(chan struct{}),
		doneCh:        make(chan struct{}),
		reconnectC:    make(chan struct{}, 1),
		lastPong:      time.Now(),
		msgChan:       make(chan MarketMessage, config.MarketMsgBufferSize),
		errChan:       make(chan error, config.ErrorBufferSize),
		connCtx:       ctx,
		cancel:        cancel,
	}
}

// Start connects and begins read/ping/reconnect loops.
func (s *MarketStream) Start(ctx context.Context) error {
	s.runningMu.Lock()
	if s.running {
		s.runningMu.Unlock()
		return fmt.Errorf("market stream already running")
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
	s.startWatchdogs()

	logrus.WithField("url", s.url).Info("市场订单簿 WebSocket：已启动")
	return nil
}

// Stop shuts down the stream.
func (s *MarketStream) Stop() {
	s.runningMu.Lock()
	if !s.running {
		s.runningMu.Unlock()
		return
	}
	s.running = false
	s.runningMu.Unlock()

	s.cancel()
	close(s.stopCh)
	s.stopWatchdogs()

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
		logrus.Printf("%s shutdown timeout", clobLogMarket)
	}

	logrus.Printf("%s stopped", clobLogMarket)
}

// IsRunning reports whether the stream is running.
func (s *MarketStream) IsRunning() bool {
	s.runningMu.RLock()
	defer s.runningMu.RUnlock()
	return s.running
}

// Subscribe adds asset IDs and sends a subscribe frame.
func (s *MarketStream) Subscribe(assetIDs ...string) error {
	if len(assetIDs) == 0 {
		return nil
	}

	s.subMu.Lock()
	newSubs := make([]string, 0, len(assetIDs))
	for _, id := range assetIDs {
		if !s.subscriptions[id] {
			s.subscriptions[id] = true
			newSubs = append(newSubs, id)
		}
	}
	s.subMu.Unlock()

	if len(newSubs) == 0 {
		return nil
	}

	return s.sendSubscription(newSubs, "subscribe")
}

// Unsubscribe removes asset IDs and sends an unsubscribe frame.
func (s *MarketStream) Unsubscribe(assetIDs ...string) error {
	if len(assetIDs) == 0 {
		return nil
	}

	s.subMu.Lock()
	toRemove := make([]string, 0, len(assetIDs))
	for _, id := range assetIDs {
		if s.subscriptions[id] {
			delete(s.subscriptions, id)
			toRemove = append(toRemove, id)
		}
	}
	s.subMu.Unlock()

	if len(toRemove) == 0 {
		return nil
	}

	return s.sendSubscription(toRemove, "unsubscribe")
}

// Subscriptions returns a snapshot of subscribed asset IDs.
func (s *MarketStream) Subscriptions() []string {
	s.subMu.RLock()
	defer s.subMu.RUnlock()
	out := make([]string, 0, len(s.subscriptions))
	for id := range s.subscriptions {
		out = append(out, id)
	}
	return out
}

// SubscriptionCount returns the subscription count.
func (s *MarketStream) SubscriptionCount() int {
	s.subMu.RLock()
	defer s.subMu.RUnlock()
	return len(s.subscriptions)
}

func (s *MarketStream) OnBook(h BookHandler) { s.handlerMu.Lock(); s.onBook = h; s.handlerMu.Unlock() }
func (s *MarketStream) OnPriceChange(h PriceChangeHandler) {
	s.handlerMu.Lock()
	s.onPriceChange = h
	s.handlerMu.Unlock()
}
func (s *MarketStream) OnLastTradePrice(h LastTradePriceHandler) {
	s.handlerMu.Lock()
	s.onLastTradePrice = h
	s.handlerMu.Unlock()
}
func (s *MarketStream) OnBestBidAsk(h BestBidAskHandler) {
	s.handlerMu.Lock()
	s.onBestBidAsk = h
	s.handlerMu.Unlock()
}
func (s *MarketStream) OnTickSizeChange(h TickSizeChangeHandler) {
	s.handlerMu.Lock()
	s.onTickSizeChange = h
	s.handlerMu.Unlock()
}
func (s *MarketStream) OnNewMarket(h NewMarketHandler) {
	s.handlerMu.Lock()
	s.onNewMarket = h
	s.handlerMu.Unlock()
}
func (s *MarketStream) OnMarketResolved(h MarketResolvedHandler) {
	s.handlerMu.Lock()
	s.onMarketResolved = h
	s.handlerMu.Unlock()
}

// Messages returns the fan-out channel of parsed market messages.
func (s *MarketStream) Messages() <-chan MarketMessage { return s.msgChan }

// Errors returns asynchronous errors (including reconnect exhaustion).
func (s *MarketStream) Errors() <-chan error { return s.errChan }

func (s *MarketStream) connect() error {
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
			logrus.Printf("%s dial attempt %d/3 failed: %v, retrying...", clobLogMarket, i+1, err)
			time.Sleep(time.Duration(i+1) * time.Second)
		}
	}
	if err != nil {
		return fmt.Errorf("dial failed: %w", err)
	}

	conn.SetPongHandler(func(appData string) error {
		s.lastPongMu.Lock()
		s.lastPong = time.Now()
		s.lastPongMu.Unlock()
		_ = conn.SetReadDeadline(time.Now().Add(s.config.PongTimeout))
		return nil
	})

	s.conn = conn
	s.reconnectMu.Lock()
	s.reconnectAttempts = 0
	s.reconnectMu.Unlock()
	s.connectedAtMu.Lock()
	s.connectedAt = time.Now()
	s.connectedAtMu.Unlock()
	s.lastPongMu.Lock()
	s.lastPong = time.Now()
	s.lastPongMu.Unlock()

	_ = conn.SetReadDeadline(time.Now().Add(s.config.PongTimeout))
	return nil
}

// ForceReconnect closes the current connection and schedules a reconnect.
func (s *MarketStream) ForceReconnect() {
	s.connMu.Lock()
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
	s.connMu.Unlock()
	if s.config.ReconnectEnabled {
		s.triggerReconnect()
	}
}

func (s *MarketStream) startWatchdogs() {
	s.stopWatchdogs()
	th := s.config.SleepThreshold
	if th <= 0 {
		th = 5 * time.Second
	}
	s.sleepW = startSleepWatchdog(th, func() {
		logrus.Printf("%s sleep/wake detected, forcing reconnect", clobLogMarket)
		s.ForceReconnect()
	})
	s.pongW = startPongWatchdog(s.config,
		func() *websocket.Conn {
			s.connMu.Lock()
			defer s.connMu.Unlock()
			return s.conn
		},
		func() time.Time {
			s.lastPongMu.RLock()
			defer s.lastPongMu.RUnlock()
			return s.lastPong
		},
		func() {
			logrus.Printf("%s pong watchdog stale, forcing reconnect", clobLogMarket)
			s.ForceReconnect()
		},
	)
}

func (s *MarketStream) stopWatchdogs() {
	if s.sleepW != nil {
		s.sleepW.stop()
		s.sleepW = nil
	}
	if s.pongW != nil {
		s.pongW.stop()
		s.pongW = nil
	}
}

func (s *MarketStream) maybeResetReconnectAttempts() {
	stable := s.config.ReconnectStable
	if stable <= 0 {
		return
	}
	s.connectedAtMu.Lock()
	at := s.connectedAt
	s.connectedAtMu.Unlock()
	if at.IsZero() || time.Since(at) < stable {
		return
	}
	s.reconnectMu.Lock()
	if s.reconnectAttempts > 0 {
		s.reconnectAttempts = 0
	}
	s.reconnectMu.Unlock()
}

func (s *MarketStream) sendSubscription(assetIDs []string, operation string) error {
	if len(assetIDs) == 0 {
		return nil
	}

	msg := map[string]interface{}{
		"assets_ids":             assetIDs,
		"type":                   "market",
		"custom_feature_enabled": true,
		"initial_dump":           true,
	}

	if operation != "" {
		msg["operation"] = operation
	}

	if err := s.WriteJSON(msg); err != nil {
		return fmt.Errorf("send subscription failed: %w", err)
	}
	logrus.Printf("%s subscription message sent (assets=%d, op=%s)", clobLogMarket, len(assetIDs), operation)

	return nil
}

// WriteJSON writes JSON to the connection (thread-safe).
func (s *MarketStream) WriteJSON(v interface{}) error {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	if s.conn == nil {
		return fmt.Errorf("not connected")
	}
	return s.conn.WriteJSON(v)
}

// WriteMessage writes a WebSocket frame (thread-safe).
func (s *MarketStream) WriteMessage(messageType int, data []byte) error {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	if s.conn == nil {
		return fmt.Errorf("not connected")
	}
	return s.conn.WriteMessage(messageType, data)
}

func (s *MarketStream) resubscribe() error {
	s.subMu.RLock()
	assetIDs := make([]string, 0, len(s.subscriptions))
	for id := range s.subscriptions {
		assetIDs = append(assetIDs, id)
	}
	s.subMu.RUnlock()

	if len(assetIDs) == 0 {
		return nil
	}

	return s.sendSubscription(assetIDs, "")
}

func (s *MarketStream) readLoop() {
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
				logrus.Printf("%s connection closed normally", clobLogMarket)
				return
			}

			if nerr, ok := err.(interface{ Timeout() bool }); ok && nerr.Timeout() {
				logrus.WithFields(logx.Pairs(
					"ws_channel", "clob_market",
					"err", err.Error(),
					"pong_timeout", s.config.PongTimeout.String(),
				)).Warn("CLOB WebSocket 读超时")
			} else {
				logrus.WithFields(logx.Pairs("ws_channel", "clob_market", "err", err.Error())).Warn("CLOB WebSocket 读错误")
			}
			continue
		}

		_ = conn.SetReadDeadline(time.Now().Add(s.config.PongTimeout))
		s.maybeResetReconnectAttempts()

		s.handleMessage(message)
	}
}

func (s *MarketStream) pingLoop() {
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
				logrus.Printf("%s ping failed: %v", clobLogMarket, err)
				if s.config.ReconnectEnabled {
					s.triggerReconnect()
				}
			}
		}
	}
}

func (s *MarketStream) triggerReconnect() {
	select {
	case s.reconnectC <- struct{}{}:
	default:
	}
}

func (s *MarketStream) reconnector() {
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

			if maxReconnectExceeded(s.config, attempts) {
				select {
				case s.errChan <- fmt.Errorf("max reconnect attempts reached (%d)", s.config.MaxReconnectAttempts):
				default:
				}
				continue
			}

			delay := ReconnectDelayForAttempt(s.config, attempts)
			nextAt := time.Now().Add(delay)
			if s.config.OnReconnectScheduled != nil {
				s.config.OnReconnectScheduled(attempts, nextAt)
			}

			maxLabel := "∞"
			if s.config.MaxReconnectAttempts > 0 {
				maxLabel = fmt.Sprintf("%d", s.config.MaxReconnectAttempts)
			}
			logrus.Printf("%s reconnecting in %v (attempt %d/%s)...", clobLogMarket, delay, attempts, maxLabel)

			select {
			case <-s.connCtx.Done():
				return
			case <-s.stopCh:
				return
			case <-time.After(delay):
			}

			if err := s.connect(); err != nil {
				logrus.Printf("%s reconnect attempt %d failed: %v", clobLogMarket, attempts, err)
				s.triggerReconnect()
				continue
			}

			s.reconnectMu.Lock()
			s.reconnectAttempts = 0
			s.reconnectMu.Unlock()

			if err := s.resubscribe(); err != nil {
				logrus.Printf("%s resubscribe failed: %v", clobLogMarket, err)
			}

			logrus.Printf("%s reconnected successfully (attempts=%d)", clobLogMarket, attempts)
		}
	}
}

func (s *MarketStream) handleMessage(data []byte) {
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
				_ = s.conn.WriteMessage(websocket.TextMessage, []byte("PONG"))
				_ = s.conn.SetReadDeadline(time.Now().Add(s.config.PongTimeout))
			}
			s.connMu.Unlock()
		} else {
			logrus.Printf("%s received text message: %s", clobLogMarket, text)
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

func (s *MarketStream) handleSingleMessage(data []byte) {
	var envelope struct {
		EventType string `json:"event_type"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		select {
		case s.errChan <- fmt.Errorf("unmarshal event_type: %w", err):
		default:
		}
		return
	}

	msg := MarketMessage{
		EventType: EventType(envelope.EventType),
		Raw:       data,
	}
	select {
	case s.msgChan <- msg:
	default:
	}

	switch EventType(envelope.EventType) {
	case EventBook:
		var ev BookEvent
		if err := json.Unmarshal(data, &ev); err == nil {
			s.handlerMu.RLock()
			h := s.onBook
			s.handlerMu.RUnlock()
			if h != nil {
				h(ev)
			}
		}
	case EventPriceChange:
		var ev PriceChangeEvent
		if err := json.Unmarshal(data, &ev); err == nil {
			s.handlerMu.RLock()
			h := s.onPriceChange
			s.handlerMu.RUnlock()
			if h != nil {
				h(ev)
			}
		}
	case EventLastTradePrice:
		var ev LastTradePriceEvent
		if err := json.Unmarshal(data, &ev); err == nil {
			s.handlerMu.RLock()
			h := s.onLastTradePrice
			s.handlerMu.RUnlock()
			if h != nil {
				h(ev)
			}
		}
	case EventBestBidAsk:
		var ev BestBidAskEvent
		if err := json.Unmarshal(data, &ev); err == nil {
			s.handlerMu.RLock()
			h := s.onBestBidAsk
			s.handlerMu.RUnlock()
			if h != nil {
				h(ev)
			}
		}
	case EventTickSizeChange:
		var ev TickSizeChangeEvent
		if err := json.Unmarshal(data, &ev); err == nil {
			s.handlerMu.RLock()
			h := s.onTickSizeChange
			s.handlerMu.RUnlock()
			if h != nil {
				h(ev)
			}
		}
	case EventNewMarket:
		var ev NewMarketEvent
		if err := json.Unmarshal(data, &ev); err == nil {
			s.handlerMu.RLock()
			h := s.onNewMarket
			s.handlerMu.RUnlock()
			if h != nil {
				h(ev)
			}
		}
	case EventMarketResolved:
		var ev MarketResolvedEvent
		if err := json.Unmarshal(data, &ev); err == nil {
			s.handlerMu.RLock()
			h := s.onMarketResolved
			s.handlerMu.RUnlock()
			if h != nil {
				h(ev)
			}
		}
	case EventSubscribed:
		logrus.Printf("%s subscription confirmed", clobLogMarket)
	}
}
