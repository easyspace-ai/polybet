package risksvc

import "testing"

func TestLooksLikeJunkTop(t *testing.T) {
	if !looksLikeJunkTop(0, 0.01) {
		t.Fatal("expected junk top")
	}
	if looksLikeJunkTop(0.54, 0.55) {
		t.Fatal("healthy top should not be junk")
	}
}
