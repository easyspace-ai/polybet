package connectivity

import "time"

// Snapshot is the O(1) read model for dashboard connectivity (no I/O).
type Snapshot struct {
	RelayConnected    bool
	DashClients       int
	Owner             Owner
	User              ChannelState
	Orderbook         ChannelState
	OpenPositionCount int
	LastBookUpdateMs  int64

	UserWsLastIssue             string
	OrderbookReconnectAttempt   int
	UserReconnectAttempt        int
	OrderbookNextRetryAtMs      int64
	UserNextRetryAtMs           int64
	LastClientHeartbeat         time.Time
	SubscribedTokenCount        int
	ClientUserConnected         bool
	ClientOrderbookConnected    bool
}

// LegacyWSStatusJSON maps to the historical GET /api/ws/status shape.
func (s Snapshot) LegacyWSStatusJSON() map[string]any {
	out := map[string]any{
		"dashConnected":           s.RelayConnected,
		"dashClients":             s.DashClients,
		"openPositionsCount":      s.OpenPositionCount,
		"connectivityOwner":       string(s.Owner),
		"polyOrderbookConnected":  s.Orderbook.Connected,
		"polyOrderbookConnecting": s.Orderbook.Connecting,
		"polyUserConnected":       s.User.Connected,
		"polyUserConnecting":      s.User.Connecting,
		"orderbookReconnectAttempt": s.OrderbookReconnectAttempt,
		"userReconnectAttempt":      s.UserReconnectAttempt,
	}
	if s.UserWsLastIssue != "" {
		out["userWsLastIssue"] = s.UserWsLastIssue
	}
	if s.LastBookUpdateMs > 0 {
		out["lastBookUpdateMs"] = s.LastBookUpdateMs
	}
	if s.OrderbookNextRetryAtMs > 0 {
		out["orderbookNextRetryAt"] = s.OrderbookNextRetryAtMs
	}
	if s.UserNextRetryAtMs > 0 {
		out["userNextRetryAt"] = s.UserNextRetryAtMs
	}
	return out
}

// ConnectivitySnapshotJSON is the push payload for connectivity_snapshot WS messages.
func (s Snapshot) ConnectivitySnapshotJSON() map[string]any {
	data := s.LegacyWSStatusJSON()
	data["userDisplay"] = string(s.User.Display)
	data["orderbookDisplay"] = string(s.Orderbook.Display)
	data["subscribedTokenCount"] = s.SubscribedTokenCount
	if !s.LastClientHeartbeat.IsZero() {
		data["lastClientHeartbeat"] = s.LastClientHeartbeat.UTC().Format(time.RFC3339)
	}
	return map[string]any{
		"type": "connectivity_snapshot",
		"data": data,
	}
}
