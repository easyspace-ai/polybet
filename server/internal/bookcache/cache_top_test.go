package bookcache

import "testing"

func TestApplyTopOfBookUpdatesTopOfBook(t *testing.T) {
	c := New(5)
	tid := "0x0000000000000000000000000000000000000000000000000000000000000001"
	c.ApplyTopOfBook(tid, 0.54, 0.55, 1000)
	bb, ba, ok := c.TopOfBook(tid)
	if !ok {
		t.Fatal("expected top of book")
	}
	if bb != 0.54 || ba != 0.55 {
		t.Fatalf("top of book = %v/%v, want 0.54/0.55", bb, ba)
	}
	// ApplyTopOfBook does NOT inject size=0 placeholder levels into the
	// ladder; depth must come from a real book event (ReplaceBook /
	// ApplyPriceChange with non-zero size). Routers and UI consumers must
	// not see phantom $0 rows.
	bids, asks := c.GetBidsAsks(tid, 5)
	if len(bids) != 0 || len(asks) != 0 {
		t.Fatalf("expected no level rows from top-of-book-only update, got bids=%d asks=%d", len(bids), len(asks))
	}
}

func TestApplyTopOfBookRespectsTimestamp(t *testing.T) {
	c := New(5)
	tid := "0x0000000000000000000000000000000000000000000000000000000000000002"
	c.ApplyTopOfBook(tid, 0.40, 0.41, 2000)
	c.ApplyTopOfBook(tid, 0.10, 0.11, 1000)
	bb, ba, ok := c.TopOfBook(tid)
	if !ok {
		t.Fatal("expected top of book")
	}
	if bb != 0.40 || ba != 0.41 {
		t.Fatalf("stale tick overwrote book: got %v/%v", bb, ba)
	}
}
