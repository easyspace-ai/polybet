package app

// broadcastPolyStatus pushes upstream WS state (with reconnect metadata) to dashboard clients.
func (a *App) broadcastPolyStatus() {
	if a.Risk == nil {
		return
	}
	issue := ""
	a.Risk.UserWSLastIssue(&issue)
	extras := a.Risk.WSMeta.Snapshot(issue)
	st := map[string]any{
		"type":                   "poly_status",
		"polyOrderbookConnected": a.Risk.OrderbookWSConnected(),
		"polyUserConnected":      a.Risk.UserWSConnected(),
	}
	if extras.OrderbookNextRetryAt != nil {
		st["orderbookNextRetryAt"] = *extras.OrderbookNextRetryAt
	}
	st["orderbookReconnectAttempt"] = extras.OrderbookReconnectAttempt
	if extras.UserNextRetryAt != nil {
		st["userNextRetryAt"] = *extras.UserNextRetryAt
	}
	st["userReconnectAttempt"] = extras.UserReconnectAttempt
	if extras.UserWsLastIssue != "" {
		st["userWsLastIssue"] = extras.UserWsLastIssue
	}
	if len(extras.WSEvents) > 0 {
		st["wsEvents"] = extras.WSEvents
	}
	if a.Hub != nil {
		a.Hub.BroadcastJSON(st)
	}
	if a.RiskHub != nil {
		a.RiskHub.BroadcastJSON(st)
	}
}
