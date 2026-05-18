package risksvc

import (
	"context"
	"testing"

	"github.com/easyspace-ai/polybet/internal/store"
)

// TestDefaultStopPctMatchesStoreFallback locks the invariant that the
// service-layer fallback equals the SQL COALESCE fallback. Drift between
// these two values previously meant legacy positions ran with a tighter
// stop than the dashboard reported. Any future change to one constant
// must also change the other; otherwise this test fails.
func TestResolveStopLossPctZeroEntry(t *testing.T) {
	t.Parallel()
	if got := resolveStopLossPct(context.Background(), nil, 0); got != 0 {
		t.Fatalf("resolveStopLossPct(0)=%v want 0", got)
	}
}

func TestShouldActivateTrailingStop(t *testing.T) {
	t.Parallel()
	if !shouldActivateTrailingStop(0, 20, 55) {
		t.Fatal("expected activate when avg entry was unknown")
	}
	if shouldActivateTrailingStop(55, 30, 55) {
		t.Fatal("expected no activate when already armed")
	}
	if !shouldActivateTrailingStop(55, 0, 55) {
		t.Fatal("expected activate when stop unset")
	}
}

func TestDefaultStopPctMatchesStoreFallback(t *testing.T) {
	t.Parallel()
	if defaultStopPct != store.DefaultStopLossPct {
		t.Fatalf("defaultStopPct=%v != store.DefaultStopLossPct=%v", defaultStopPct, store.DefaultStopLossPct)
	}
	if defaultStopPct <= 0 || defaultStopPct >= 100 {
		t.Fatalf("defaultStopPct out of plausible range: %v", defaultStopPct)
	}
}
