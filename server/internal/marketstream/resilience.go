package marketstream

import (
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ReconnectDelayForAttempt returns exponential backoff delay with jitter.
func ReconnectDelayForAttempt(cfg *Config, attempt int) time.Duration {
	if cfg == nil || attempt < 1 {
		attempt = 1
	}
	base := cfg.BackoffBase
	if base <= 0 {
		base = cfg.ReconnectDelay
	}
	if base <= 0 {
		base = time.Second
	}
	max := cfg.BackoffMax
	if max <= 0 {
		max = cfg.MaxReconnectDelay
	}
	if max <= 0 {
		max = 60 * time.Second
	}
	exp := float64(base) * math.Pow(2, float64(attempt-1))
	delay := time.Duration(exp)
	if delay > max {
		delay = max
	}
	if cfg.BackoffJitterPct > 0 {
		jitter := 1.0 + (rand.Float64()*2-1)*float64(cfg.BackoffJitterPct)/100.0
		return time.Duration(float64(delay) * jitter)
	}
	return delay
}

func maxReconnectExceeded(cfg *Config, attempts int) bool {
	if cfg == nil || cfg.MaxReconnectAttempts <= 0 {
		return false
	}
	return attempts > cfg.MaxReconnectAttempts
}

// sleepWatchdog detects OS sleep/wake via wall-clock drift on a ticker.
type sleepWatchdog struct {
	threshold time.Duration
	onWake    func()
	stopCh    chan struct{}
	doneCh    chan struct{}
	mu        sync.Mutex
}

func startSleepWatchdog(threshold time.Duration, onWake func()) *sleepWatchdog {
	if threshold <= 0 {
		threshold = 5 * time.Second
	}
	w := &sleepWatchdog{
		threshold: threshold,
		onWake:    onWake,
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}
	interval := threshold / 2
	if interval < time.Second {
		interval = time.Second
	}
	go func() {
		defer close(w.doneCh)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		var last time.Time
		for {
			select {
			case <-w.stopCh:
				return
			case now := <-ticker.C:
				if !last.IsZero() {
					delta := now.Sub(last) - interval
					if delta > w.threshold {
						if w.onWake != nil {
							w.onWake()
						}
					}
				}
				last = now
			}
		}
	}()
	return w
}

func (w *sleepWatchdog) stop() {
	select {
	case <-w.stopCh:
	default:
		close(w.stopCh)
	}
	<-w.doneCh
}

// pongWatchdog closes the connection if no pong within PongTimeout.
func startPongWatchdog(cfg *Config, getConn func() *websocket.Conn, getLastPong func() time.Time, onStale func()) *sleepWatchdog {
	// Reuse sleepWatchdog struct as a stoppable goroutine holder.
	if cfg == nil || cfg.PongTimeout <= 0 {
		return nil
	}
	w := &sleepWatchdog{
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
	go func() {
		defer close(w.doneCh)
		ticker := time.NewTicker(cfg.PongTimeout / 3)
		if ticker.C == nil {
			ticker = time.NewTicker(10 * time.Second)
		}
		defer ticker.Stop()
		for {
			select {
			case <-w.stopCh:
				return
			case <-ticker.C:
				conn := getConn()
				if conn == nil {
					continue
				}
				if time.Since(getLastPong()) > cfg.PongTimeout {
					if onStale != nil {
						onStale()
					}
				}
			}
		}
	}()
	return w
}
