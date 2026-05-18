package monitor

import (
	"context"
	"sync"
	"time"

	"github.com/easyspace-ai/polybet/internal/service/risksvc"
	"github.com/easyspace-ai/polybet/internal/storage"
)

const taskWatchInterval = 3 * time.Second

type watchEntry struct {
	positionID string
	registered time.Time
}

// TaskWatcher polls task + position state until close succeeds or gives up.
type TaskWatcher struct {
	st   *storage.Backend
	risk *risksvc.Service

	mu      sync.Mutex
	entries map[string]*watchEntry
}

func NewTaskWatcher(st *storage.Backend, risk *risksvc.Service) *TaskWatcher {
	return &TaskWatcher{
		st:      st,
		risk:    risk,
		entries: make(map[string]*watchEntry),
	}
}

func (w *TaskWatcher) Register(positionID string) {
	if positionID == "" {
		return
	}
	w.mu.Lock()
	w.entries[positionID] = &watchEntry{
		positionID: positionID,
		registered: time.Now().UTC(),
	}
	w.mu.Unlock()
}

func (w *TaskWatcher) Run(ctx context.Context) {
	t := time.NewTicker(taskWatchInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.tick(ctx)
		}
	}
}

func (w *TaskWatcher) tick(ctx context.Context) {
	w.mu.Lock()
	list := make([]*watchEntry, 0, len(w.entries))
	for _, e := range w.entries {
		list = append(list, e)
	}
	w.mu.Unlock()

	for _, e := range list {
		if w.done(ctx, e) {
			w.mu.Lock()
			delete(w.entries, e.positionID)
			w.mu.Unlock()
		}
	}
}

func (w *TaskWatcher) done(ctx context.Context, e *watchEntry) bool {
	if w.st == nil {
		return true
	}
	if time.Since(e.registered) > 30*time.Minute {
		return true
	}
	if e.positionID != "" && w.risk != nil {
		acct, _ := w.st.GetActivePolymarketAccount(ctx)
		if acct != nil {
			_ = w.risk.SyncPositionsFromDataAPI(ctx, acct.ID)
		}
		p, err := w.st.GetRiskPosition(ctx, e.positionID)
		if err == nil && p != nil && p.Status != "open" {
			return true
		}
	}
	return false
}
