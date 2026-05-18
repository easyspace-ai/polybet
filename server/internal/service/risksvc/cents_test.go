package risksvc

import "testing"

func TestStopTriggerReferenceCentsNormalized(t *testing.T) {
	bid := CentsFromPrice01(0.28999999999999995)
	ask := CentsFromPrice01(0.30)
	trigger := FloorCents1(stopTriggerReferenceCents(bid, ask))
	trail := FloorCents1(29.0)
	if trigger != 29.0 {
		t.Fatalf("trigger = %v, want 29.0", trigger)
	}
	// At exact trail: should trigger (29 <= 29).
	if trigger > trail {
		t.Fatalf("trigger %v should be at or below trail %v for equal-price case", trigger, trail)
	}
	// Raw float noise would read below trail without normalization.
	rawBid := 0.28999999999999995 * 100
	if rawBid >= trail && FloorCents1(rawBid) > trail {
		t.Fatalf("unexpected: raw %v trail %v", rawBid, trail)
	}
}

func TestFloorCents1(t *testing.T) {
	tests := []struct {
		in, want float64
	}{
		{55.989999999999995, 55.9},
		{55.98, 55.9},
		{55.91, 55.9},
		{55.9, 55.9},
		{0, 0},
		{-1, -1},
	}
	for _, tc := range tests {
		if g := FloorCents1(tc.in); g != tc.want {
			t.Fatalf("FloorCents1(%v) = %v, want %v", tc.in, g, tc.want)
		}
	}
}

func TestTrailingStopCentsFromHW(t *testing.T) {
	// 20% stop from floored HW 55.9 → 44.72 → floor 44.7
	if g := TrailingStopCentsFromHW(55.989999999999995, 20); g != 44.7 {
		t.Fatalf("got %v want 44.7", g)
	}
	if g := TrailingStopCentsFromHW(55.9, 20); g != 44.7 {
		t.Fatalf("got %v want 44.7", g)
	}
}

func TestTrailingStopCentsFromHWWithAbs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		hw, pct, maxDrop float64
		want             float64
	}{
		// maxDrop disabled (<=0): equals legacy percent-only formula.
		{"abs_disabled_equiv", 55.9, 20, 0, 44.7},
		{"abs_negative_equiv", 55.9, 20, -3, 44.7},
		// On a 95¢ favourite, 10% trail = 85.5¢; maxDrop=3 tightens to 92¢.
		{"abs_caps_drop_on_high_price", 95, 10, 3, 92},
		// 90¢ HW, 50% pct trail = 45¢, maxDrop=3 → 87¢ wins.
		{"max_wins_when_pct_loose", 90, 50, 3, 87},
		// 5¢ HW, 10% trail = 4.5¢; maxDrop=2 → 5-2=3, but pct (4.5) > abs (3) → pct wins.
		{"pct_wins_when_already_tight", 5, 10, 2, 4.5},
		// 5¢ HW, absurdly large maxDrop=100 → -95 clamped at 0; pct wins (4.5).
		{"pct_wins_against_giant_abs", 5, 10, 100, 4.5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if g := TrailingStopCentsFromHWWithAbs(tc.hw, tc.pct, tc.maxDrop); g != tc.want {
				t.Fatalf("hw=%v pct=%v maxDrop=%v got %v want %v", tc.hw, tc.pct, tc.maxDrop, g, tc.want)
			}
		})
	}
}
