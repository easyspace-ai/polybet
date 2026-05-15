// Package marketstream provides a WebSocket client layer for Polymarket CLOB
// market and user channels (subscribe, reconnect, callbacks).
package marketstream

import (
	"encoding/json"
	"strings"
	"time"
)

const (
	wsMarketURL = "wss://ws-subscriptions-clob.polymarket.com/ws/market"
	wsUserURL   = "wss://ws-subscriptions-clob.polymarket.com/ws/user"

	// clobLogMarket and clobLogUser prefix printf-style logs so market vs user
	// WebSocket paths are identifiable in shared message shapes.
	clobLogMarket = "[CLOB:market]"
	clobLogUser   = "[CLOB:user]"
)

// ResolveCLOBWSEndpoints maps POLYMARKET_CLOB_WS_URL-style base (e.g. wss://host)
// to full market and user WebSocket URLs. Empty base returns production defaults.
func ResolveCLOBWSEndpoints(base string) (marketURL, userURL string) {
	base = strings.TrimSpace(base)
	if base == "" {
		return wsMarketURL, wsUserURL
	}
	base = strings.TrimRight(base, "/")
	switch {
	case strings.HasSuffix(base, "/ws/market"):
		return base, strings.TrimSuffix(base, "/ws/market") + "/ws/user"
	case strings.HasSuffix(base, "/ws/user"):
		return strings.TrimSuffix(base, "/ws/user") + "/ws/market", base
	default:
		return base + "/ws/market", base + "/ws/user"
	}
}

// Config holds options for WebSocket clients.
type Config struct {
	ProxyURL string

	// MarketWSURL and UserWSURL override dial targets (full wss URLs).
	// Usually leave empty and set via ResolveCLOBWSEndpoints + assignment.
	MarketWSURL string
	UserWSURL   string

	ReconnectEnabled     bool
	ReconnectDelay       time.Duration
	MaxReconnectDelay    time.Duration
	MaxReconnectAttempts int

	PingInterval time.Duration
	PongTimeout  time.Duration
	ReadTimeout  time.Duration

	MarketMsgBufferSize int
	UserMsgBufferSize   int
	ErrorBufferSize     int

	ReadBufferSize   int
	WriteBufferSize  int
	HandshakeTimeout time.Duration
}

// DefaultConfig returns default options.
func DefaultConfig() *Config {
	return &Config{
		ReconnectEnabled:     true,
		ReconnectDelay:       2 * time.Second,
		MaxReconnectDelay:    30 * time.Second,
		MaxReconnectAttempts: 10,
		PingInterval:         10 * time.Second,
		PongTimeout:          30 * time.Second,
		ReadTimeout:          300 * time.Second,
		MarketMsgBufferSize:  1000,
		UserMsgBufferSize:    1000,
		ErrorBufferSize:      100,
		ReadBufferSize:       4096,
		WriteBufferSize:      4096,
		HandshakeTimeout:     15 * time.Second,
	}
}

// EventType classifies WS payloads.
type EventType string

const (
	EventBook           EventType = "book"
	EventPriceChange    EventType = "price_change"
	EventLastTradePrice EventType = "last_trade_price"
	EventTickSizeChange EventType = "tick_size_change"
	EventBestBidAsk     EventType = "best_bid_ask"
	EventNewMarket      EventType = "new_market"
	EventMarketResolved EventType = "market_resolved"

	EventUserTrade  EventType = "trade"
	EventUserOrder  EventType = "order"
	EventPosition   EventType = "position"
	EventSubscribed EventType = "subscribed"
	EventPong       EventType = "pong"
)

// OrderLevel is one price level in the book.
type OrderLevel struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

// BookEvent is a full book snapshot or update.
type BookEvent struct {
	EventType string       `json:"event_type"`
	AssetID   string       `json:"asset_id"`
	Market    string       `json:"market"`
	Bids      []OrderLevel `json:"bids"`
	Asks      []OrderLevel `json:"asks"`
	Timestamp string       `json:"timestamp"`
	Hash      string       `json:"hash"`
}

// PriceChange is one row inside a price_change event.
type PriceChange struct {
	AssetID string `json:"asset_id"`
	Price   string `json:"price"`
	Size    string `json:"size"`
	Side    string `json:"side"`
	Hash    string `json:"hash"`
	BestBid string `json:"best_bid"`
	BestAsk string `json:"best_ask"`
}

// PriceChangeEvent carries best bid/ask updates per asset.
type PriceChangeEvent struct {
	EventType    string        `json:"event_type"`
	Market       string        `json:"market"`
	PriceChanges []PriceChange `json:"price_changes"`
	Timestamp    string        `json:"timestamp"`
}

// LastTradePriceEvent is a public last-trade tick.
type LastTradePriceEvent struct {
	AssetID    string `json:"asset_id"`
	EventType  string `json:"event_type"`
	FeeRateBps string `json:"fee_rate_bps"`
	Market     string `json:"market"`
	Price      string `json:"price"`
	Side       string `json:"side"`
	Size       string `json:"size"`
	Timestamp  string `json:"timestamp"`
}

// BestBidAskEvent is top-of-book without full depth.
type BestBidAskEvent struct {
	EventType string `json:"event_type"`
	Market    string `json:"market"`
	AssetID   string `json:"asset_id"`
	BestBid   string `json:"best_bid"`
	BestAsk   string `json:"best_ask"`
	Spread    string `json:"spread"`
	Timestamp string `json:"timestamp"`
}

// TickSizeChangeEvent signals tick size changes.
type TickSizeChangeEvent struct {
	EventType   string `json:"event_type"`
	AssetID     string `json:"asset_id"`
	Market      string `json:"market"`
	OldTickSize string `json:"old_tick_size"`
	NewTickSize string `json:"new_tick_size"`
	Timestamp   string `json:"timestamp"`
}

// NewMarketEvent is emitted when a new market appears.
type NewMarketEvent struct {
	EventType             string      `json:"event_type"`
	ID                    string      `json:"id"`
	Question              string      `json:"question"`
	Market                string      `json:"market"`
	Slug                  string      `json:"slug"`
	Description           string      `json:"description"`
	AssetsIDs             []string    `json:"assets_ids"`
	Outcomes              []string    `json:"outcomes"`
	Timestamp             string      `json:"timestamp"`
	Tags                  []string    `json:"tags"`
	ConditionID           string      `json:"condition_id"`
	Active                bool        `json:"active"`
	ClobTokenIds          []string    `json:"clob_token_ids"`
	OrderPriceMinTickSize string      `json:"order_price_min_tick_size"`
	GroupItemTitle        string      `json:"group_item_title"`
	TakerBaseFee          string      `json:"taker_base_fee"`
	FeesEnabled           bool        `json:"fees_enabled"`
	FeeSchedule           FeeSchedule `json:"fee_schedule"`
}

// FeeSchedule describes fee curve metadata.
type FeeSchedule struct {
	Exponent   string `json:"exponent"`
	Rate       string `json:"rate"`
	TakerOnly  bool   `json:"taker_only"`
	RebateRate string `json:"rebate_rate"`
}

// MarketResolvedEvent is emitted when a market resolves.
type MarketResolvedEvent struct {
	EventType      string   `json:"event_type"`
	ID             string   `json:"id"`
	Question       string   `json:"question"`
	Market         string   `json:"market"`
	Slug           string   `json:"slug"`
	Description    string   `json:"description"`
	AssetsIDs      []string `json:"assets_ids"`
	Outcomes       []string `json:"outcomes"`
	WinningAssetID string   `json:"winning_asset_id"`
	WinningOutcome string   `json:"winning_outcome"`
	Timestamp      string   `json:"timestamp"`
}

// MarketMessage is a raw market message with parsed event_type.
type MarketMessage struct {
	EventType EventType       `json:"event_type"`
	Raw       json.RawMessage `json:"-"`
}

// APICreds are required for the user channel.
type APICreds struct {
	APIKey        string `json:"apiKey"`
	APISecret     string `json:"secret"`
	APIPassphrase string `json:"passphrase"`
}

// Numeric unmarshals Polymarket numbers as string or JSON number.
type Numeric float64

func (n *Numeric) UnmarshalJSON(data []byte) error {
	data = jsonBytesTrim(data)
	if len(data) == 0 {
		*n = 0
		return nil
	}
	if data[0] == '"' && data[len(data)-1] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		if s == "" {
			*n = 0
			return nil
		}
		f, err := jsonNumberParse(s)
		if err != nil {
			return err
		}
		*n = Numeric(f)
		return nil
	}
	var f float64
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	*n = Numeric(f)
	return nil
}

func (n Numeric) Float64() float64 { return float64(n) }

// UserTradeEvent is a user-channel trade (same wire shape as CLOB user "trade").
type UserTradeEvent struct {
	EventType       string  `json:"event_type"`
	ID              string  `json:"id,omitempty"`
	Status          string  `json:"status,omitempty"`
	ProxyWallet     string  `json:"proxyWallet"`
	Type            string  `json:"type"`
	Side            string  `json:"side"`
	IsMaker         bool    `json:"isMaker"`
	Asset           string  `json:"asset,omitempty"`
	AssetID         string  `json:"asset_id,omitempty"`
	ConditionID     string  `json:"conditionId"`
	Size            Numeric `json:"size"`
	UsdcSize        Numeric `json:"usdcSize"`
	Price           Numeric `json:"price"`
	Timestamp       int64   `json:"timestamp"`
	Title           string  `json:"title"`
	Slug            string  `json:"slug"`
	Market          string  `json:"market,omitempty"`
	Outcome         string  `json:"outcome"`
	OutcomeIndex    int     `json:"outcomeIndex"`
	TransactionHash string  `json:"transactionHash"`
}

// UserOrderEvent is a user-channel order update.
type UserOrderEvent struct {
	EventType string  `json:"event_type"`
	OrderID   string  `json:"order_id"`
	Market    string  `json:"market"`
	AssetID   string  `json:"asset_id"`
	Side      string  `json:"side"`
	Size      Numeric `json:"size"`
	Price     Numeric `json:"price"`
	Status    string  `json:"status"`
	Type      string  `json:"type"`
	Timestamp int64   `json:"timestamp"`
}

// PositionEvent is a user-channel position update.
type PositionEvent struct {
	EventType    string  `json:"event_type"`
	Asset        string  `json:"asset"`
	ConditionID  string  `json:"conditionId"`
	Size         Numeric `json:"size"`
	AvgPrice     Numeric `json:"avgPrice"`
	CurPrice     Numeric `json:"curPrice"`
	RealizedPNL  Numeric `json:"realizedPnl"`
	Title        string  `json:"title"`
	Slug         string  `json:"slug"`
	Outcome      string  `json:"outcome"`
	OutcomeIndex int     `json:"outcomeIndex"`
}

// UserMessage is a raw user message with parsed event_type.
type UserMessage struct {
	EventType EventType       `json:"event_type"`
	Raw       json.RawMessage `json:"-"`
}

// BookHandler receives book events.
type BookHandler func(event BookEvent)

// PriceChangeHandler receives price_change events.
type PriceChangeHandler func(event PriceChangeEvent)

// LastTradePriceHandler receives last_trade_price events.
type LastTradePriceHandler func(event LastTradePriceEvent)

// BestBidAskHandler receives best_bid_ask events.
type BestBidAskHandler func(event BestBidAskEvent)

// TickSizeChangeHandler receives tick_size_change events.
type TickSizeChangeHandler func(event TickSizeChangeEvent)

// NewMarketHandler receives new_market events.
type NewMarketHandler func(event NewMarketEvent)

// MarketResolvedHandler receives market_resolved events.
type MarketResolvedHandler func(event MarketResolvedEvent)

// UserTradeHandler receives user trade events.
type UserTradeHandler func(event UserTradeEvent)

// UserOrderHandler receives user order events.
type UserOrderHandler func(event UserOrderEvent)

// PositionHandler receives position events.
type PositionHandler func(event PositionEvent)

func jsonBytesTrim(data []byte) []byte {
	i, j := 0, len(data)
	for i < j && (data[i] == ' ' || data[i] == '\t' || data[i] == '\n' || data[i] == '\r') {
		i++
	}
	for j > i && (data[j-1] == ' ' || data[j-1] == '\t' || data[j-1] == '\n' || data[j-1] == '\r') {
		j--
	}
	return data[i:j]
}

func jsonNumberParse(s string) (float64, error) {
	var f float64
	if err := json.Unmarshal([]byte(s), &f); err != nil {
		return 0, err
	}
	return f, nil
}
