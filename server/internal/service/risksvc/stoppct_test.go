package risksvc

import (
	"testing"

	"github.com/easyspace-ai/polybet/internal/store"
)

// TestDefaultStopPctMatchesStoreFallback locks the invariant that the
// service-layer fallback equals the SQL COALESCE fallback. Drift between
// these two values previously meant legacy positions ran with a tighter
// stop than the dashboard reported. Any future change to one constant
// must also change the other; otherwise this test fails.
func TestDefaultStopPctMatchesStoreFallback(t *testing.T) {
	t.Parallel()
	if defaultStopPct != store.DefaultStopLossPct {
		t.Fatalf("defaultStopPct=%v != store.DefaultStopLossPct=%v", defaultStopPct, store.DefaultStopLossPct)
	}
	if defaultStopPct <= 0 || defaultStopPct >= 100 {
		t.Fatalf("defaultStopPct out of plausible range: %v", defaultStopPct)
	}
}
