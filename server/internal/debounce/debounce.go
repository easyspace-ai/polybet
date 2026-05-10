package debounce

import (
	"sync"
	"time"
)

// Debouncer runs fn after quiet period (matches Node 120ms book→risk bridge).
type Debouncer struct {
	mu     sync.Mutex
	timers map[string]*time.Timer
	delay  time.Duration
}

func New(d time.Duration) *Debouncer {
	return &Debouncer{timers: make(map[string]*time.Timer), delay: d}
}

func (d *Debouncer) Trigger(key string, fn func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if t, ok := d.timers[key]; ok {
		t.Stop()
	}
	d.timers[key] = time.AfterFunc(d.delay, func() {
		d.mu.Lock()
		delete(d.timers, key)
		d.mu.Unlock()
		fn()
	})
}
