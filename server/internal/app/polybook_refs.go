package app

import (
	"sync"

	"github.com/easyspace-ai/polybet/internal/store"
)

type polyBookRefs struct {
	mu   sync.Mutex
	refs map[string]int
}

func newPolyBookRefs() *polyBookRefs {
	return &polyBookRefs{refs: make(map[string]int)}
}

func (r *polyBookRefs) add(tokenID string) int {
	tid := store.NormalizeRiskCLOBTokenID(tokenID)
	if tid == "" {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refs[tid]++
	return r.refs[tid]
}

func (r *polyBookRefs) count(tokenID string) int {
	tid := store.NormalizeRiskCLOBTokenID(tokenID)
	if tid == "" {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.refs[tid]
}

func (r *polyBookRefs) remove(tokenID string) int {
	tid := store.NormalizeRiskCLOBTokenID(tokenID)
	if tid == "" {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.refs[tid]
	if n <= 1 {
		delete(r.refs, tid)
		return 0
	}
	r.refs[tid] = n - 1
	return r.refs[tid]
}

// PolyBookClientSubscribe records a dashboard/risk WS client book subscription.
func (a *App) PolyBookClientSubscribe(tokenID string) {
	if a == nil {
		return
	}
	if a.polyBookRefs == nil {
		a.polyBookRefs = newPolyBookRefs()
	}
	a.polyBookRefs.add(tokenID)
	a.EnsureOrderbookToken(tokenID)
}

// PolyBookClientUnsubscribe drops one client ref and releases upstream when idle.
func (a *App) PolyBookClientUnsubscribe(tokenID string) {
	if a == nil || a.polyBookRefs == nil {
		return
	}
	if a.polyBookRefs.remove(tokenID) == 0 {
		a.tryReleaseOrderbookToken(tokenID)
	}
}

func (a *App) tryReleaseOrderbookToken(tokenID string) {
	if a.StopLoss != nil {
		a.StopLoss.TryReleaseToken(tokenID)
		a.StopLoss.NotifyPositionsChanged()
	}
}

// NotifyRiskPositionsChanged wakes the stop-loss reconcile loop immediately.
func (a *App) NotifyRiskPositionsChanged() {
	if a != nil && a.StopLoss != nil {
		a.StopLoss.NotifyPositionsChanged()
	}
}
