package connectivity

import (
	"sync"
	"time"
)

const ClientHeartbeatStale = 45 * time.Second

// Registry holds the in-memory connectivity snapshot (single source of truth).
type Registry struct {
	mu       sync.RWMutex
	snap     Snapshot
	onChange func(Snapshot)
}

func NewRegistry() *Registry {
	return &Registry{
		snap: Snapshot{
			Owner: OwnerServer,
			User: ChannelState{
				Display: DisplayDisconnected,
			},
			Orderbook: ChannelState{
				Display: DisplayStandby,
			},
		},
	}
}

func (r *Registry) SetOnChange(fn func(Snapshot)) {
	r.mu.Lock()
	r.onChange = fn
	r.mu.Unlock()
}

func (r *Registry) Snapshot() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.snap
}

func (r *Registry) update(mut func(*Snapshot)) {
	r.mu.Lock()
	mut(&r.snap)
	r.applyDerived(&r.snap)
	s := r.snap
	fn := r.onChange
	r.mu.Unlock()
	if fn != nil {
		fn(s)
	}
}

func (r *Registry) applyDerived(s *Snapshot) {
	openN := s.OpenPositionCount
	s.Orderbook.Required = openN > 0
	s.User.Required = true

	switch s.Owner {
	case OwnerClient:
		if !s.LastClientHeartbeat.IsZero() && time.Since(s.LastClientHeartbeat) <= ClientHeartbeatStale {
			s.User.Connected = s.ClientUserConnected
			s.User.Connecting = !s.ClientUserConnected
			s.Orderbook.Connected = s.ClientOrderbookConnected
			s.Orderbook.Connecting = !s.ClientOrderbookConnected && s.Orderbook.Required
		} else {
			s.Owner = OwnerServer
		}
	}

	if s.Owner == OwnerClient && !s.LastClientHeartbeat.IsZero() && time.Since(s.LastClientHeartbeat) <= ClientHeartbeatStale {
		s.User.Display = displayFor(s.User.Connected, s.User.Connecting, s.User.Required)
		if !s.Orderbook.Required {
			s.Orderbook.Display = DisplayStandby
		} else {
			s.Orderbook.Display = displayFor(s.Orderbook.Connected, s.Orderbook.Connecting, true)
		}
		return
	}

	// Server-owned (or stale client heartbeat): keep Connected/Connecting from last server sync.
	s.User.Display = displayFor(s.User.Connected, s.User.Connecting, s.User.Required)
	if !s.Orderbook.Required {
		s.Orderbook.Display = DisplayStandby
	} else {
		s.Orderbook.Display = displayFor(s.Orderbook.Connected, s.Orderbook.Connecting, true)
	}
}

func displayFor(connected, connecting, required bool) ChannelDisplay {
	if !required {
		return DisplayStandby
	}
	if connected {
		return DisplayConnected
	}
	if connecting {
		return DisplayConnecting
	}
	return DisplayDisconnected
}

func (r *Registry) SetRelay(dashClients int) {
	r.update(func(s *Snapshot) {
		s.DashClients = dashClients
		s.RelayConnected = dashClients > 0
	})
}

func (r *Registry) SetOpenPositionCount(n int) {
	r.update(func(s *Snapshot) {
		s.OpenPositionCount = n
	})
}

func (r *Registry) SetLastBookUpdateMs(ms int64) {
	r.update(func(s *Snapshot) {
		s.LastBookUpdateMs = ms
	})
}

// ClientHeartbeat reports browser-owned CLOB upstream state.
func (r *Registry) ClientHeartbeat(userConnected, obConnected bool, subscribedTokens int) {
	r.update(func(s *Snapshot) {
		s.Owner = OwnerClient
		s.LastClientHeartbeat = time.Now().UTC()
		s.ClientUserConnected = userConnected
		s.ClientOrderbookConnected = obConnected
		s.SubscribedTokenCount = subscribedTokens
	})
}

// SyncServerUpstream copies risksvc / stoplossengine upstream flags (fallback path).
func (r *Registry) SyncServerUpstream(userConnected, userConnecting, obConnected, obConnecting bool, issue string) {
	r.update(func(s *Snapshot) {
		if s.Owner == OwnerClient && !s.LastClientHeartbeat.IsZero() && time.Since(s.LastClientHeartbeat) <= ClientHeartbeatStale {
			return
		}
		s.Owner = OwnerServer
		s.User.Connected = userConnected
		s.User.Connecting = userConnecting
		s.Orderbook.Connected = obConnected
		s.Orderbook.Connecting = obConnecting
		if issue != "" {
			s.UserWsLastIssue = issue
		}
	})
}

func (r *Registry) SetWSMeta(orderbookAttempt, userAttempt int, obRetryMs, userRetryMs int64) {
	r.update(func(s *Snapshot) {
		s.OrderbookReconnectAttempt = orderbookAttempt
		s.UserReconnectAttempt = userAttempt
		s.OrderbookNextRetryAtMs = obRetryMs
		s.UserNextRetryAtMs = userRetryMs
	})
}
