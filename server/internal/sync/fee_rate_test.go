package sync

import (
	"encoding/json"
	"testing"
)

func fee(s string) *optionalFee {
	cp := s
	return &optionalFee{s: &cp}
}

func TestMarketFeeRate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		m    *gammaMarket
		want float64
		ok   bool
	}{
		{"nil_market", nil, 0, false},
		{"all_missing", &gammaMarket{}, 0, false},
		{"feeRateBps_200", &gammaMarket{FeeRateBps: fee("200")}, 0.02, true},
		{"feeRateBps_0", &gammaMarket{FeeRateBps: fee("0")}, 0, true},
		{"feeRate_fraction", &gammaMarket{FeeRate: fee("0.025")}, 0.025, true},
		// Both present → bps takes precedence (more authoritative on Polymarket).
		{"bps_wins_over_fraction", &gammaMarket{
			FeeRateBps: fee("100"),
			FeeRate:    fee("0.99"),
		}, 0.01, true},
		// takerBaseFee is the legacy fallback.
		{"taker_base_fee_legacy", &gammaMarket{TakerBaseFee: fee("0.015")}, 0.015, true},
		// Out-of-range values are rejected (defensive).
		{"reject_one", &gammaMarket{FeeRate: fee("1.0")}, 0, false},
		{"reject_negative", &gammaMarket{FeeRate: fee("-0.01")}, 0, false},
		{"reject_garbage", &gammaMarket{FeeRate: fee("not-a-number")}, 0, false},
		{"reject_empty", &gammaMarket{FeeRate: fee("   ")}, 0, false},
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

func TestGammaMarketFeeFieldsNumericJSON(t *testing.T) {
	// Gamma sometimes returns fee fields as JSON numbers (e.g. NHL series).
	const payload = `{
		"conditionId": "0x1",
		"question": "q",
		"clobTokenIds": "[\"t1\",\"t2\"]",
		"outcomes": "[\"A\",\"B\"]",
		"outcomePrices": "[\"0.5\",\"0.5\"]",
		"active": true,
		"closed": false,
		"liquidity": "1000",
		"feeRateBps": 200,
		"feeRate": 0.025,
		"takerBaseFee": 0.015
	}`
	var m gammaMarket
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		t.Fatal(err)
	}
	if v, ok := marketFeeRate(&m); !ok || v != 0.02 {
		t.Fatalf("feeRateBps number: got %v ok=%v want 0.02 true", v, ok)
	}
	onlyTaker := `{
		"conditionId": "0x2",
		"question": "q",
		"clobTokenIds": "[\"t1\",\"t2\"]",
		"outcomes": "[\"A\",\"B\"]",
		"outcomePrices": "[\"0.5\",\"0.5\"]",
		"active": true,
		"closed": false,
		"liquidity": "0",
		"takerBaseFee": 0.015
	}`
	var m2 gammaMarket
	if err := json.Unmarshal([]byte(onlyTaker), &m2); err != nil {
		t.Fatal(err)
	}
	if v, ok := marketFeeRate(&m2); !ok || v != 0.015 {
		t.Fatalf("takerBaseFee number: got %v ok=%v want 0.015 true", v, ok)
	}
}
