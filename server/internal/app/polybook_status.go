package app

import (
	"context"
	"time"

	"github.com/easyspace-ai/polybet/internal/polyexec"
	"github.com/easyspace-ai/polybet/internal/riskruntime"
	"github.com/easyspace-ai/polybet/internal/store"
)

const polyBookSubStaleAfter = 15 * time.Second

// PolyBookSubStatus is per-token dashboard/risk WS book subscription health.
type PolyBookSubStatus struct {
	TokenID            string `json:"tokenId"`
	ClientSubscribed   bool   `json:"clientSubscribed"`
	ClientRefs         int    `json:"clientRefs"`
	UpstreamSubscribed bool   `json:"upstreamSubscribed"`
	LastFrameMs        int64  `json:"lastFrameMs,omitempty"`
	Stale              bool   `json:"stale"`
}

// PolyBookClientRefs returns active dashboard/risk WS subscribePolyBook ref count.
func (a *App) PolyBookClientRefs(tokenID string) int {
	if a == nil || a.polyBookRefs == nil {
		return 0
	}
	return a.polyBookRefs.count(tokenID)
}

// PolyBookUpstreamSubscribed reports whether the stop-loss market WS tracks this token.
func (a *App) PolyBookUpstreamSubscribed(tokenID string) bool {
	if a == nil || a.StopLoss == nil {
		return false
	}
	return a.StopLoss.IsTokenSubscribed(tokenID)
}

// PolyBookLastFrameMs returns the bookcache update timestamp for tokenID (unix ms), or 0.
func (a *App) PolyBookLastFrameMs(tokenID string) int64 {
	if a == nil || a.Cache == nil {
		return 0
	}
	return a.Cache.BookUpdatedAtMs(store.NormalizeRiskCLOBTokenID(tokenID))
}

// PolyBookSubStatusFor returns subscription health for one token.
func (a *App) PolyBookSubStatusFor(tokenID string) PolyBookSubStatus {
	tid := store.NormalizeRiskCLOBTokenID(tokenID)
	refs := a.PolyBookClientRefs(tid)
	lastMs := a.PolyBookLastFrameMs(tid)
	stale := lastMs == 0
	if !stale {
		stale = time.Since(time.UnixMilli(lastMs)) > polyBookSubStaleAfter
	}
	return PolyBookSubStatus{
		TokenID:            tid,
		ClientSubscribed:   refs > 0,
		ClientRefs:         refs,
		UpstreamSubscribed: a.PolyBookUpstreamSubscribed(tid),
		LastFrameMs:        lastMs,
		Stale:              stale,
	}
}

// PolyBookSubStatusesFor returns subscription health for each token (deduped, normalized).
func (a *App) PolyBookSubStatusesFor(tokenIDs []string) []map[string]any {
	statuses := a.polyBookSubStatusesFor(tokenIDs)
	out := make([]map[string]any, 0, len(statuses))
	for _, s := range statuses {
		out = append(out, map[string]any{
			"tokenId":            s.TokenID,
			"clientSubscribed":   s.ClientSubscribed,
			"clientRefs":         s.ClientRefs,
			"upstreamSubscribed": s.UpstreamSubscribed,
			"lastFrameMs":        s.LastFrameMs,
			"stale":              s.Stale,
		})
	}
	return out
}

func (a *App) polyBookSubStatusesFor(tokenIDs []string) []PolyBookSubStatus {
	seen := make(map[string]struct{}, len(tokenIDs))
	out := make([]PolyBookSubStatus, 0, len(tokenIDs))
	for _, id := range tokenIDs {
		tid := store.NormalizeRiskCLOBTokenID(id)
		if tid == "" {
			continue
		}
		if _, ok := seen[tid]; ok {
			continue
		}
		seen[tid] = struct{}{}
		out = append(out, a.PolyBookSubStatusFor(tid))
	}
	return out
}

// PublishBookSummaryTick emits a throttled market.book.summary_tick from bookcache top-of-book.
func (a *App) PublishBookSummaryTick(tokenID string) {
	if a == nil || a.RiskRuntime == nil || a.Cache == nil {
		return
	}
	tid := store.NormalizeRiskCLOBTokenID(tokenID)
	if tid == "" {
		return
	}
	bestBid, bestAsk, ok := a.Cache.TopOfBook(tid)
	if !ok && bestBid <= 0 && bestAsk <= 0 {
		return
	}
	bidCents := polyexec.CentsFromPrice01(bestBid)
	askCents := polyexec.CentsFromPrice01(bestAsk)
	acctID := ""
	if acct, err := a.Store.GetActivePolymarketAccount(context.Background()); err == nil && acct != nil {
		acctID = acct.ID
	}
	var positions []riskruntime.BookSummaryPosition
	if a.StopLoss != nil {
		positions = a.StopLoss.MonitoredPositionsForToken(context.Background(), acctID, tid)
	}
	a.RiskRuntime.MaybePublishMarketBookSummary(tid, acctID, bidCents, askCents, positions)
}
