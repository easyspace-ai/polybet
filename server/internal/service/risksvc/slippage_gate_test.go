package risksvc

import (
	"math"
	"testing"
)

func TestProjectedSellSlippageBps(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name           string
		evalBidCents   float64
		sellExtraTicks int
		wantBpsApprox  float64 // tolerance ±0.5
	}{
		// 70¢ contract, 5 ticks (=5¢) deeper → 5/70*10000 ≈ 714 bps
		{"high_price_5_ticks", 70, 5, 714},
		// 50¢ contract, 5 ticks → 5/50*10000 = 1000 bps
		{"mid_price_5_ticks", 50, 5, 1000},
		// 5¢ contract, 5 ticks → 5/5*10000 = 10000 bps (catastrophic)
		{"low_price_5_ticks", 5, 5, 10000},
		// 50¢ contract, 0 ticks → 0 bps (limit at bestBid).
		{"zero_ticks", 50, 0, 0},
		// Negative ticks treated as 0 (defensive).
		{"negative_ticks", 50, -3, 0},
		// Zero / negative bid → 0 (caller must short-circuit).
		{"zero_bid", 0, 5, 0},
		{"negative_bid", -1, 5, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := projectedSellSlippageBps(tc.evalBidCents, tc.sellExtraTicks)
			if math.Abs(got-tc.wantBpsApprox) > 0.5 {
				t.Fatalf("got %v want ~%v (±0.5)", got, tc.wantBpsApprox)
			}
		})
	}
}

func TestIsCloseSlippageCapErr(t *testing.T) {
	t.Parallel()
	if !IsCloseSlippageCapErr(errCloseSlippageCap) {
		t.Fatal("sentinel err should match")
	}
	if IsCloseSlippageCapErr(nil) {
		t.Fatal("nil should not match")
	}
}
