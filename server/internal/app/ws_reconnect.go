package app

import (
	"strings"
	"sync"
	"time"

	"github.com/easyspace-ai/polybet/internal/marketstream"
)

const forceReconnectCooldown = 10 * time.Second

var (
	lastForceReconnectOb   time.Time
	lastForceReconnectUser time.Time
	forceReconnectMu       sync.Mutex
)

func (a *App) setActiveUserStream(u *marketstream.UserStream) {
	a.userStreamMu.Lock()
	a.activeUserWS = u
	a.userStreamMu.Unlock()
}

func (a *App) clearActiveUserStream() {
	a.userStreamMu.Lock()
	a.activeUserWS = nil
	a.userStreamMu.Unlock()
}

// EnsureOrderbookToken subscribes the stop-loss market WS to one token when the
// risk dashboard requests orderbook data and reconcile has not caught up yet.
func (a *App) EnsureOrderbookToken(tokenID string) {
	if a.StopLoss != nil {
		a.StopLoss.EnsureTokenSubscribed(tokenID)
	}
}

// ForceWSReconnect triggers upstream reconnect for orderbook, user, or all.
// A per-channel cooldown prevents multiple dashboard tabs from stampeding the
// upstream connection with overlapping force-reconnect requests.
// Returns true when the reconnect was actually triggered, false if it was
// ignored because the channel is still within its cooldown window.
func (a *App) ForceWSReconnect(channel string) bool {
	executed := false
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case "orderbook", "ob", "market":
		forceReconnectMu.Lock()
		if time.Since(lastForceReconnectOb) < forceReconnectCooldown {
			forceReconnectMu.Unlock()
			return false
		}
		lastForceReconnectOb = time.Now()
		forceReconnectMu.Unlock()
		executed = true

		if a.StopLoss != nil {
			a.StopLoss.ForceMarketReconnect()
		}
		if a.Risk.WSMeta != nil {
			a.Risk.WSMeta.Record("orderbook", "info", "manual reconnect requested")
		}
	case "user":
		forceReconnectMu.Lock()
		if time.Since(lastForceReconnectUser) < forceReconnectCooldown {
			forceReconnectMu.Unlock()
			return false
		}
		lastForceReconnectUser = time.Now()
		forceReconnectMu.Unlock()
		executed = true

		a.userStreamMu.Lock()
		u := a.activeUserWS
		a.userStreamMu.Unlock()
		if u != nil {
			u.ForceReconnect()
		}
		if a.Risk.WSMeta != nil {
			a.Risk.WSMeta.Record("user", "info", "manual reconnect requested")
		}
	case "all", "":
		ob := a.ForceWSReconnect("orderbook")
		us := a.ForceWSReconnect("user")
		return ob || us
	default:
		return a.ForceWSReconnect("all")
	}
	if executed {
		a.broadcastPolyStatusAsync()
	}
	return executed
}
