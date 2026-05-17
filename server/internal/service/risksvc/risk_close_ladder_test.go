package risksvc

import "testing"

func TestTierForAttempt_StickyLast(t *testing.T) {
	t.Parallel()
	tiers := []ladderTier{
		{Type: riskCloseModeFOKSell, ExtraTicks: 2},
		{Type: riskCloseModeFAKSell, ExtraTicks: 5},
		{Type: riskCloseModeHedgeFOKBuy},
	}
	cases := []struct {
		attempts int
		want     string
	}{
		{0, riskCloseModeFOKSell},
		{1, riskCloseModeFAKSell},
		{2, riskCloseModeHedgeFOKBuy},
		{3, riskCloseModeHedgeFOKBuy}, // sticky last
		{99, riskCloseModeHedgeFOKBuy},
		{-1, riskCloseModeFOKSell}, // negative clamps to 0
	}
	for _, tc := range cases {
		if got := tierForAttempt(tiers, tc.attempts); got.Type != tc.want {
			t.Fatalf("attempts=%d got %q want %q", tc.attempts, got.Type, tc.want)
		}
	}
}

func TestDefaultLadderTiers_HedgeIsLast(t *testing.T) {
	t.Parallel()
	tiers := defaultLadderTiers()
	if len(tiers) == 0 {
		t.Fatal("expected non-empty default ladder")
	}
	last := tiers[len(tiers)-1]
	if last.Type != riskCloseModeHedgeFOKBuy {
		t.Fatalf("default ladder should end with hedge_fok_buy, got %q", last.Type)
	}
	// Tiers should escalate: index 0 must be the gentlest (FOK with smallest ticks).
	if tiers[0].Type != riskCloseModeFOKSell {
		t.Fatalf("default ladder first tier should be fok_sell, got %q", tiers[0].Type)
	}
}
