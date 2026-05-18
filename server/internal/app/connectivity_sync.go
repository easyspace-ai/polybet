package app

import (
	"context"
	"time"
)

// connectivitySyncLoop keeps the in-memory registry aligned with server upstream state.
func (a *App) connectivitySyncLoop(ctx context.Context) {
	defer a.wg.Done()
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if a.Monitor != nil {
				a.Monitor.SyncRegistryFromRisk()
			}
			if a.Conn != nil && a.Hub != nil {
				a.Conn.SetRelayClients(a.Hub.ClientCount() + a.riskHubClients())
			}
			n := a.OpenRiskPositionCount(ctx)
			if a.Conn != nil {
				a.Conn.SetOpenPositionCount(n)
			}
			if a.Cache != nil && a.Conn != nil {
				if ms := a.Cache.LastBookUpdateMs(); ms > 0 {
					a.Conn.SetLastBookUpdateMs(ms)
				}
			}
		}
	}
}

func (a *App) riskHubClients() int {
	if a.RiskHub == nil {
		return 0
	}
	return a.RiskHub.ClientCount()
}
