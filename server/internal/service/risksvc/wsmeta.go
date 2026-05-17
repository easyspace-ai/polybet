package risksvc

import (
	"sync"
	"time"
)

// WSEvent is a single connection lifecycle entry for dashboard UI.
type WSEvent struct {
	Channel string `json:"channel"`
	At      string `json:"at"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

const wsEventCap = 20

type wsChannelMeta struct {
	nextRetryAtMs int64
	attempt       int
	events        []WSEvent
}

// WSMetaCollector tracks upstream reconnect metadata for poly_status / API.
type WSMetaCollector struct {
	mu sync.Mutex
	ob wsChannelMeta
	us wsChannelMeta
}

func NewWSMetaCollector() *WSMetaCollector {
	return &WSMetaCollector{}
}

func (c *WSMetaCollector) Record(channel, level, message string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ev := WSEvent{
		Channel: channel,
		At:      time.Now().UTC().Format(time.RFC3339),
		Level:   level,
		Message: message,
	}
	var dst *wsChannelMeta
	switch channel {
	case "orderbook":
		dst = &c.ob
	case "user":
		dst = &c.us
	default:
		return
	}
	dst.events = append([]WSEvent{ev}, dst.events...)
	if len(dst.events) > wsEventCap {
		dst.events = dst.events[:wsEventCap]
	}
}

func (c *WSMetaCollector) SetReconnectSchedule(channel string, attempt int, nextRetryAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	dst := c.metaFor(channel)
	if dst == nil {
		return
	}
	dst.attempt = attempt
	if nextRetryAt.IsZero() {
		dst.nextRetryAtMs = 0
	} else {
		dst.nextRetryAtMs = nextRetryAt.UnixMilli()
	}
	c.Record(channel, "info", "scheduled reconnect")
}

func (c *WSMetaCollector) ClearReconnectSchedule(channel string) {
	c.SetReconnectSchedule(channel, 0, time.Time{})
}

func (c *WSMetaCollector) metaFor(channel string) *wsChannelMeta {
	switch channel {
	case "orderbook":
		return &c.ob
	case "user":
		return &c.us
	default:
		return nil
	}
}

// PolyStatusExtras is merged into poly_status broadcasts.
type PolyStatusExtras struct {
	OrderbookNextRetryAt      *int64    `json:"orderbookNextRetryAt,omitempty"`
	OrderbookReconnectAttempt int       `json:"orderbookReconnectAttempt,omitempty"`
	UserNextRetryAt           *int64    `json:"userNextRetryAt,omitempty"`
	UserReconnectAttempt      int       `json:"userReconnectAttempt,omitempty"`
	UserWsLastIssue           string    `json:"userWsLastIssue,omitempty"`
	WSEvents                  []WSEvent `json:"wsEvents,omitempty"`
}

func (c *WSMetaCollector) Snapshot(userIssue string) PolyStatusExtras {
	c.mu.Lock()
	defer c.mu.Unlock()
	var events []WSEvent
	events = append(events, c.ob.events...)
	events = append(events, c.us.events...)
	if len(events) > wsEventCap {
		events = events[:wsEventCap]
	}
	out := PolyStatusExtras{
		OrderbookReconnectAttempt: c.ob.attempt,
		UserReconnectAttempt:      c.us.attempt,
		UserWsLastIssue:           userIssue,
		WSEvents:                  events,
	}
	if c.ob.nextRetryAtMs > 0 {
		v := c.ob.nextRetryAtMs
		out.OrderbookNextRetryAt = &v
	}
	if c.us.nextRetryAtMs > 0 {
		v := c.us.nextRetryAtMs
		out.UserNextRetryAt = &v
	}
	return out
}
