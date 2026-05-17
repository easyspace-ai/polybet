package risksvc

import (
	"testing"
	"time"
)

// TestSetNowFnForTestPinsTheClock confirms nowFn injection works end-to-end.
// The cooldown active-check is a small but real consumer of s.now() so a
// pinned clock that crosses the deadline lets us assert the behaviour
// without a real time.Sleep in the test.
func TestSetNowFnForTestPinsTheClock(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, time.May, 17, 12, 0, 0, 0, time.UTC)
	clock := t0
	s := &Service{
		slMktEndedCool: map[string]time.Time{
			"pos1": t0.Add(60 * time.Second), // cooldown ends 60s from now
		},
	}
	s.nowFn = func() time.Time { return clock }

	if !s.stopLossMarketEndedCooldownActive("pos1") {
		t.Fatal("cooldown should be active at t0")
	}
	// Travel 30s forward: still within window.
	clock = t0.Add(30 * time.Second)
	if !s.stopLossMarketEndedCooldownActive("pos1") {
		t.Fatal("cooldown should still be active at t0+30s")
	}
	// Travel 90s forward: past the deadline. Should also delete the entry.
	clock = t0.Add(90 * time.Second)
	if s.stopLossMarketEndedCooldownActive("pos1") {
		t.Fatal("cooldown should be expired at t0+90s")
	}
	if _, ok := s.slMktEndedCool["pos1"]; ok {
		t.Fatal("expired entry should be lazily evicted on read")
	}

	// SetNowFnForTest(nil) restores time.Now.
	s.SetNowFnForTest(nil)
	if s.nowFn == nil {
		t.Fatal("nil should restore default, not zero the field")
	}
	// Ensure the restored clock is roughly real time.
	delta := time.Since(s.now())
	if delta < 0 || delta > time.Second {
		t.Fatalf("restored clock not in real time: delta=%v", delta)
	}
}
