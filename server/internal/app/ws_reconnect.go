package app

import (
	"strings"

	"github.com/easyspace-ai/polybet/internal/marketstream"
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
func (a *App) ForceWSReconnect(channel string) {
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case "orderbook", "ob", "market":
		if a.StopLoss != nil {
			a.StopLoss.ForceMarketReconnect()
		}
		if a.Risk.WSMeta != nil {
			a.Risk.WSMeta.Record("orderbook", "info", "manual reconnect requested")
		}
	case "user":
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
		a.ForceWSReconnect("orderbook")
		a.ForceWSReconnect("user")
	default:
		a.ForceWSReconnect("all")
	}
	a.broadcastPolyStatus()
}
