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
		"type":                    "poly_status",
		"polyOrderbookConnected":  a.Risk.OrderbookWSConnected(),
		"polyOrderbookConnecting": a.Risk.OrderbookWSConnecting(),
		"polyUserConnected":       a.Risk.UserWSConnected(),
		"polyUserConnecting":      a.Risk.UserWSConnecting(),
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
	if a.Monitor != nil {
		a.Monitor.SyncRegistryFromRisk()
	}
	if a.Conn != nil {
		a.Conn.BroadcastNow()
	}
}

// broadcastPolyStatusAsync is for hot paths (manual reconnect) where blocking on
// slow dashboard WS clients would stall HTTP handlers.
func (a *App) broadcastPolyStatusAsync() {
	if a.Risk == nil {
		return
	}
	issue := ""
	a.Risk.UserWSLastIssue(&issue)
	extras := a.Risk.WSMeta.Snapshot(issue)
	st := map[string]any{
		"type":                    "poly_status",
		"polyOrderbookConnected":  a.Risk.OrderbookWSConnected(),
		"polyOrderbookConnecting": a.Risk.OrderbookWSConnecting(),
		"polyUserConnected":       a.Risk.UserWSConnected(),
		"polyUserConnecting":      a.Risk.UserWSConnecting(),
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
		a.Hub.BroadcastJSONAsync(st)
	}
	if a.RiskHub != nil {
		a.RiskHub.BroadcastJSONAsync(st)
	}
	if a.Monitor != nil {
		a.Monitor.SyncRegistryFromRisk()
	}
	if a.Conn != nil {
		a.Conn.BroadcastNow()
	}
}
