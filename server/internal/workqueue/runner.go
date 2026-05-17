package workqueue

import (
	"context"
	"sync"
)

// Runner ensures at most one background job per key runs at a time.
type Runner struct {
	mu      sync.Mutex
	running map[string]bool
}

func New() *Runner {
	return &Runner{running: make(map[string]bool)}
}

// Run starts fn in a new goroutine when no job with the same key is running.
// Returns true when this call started the job, false when one is already active.
func (r *Runner) Run(key string, fn func(context.Context) error) (started bool) {
	if r == nil {
		go func() { _ = fn(context.Background()) }()
		return true
	}
	r.mu.Lock()
	if r.running[key] {
		r.mu.Unlock()
		return false
	}
	r.running[key] = true
	r.mu.Unlock()

	go func() {
		defer func() {
			r.mu.Lock()
			delete(r.running, key)
			r.mu.Unlock()
		}()
		_ = fn(context.Background())
	}()
	return true
}
