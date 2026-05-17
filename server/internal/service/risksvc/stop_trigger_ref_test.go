package risksvc

import "testing"

func TestStopTriggerReferenceCents(t *testing.T) {
	t.Parallel()
	if g := stopTriggerReferenceCents(61, 62); g != 61 {
		t.Fatalf("prefer bid: got %v want 61", g)
	}
	if g := stopTriggerReferenceCents(0, 0.1); g != 0.1 {
		t.Fatalf("empty bid use mark: got %v want 0.1", g)
	}
	if g := stopTriggerReferenceCents(0, 0); g != 0 {
		t.Fatalf("no book: got %v want 0", g)
	}
}
