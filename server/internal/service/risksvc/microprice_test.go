package risksvc

import (
	"math"
	"testing"
)

func TestMicroPriceCents_BothSides(t *testing.T) {
	t.Parallel()
	// bid=70¢ with 10 USD depth, ask=72¢ with 30 USD depth. Heavier ask
	// depth pulls micro-price toward the bid (70.5¢).
	got := microPriceCents(70, 72, 10, 30)
	// Expected: (70 * 30 + 72 * 10) / (10 + 30) = (2100 + 720) / 40 = 70.5
	if math.Abs(got-70.5) > 1e-9 {
		t.Fatalf("got %v want 70.5", got)
	}
}

func TestMicroPriceCents_SymmetricDepth(t *testing.T) {
	t.Parallel()
	// Equal depth → arithmetic mid.
	got := microPriceCents(40, 60, 100, 100)
	if math.Abs(got-50) > 1e-9 {
		t.Fatalf("got %v want 50", got)
	}
}

func TestMicroPriceCents_MissingSide(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                       string
		bid, ask, bidSize, askSize float64
		want                       float64
	}{
		{"no_bid", 0, 60, 0, 100, 60},
		{"no_ask", 40, 0, 100, 0, 40},
		{"both_missing", 0, 0, 0, 0, 0},
		{"bid_no_size", 40, 60, 0, 100, 60},
		{"ask_no_size", 40, 60, 100, 0, 40},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := microPriceCents(tc.bid, tc.ask, tc.bidSize, tc.askSize); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestMicroPriceCents_SkewsTowardThinSide(t *testing.T) {
	t.Parallel()
	// Heavy bid wall → micro-price should be near the ask (next likely trade
	// price for a market sell against the wall). 100 USD bid, 1 USD ask.
	got := microPriceCents(40, 60, 100, 1)
	// Expected: (40 * 1 + 60 * 100) / 101 ≈ 59.80
	if got <= 59.5 || got >= 60.0 {
		t.Fatalf("got %v want close to 60 (heavy bid pulls micro toward ask)", got)
	}
}
