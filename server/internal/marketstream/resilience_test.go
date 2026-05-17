package marketstream

import (
	"testing"
	"time"
)

func TestReconnectDelayForAttempt_exponentialCap(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BackoffBase = time.Second
	cfg.BackoffMax = 8 * time.Second
	cfg.BackoffJitterPct = 0
	d1 := ReconnectDelayForAttempt(cfg, 1)
	if d1 != time.Second {
		t.Fatalf("attempt 1: got %v want 1s", d1)
	}
	d3 := ReconnectDelayForAttempt(cfg, 3)
	if d3 != 4*time.Second {
		t.Fatalf("attempt 3: got %v want 4s", d3)
	}
	d5 := ReconnectDelayForAttempt(cfg, 5)
	if d5 != 8*time.Second {
		t.Fatalf("attempt 5: got %v want capped 8s", d5)
	}
}

func TestMaxReconnectExceeded_unlimited(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxReconnectAttempts = 0
	if maxReconnectExceeded(cfg, 100) {
		t.Fatal("0 max attempts should mean unlimited")
	}
}
