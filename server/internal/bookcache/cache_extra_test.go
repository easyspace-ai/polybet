package bookcache

import (
	"testing"
	"time"
)

func TestReplaceBookCachesTop(t *testing.T) {
	c := New(5)
	tid := "0x000000000000000000000000000000000000000000000000000000000000abc1"
	c.ReplaceBook(tid, []struct{ Price, Size string }{
		{"0.40", "100"}, {"0.39", "50"},
	}, []struct{ Price, Size string }{
		{"0.45", "20"}, {"0.46", "30"},
	}, 1000)
	bb, ba, ok := c.TopOfBook(tid)
	if !ok || bb != 0.40 || ba != 0.45 {
		t.Fatalf("ReplaceBook top: bb=%v ba=%v ok=%v", bb, ba, ok)
	}
	// ApplyPriceChange that improves the bid should be reflected immediately.
	c.ApplyPriceChange(tid, "BUY", "0.41", "60", 2000)
	bb, _, _ = c.TopOfBook(tid)
	if bb != 0.41 {
		t.Fatalf("incremental bid update: got %v want 0.41", bb)
	}
	// Deleting the new top should fall back to the next-best bid (0.40).
	c.ApplyPriceChange(tid, "BUY", "0.41", "0", 3000)
	bb, _, _ = c.TopOfBook(tid)
	if bb != 0.40 {
		t.Fatalf("post-delete bid recompute: got %v want 0.40", bb)
	}
	// Same dance on the ask side.
	c.ApplyPriceChange(tid, "SELL", "0.44", "10", 4000)
	_, ba, _ = c.TopOfBook(tid)
	if ba != 0.44 {
		t.Fatalf("incremental ask update: got %v want 0.44", ba)
	}
	c.ApplyPriceChange(tid, "SELL", "0.44", "0", 5000)
	_, ba, _ = c.TopOfBook(tid)
	if ba != 0.45 {
		t.Fatalf("post-delete ask recompute: got %v want 0.45", ba)
	}
}

func TestPruneIdle(t *testing.T) {
	c := New(5)
	tid1 := "0x000000000000000000000000000000000000000000000000000000000000aa01"
	tid2 := "0x000000000000000000000000000000000000000000000000000000000000aa02"
	now := time.Now().UnixMilli()
	c.ReplaceBook(tid1, nil, []struct{ Price, Size string }{{"0.5", "10"}}, now-60_000)
	c.ReplaceBook(tid2, nil, []struct{ Price, Size string }{{"0.6", "10"}}, now)
	c.SetFeeRate(tid1, 0.02)
	c.SetFeeRate(tid2, 0.02)

	if got := c.PruneIdle(0); got != 0 {
		t.Fatalf("disabled prune should be no-op, removed %d", got)
	}
	if got := c.PruneIdle(30 * time.Second); got != 1 {
		t.Fatalf("expected 1 stale token removed, got %d", got)
	}
	if c.Size() != 1 {
		t.Fatalf("expected 1 surviving token, got %d", c.Size())
	}
	// Companion fee-rate map must also drop the evicted token to avoid leak.
	if c.FeeRate(tid1) != 0 {
		t.Fatalf("fee rate for evicted token should be 0, got %v", c.FeeRate(tid1))
	}
	if c.FeeRate(tid2) == 0 {
		t.Fatalf("fee rate for live token must survive prune")
	}
}

func TestGetLevelsLockedDropsZeroSize(t *testing.T) {
	c := New(5)
	tid := "0x000000000000000000000000000000000000000000000000000000000000bb01"
	c.ReplaceBook(tid, nil, []struct{ Price, Size string }{
		{"0.55", "12"}, {"0.56", "0"}, {"0.57", "8"},
	}, 1000)
	lv := c.GetLevels(tid)
	if len(lv) != 2 {
		t.Fatalf("expected 2 levels (zero-size dropped), got %d", len(lv))
	}
	for _, l := range lv {
		if l.Size <= 0 {
			t.Fatalf("zero-size level leaked: %+v", l)
		}
	}
}
