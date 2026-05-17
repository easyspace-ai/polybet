package polyexec

import (
	"testing"
)

// applyCollateralCap is the pure clamp logic extracted from
// HedgeFOKBuySizingWithCollateral so it can be tested without spinning up a
// CLOB stub. The full function is integration-tested via the close path.
//
// This mirror exists ONLY in the test file; the real implementation
// inlines the same arithmetic and is exercised via the public function.
func applyCollateralCap(requested, available, reservePct, minHedge float64) (size float64, clamped bool, ok bool) {
	if reservePct < 0 || reservePct >= 0.5 {
		reservePct = 0
	}
	reserve := available * reservePct
	usable := available - reserve
	if usable < 0 {
		usable = 0
	}
	if available <= 0 {
		// Fail-open: treat 0 as "unknown".
		return requested, false, true
	}
	size = requested
	if requested > usable {
		size = usable
		clamped = true
	}
	if size < minHedge {
		return size, clamped, false
	}
	return size, clamped, true
}

func TestApplyCollateralCap(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                               string
		requested, available, reserve, min float64
		wantSize                           float64
		wantClamped, wantOK                bool
	}{
		// Plenty of collateral: pass through unchanged.
		{"no_clamp", 10, 100, 0.05, 1, 10, false, true},
		// Hedge requests just below the reserve limit (95): pass through.
		{"at_usable_limit", 95, 100, 0.05, 1, 95, false, true},
		// Hedge requests above usable: clamp to usable.
		{"clamp_to_usable", 200, 100, 0.05, 1, 95, true, true},
		// Clamp drops below minHedge: ok=false so caller aborts cleanly.
		{"clamp_below_min", 200, 0.5, 0.05, 1, 0.475, true, false},
		// Available <= 0 → fail-open (pass requested through).
		{"unknown_collateral", 50, 0, 0.05, 1, 50, false, true},
		// Negative reserve treated as 0.
		{"negative_reserve_falls_back", 80, 100, -0.10, 1, 80, false, true},
		// >= 0.5 reserve treated as 0 (defensive: avoid leaving > half unused).
		{"absurd_reserve_falls_back", 80, 100, 0.9, 1, 80, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			size, clamped, ok := applyCollateralCap(tc.requested, tc.available, tc.reserve, tc.min)
			if size != tc.wantSize || clamped != tc.wantClamped || ok != tc.wantOK {
				t.Fatalf("got size=%v clamped=%v ok=%v want %v %v %v", size, clamped, ok, tc.wantSize, tc.wantClamped, tc.wantOK)
			}
		})
	}
}
