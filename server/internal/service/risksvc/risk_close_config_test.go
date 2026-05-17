package risksvc

import "testing"

func TestMarkPrice01FromEvalCents(t *testing.T) {
	if v := markPrice01FromEvalCents(40, 50); v != 0.5 {
		t.Fatalf("got %v", v)
	}
	if v := markPrice01FromEvalCents(0, 30); v != 0.3 {
		t.Fatalf("got %v", v)
	}
	if v := markPrice01FromEvalCents(0, 0); v != 0 {
		t.Fatalf("got %v", v)
	}
}
