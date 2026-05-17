package sync

import "testing"

func TestMarketFeeRate(t *testing.T) {
	t.Parallel()
	strPtr := func(s string) *string { return &s }
	cases := []struct {
		name string
		m    *gammaMarket
		want float64
		ok   bool
	}{
		{"nil_market", nil, 0, false},
		{"all_missing", &gammaMarket{}, 0, false},
		{"feeRateBps_200", &gammaMarket{FeeRateBps: strPtr("200")}, 0.02, true},
		{"feeRateBps_0", &gammaMarket{FeeRateBps: strPtr("0")}, 0, true},
		{"feeRate_fraction", &gammaMarket{FeeRate: strPtr("0.025")}, 0.025, true},
		// Both present → bps takes precedence (more authoritative on Polymarket).
		{"bps_wins_over_fraction", &gammaMarket{
			FeeRateBps: strPtr("100"),
			FeeRate:    strPtr("0.99"),
		}, 0.01, true},
		// takerBaseFee is the legacy fallback.
		{"taker_base_fee_legacy", &gammaMarket{TakerBaseFee: strPtr("0.015")}, 0.015, true},
		// Out-of-range values are rejected (defensive).
		{"reject_one", &gammaMarket{FeeRate: strPtr("1.0")}, 0, false},
		{"reject_negative", &gammaMarket{FeeRate: strPtr("-0.01")}, 0, false},
		{"reject_garbage", &gammaMarket{FeeRate: strPtr("not-a-number")}, 0, false},
		{"reject_empty", &gammaMarket{FeeRate: strPtr("   ")}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := marketFeeRate(tc.m)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("got %v ok=%v want %v ok=%v", got, ok, tc.want, tc.ok)
			}
		})
	}
}
