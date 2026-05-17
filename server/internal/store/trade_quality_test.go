package store

import (
	"math"
	"testing"
)

func TestSlippageBpsBuy(t *testing.T) {
	t.Parallel()
	// Bought at expected = no slippage.
	if g := SlippageBpsBuy(0.50, 0.50); math.Abs(g) > 1e-9 {
		t.Fatalf("got %v want 0", g)
	}
	// Paid 1¢ more on a 50¢ token = 200 bps worse.
	if g := SlippageBpsBuy(0.50, 0.51); math.Abs(g-200) > 1e-9 {
		t.Fatalf("got %v want 200", g)
	}
	// Got better fill (negative bps).
	if g := SlippageBpsBuy(0.50, 0.49); math.Abs(g+200) > 1e-9 {
		t.Fatalf("got %v want -200", g)
	}
	// Invalid input → 0.
	if SlippageBpsBuy(0, 0.50) != 0 || SlippageBpsBuy(0.50, 0) != 0 || SlippageBpsBuy(-0.1, 0.5) != 0 {
		t.Fatal("invalid inputs should return 0")
	}
}

func TestSlippageBpsSell(t *testing.T) {
	t.Parallel()
	// Sell semantics: positive bps == worse for us (received less).
	// Got exactly expected → 0.
	if g := SlippageBpsSell(0.50, 0.50); math.Abs(g) > 1e-9 {
		t.Fatalf("got %v want 0", g)
	}
	// Got 1¢ less on a 50¢ token = 200 bps worse.
	if g := SlippageBpsSell(0.50, 0.49); math.Abs(g-200) > 1e-9 {
		t.Fatalf("got %v want 200", g)
	}
	// Got more than expected = negative bps (better fill).
	if g := SlippageBpsSell(0.50, 0.51); math.Abs(g+200) > 1e-9 {
		t.Fatalf("got %v want -200", g)
	}
	// Invalid → 0.
	if SlippageBpsSell(0, 0.5) != 0 || SlippageBpsSell(0.5, 0) != 0 || SlippageBpsSell(-0.1, 0.5) != 0 {
		t.Fatal("invalid inputs should return 0")
	}
}
