package httpserver

import (
	"context"

	"github.com/easyspace-ai/polybet/internal/bookcache"
	"github.com/easyspace-ai/polybet/internal/service/risksvc"
)

func buildWSStatusJSON(risk *risksvc.Service, cache *bookcache.Cache, dashClients int, openPositions int) map[string]any {
	out := map[string]any{
		"dashConnected":           dashClients > 0,
		"dashClients":             dashClients,
		"polyOrderbookConnected":  false,
		"polyOrderbookConnecting": false,
		"polyUserConnected":       false,
		"polyUserConnecting":      false,
		"openPositionsCount":      openPositions,
	}
	if risk == nil {
		return out
	}
	out["polyOrderbookConnected"] = risk.OrderbookWSConnected()
	out["polyOrderbookConnecting"] = risk.OrderbookWSConnecting()
	out["polyUserConnected"] = risk.UserWSConnected()
	out["polyUserConnecting"] = risk.UserWSConnecting()
	issue := ""
	risk.UserWSLastIssue(&issue)
	if risk.WSMeta != nil {
		ex := risk.WSMeta.Snapshot(issue)
		out["orderbookReconnectAttempt"] = ex.OrderbookReconnectAttempt
		out["userReconnectAttempt"] = ex.UserReconnectAttempt
		if ex.OrderbookNextRetryAt != nil {
			out["orderbookNextRetryAt"] = *ex.OrderbookNextRetryAt
		}
		if ex.UserNextRetryAt != nil {
			out["userNextRetryAt"] = *ex.UserNextRetryAt
		}
		if ex.UserWsLastIssue != "" {
			out["userWsLastIssue"] = ex.UserWsLastIssue
		}
		if len(ex.WSEvents) > 0 {
			out["wsEvents"] = ex.WSEvents
		}
	}
	if cache != nil {
		if ms := cache.LastBookUpdateMs(); ms > 0 {
			out["lastBookUpdateMs"] = ms
		}
	}
	return out
}

type wsApp interface {
	ForceWSReconnect(channel string)
	OpenRiskPositionCount(ctx context.Context) int
}
