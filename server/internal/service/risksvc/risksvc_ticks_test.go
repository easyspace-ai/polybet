package risksvc

import "testing"

func TestEffectiveFokSellExtraTicks(t *testing.T) {
	if v := effectiveFokSellExtraTicks(5, 0); v != 5 {
		t.Fatalf("got %d want 5", v)
	}
	if v := effectiveFokSellExtraTicks(5, 3); v != 8 {
		t.Fatalf("got %d want 8", v)
	}
	if v := effectiveFokSellExtraTicks(5, 99); v != 13 {
		t.Fatalf("got %d want 13 (5+8 capped by min attempts slice)", v)
	}
	if v := effectiveFokSellExtraTicks(25, 8); v != 30 {
		t.Fatalf("got %d want 30", v)
	}
}
