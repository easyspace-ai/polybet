package badgerdb

import (
	"context"
	"testing"
	"time"
)

func TestCountOpenRiskPositionsMinShares_openIndex(t *testing.T) {
	db, err := Open(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	now := time.Now().UTC()

	open := func(id, accountID, token string, shares float64, status string) {
		t.Helper()
		if err := db.CreateRiskPosition(ctx, &RiskPosition{
			ID: id, Platform: "polymarket", AccountID: accountID, TokenID: token,
			Title: "T", SideLabel: "Yes", AvgEntryCents: 50, SizeShares: shares, CostUSD: shares * 0.5,
			HighWaterCents: 50, StopLossPct: 20, Source: "test", Status: status, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}

	open("open-big", "acct-1", "0xaaa", 10, "open")
	open("open-small", "acct-1", "0xbbb", 0.5, "open")
	open("closed-one", "acct-1", "0xccc", 8, "open")
	if err := db.CloseRiskPosition(ctx, "closed-one"); err != nil {
		t.Fatal(err)
	}
	open("acct2-open", "acct-2", "0xeee", 20, "open")

	n, err := db.CountOpenRiskPositionsMinShares(ctx, 1, "acct-1")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("count=%d want 1 (only open-big meets minShares for acct-1)", n)
	}
}
