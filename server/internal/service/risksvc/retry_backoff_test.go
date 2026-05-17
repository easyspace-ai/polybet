package risksvc

import "testing"

func TestCloseRetryMsForLadder_HedgeUsesLongerFloor(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		attempts int
		tier     string
		minMs    int // expected at least this many ms
		maxMs    int // expected at most this many ms
	}{
		// Hedge tier starts at 30s and doubles, capped at 5 min.
		{"hedge_attempt_1", 1, riskCloseModeHedgeFOKBuy, 30000, 30000},
		{"hedge_attempt_2", 2, riskCloseModeHedgeFOKBuy, 60000, 60000},
		{"hedge_attempt_3", 3, riskCloseModeHedgeFOKBuy, 120000, 120000},
		{"hedge_attempt_5", 5, riskCloseModeHedgeFOKBuy, 300000, 300000}, // cap
		{"hedge_attempt_99", 99, riskCloseModeHedgeFOKBuy, 300000, 300000},
		// Non-hedge tiers fall through to closeRetryMs (capped at 60s).
		{"fok_attempt_1", 1, riskCloseModeFOKSell, 0, 1000},
		{"fok_attempt_99", 99, riskCloseModeFOKSell, 0, 60000},
		// Empty tier (legacy mode): fall through to closeRetryMs too.
		{"empty_tier_legacy", 1, "", 0, 1000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := closeRetryMsForLadder(tc.attempts, tc.tier)
			if got < tc.minMs || got > tc.maxMs {
				t.Fatalf("attempts=%d tier=%q got %dms want [%d,%d]", tc.attempts, tc.tier, got, tc.minMs, tc.maxMs)
			}
		})
	}
}
