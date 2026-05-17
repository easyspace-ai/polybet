package risksvc

import (
	"testing"
)

func TestSetWSMarketDownAndClear(t *testing.T) {
	t.Parallel()
	s := &Service{deg: newDegradedState()}

	if down, _ := s.WSMarketDown(); down {
		t.Fatalf("expected not down at init")
	}
	s.SetWSMarketDown("max reconnect attempts reached (10)")
	if down, reason := s.WSMarketDown(); !down || reason == "" {
		t.Fatalf("expected down with reason, got down=%v reason=%q", down, reason)
	}
	s.ClearWSMarketDown()
	if down, _ := s.WSMarketDown(); down {
		t.Fatalf("expected cleared after ClearWSMarketDown")
	}
}

func TestAutoHaltStatus(t *testing.T) {
	t.Parallel()
	s := &Service{deg: newDegradedState()}

	if h, _ := s.AutoHaltStatus(); h {
		t.Fatalf("expected not halted at init")
	}
	s.SetAutoHalted(true, "loss exceeded")
	if h, r := s.AutoHaltStatus(); !h || r != "loss exceeded" {
		t.Fatalf("got halted=%v reason=%q", h, r)
	}
	// Calling again with same value must remain halted (idempotent).
	s.SetAutoHalted(true, "second call")
	if h, _ := s.AutoHaltStatus(); !h {
		t.Fatalf("expected still halted")
	}
	s.SetAutoHalted(false, "")
	if h, _ := s.AutoHaltStatus(); h {
		t.Fatalf("expected cleared")
	}
}

func TestMarkBookTickRecordsRecency(t *testing.T) {
	t.Parallel()
	s := &Service{deg: newDegradedState()}
	if !s.LastBookTickAt().IsZero() {
		t.Fatalf("expected zero time at init")
	}
	s.MarkBookTick()
	if s.LastBookTickAt().IsZero() {
		t.Fatalf("expected non-zero time after MarkBookTick")
	}
}
