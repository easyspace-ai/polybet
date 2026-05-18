package app

import (
	"time"

	domainconn "github.com/easyspace-ai/polybet/internal/domain/connectivity"
)

// clientOwnsClobWS is true when the dashboard recently reported client-side CLOB upstream.
func (a *App) clientOwnsClobWS() bool {
	if a == nil || a.ConnRegistry == nil {
		return false
	}
	s := a.ConnRegistry.Snapshot()
	return s.Owner == domainconn.OwnerClient &&
		!s.LastClientHeartbeat.IsZero() &&
		time.Since(s.LastClientHeartbeat) <= domainconn.ClientHeartbeatStale
}
