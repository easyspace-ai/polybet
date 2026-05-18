package badgerdb

import (
	"context"
	"testing"
	"time"

	badger "github.com/dgraph-io/badger/v4"
)

func TestRiskPositionSeqCreateAndMigrate(t *testing.T) {
	db, err := Open(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()

	now := time.Now().UTC()
	legacyID := "legacy-pos-1"
	if err := db.Update(func(txn *badger.Txn) error {
		doc := &RiskPosDoc{
			ID: legacyID, Platform: "polymarket", AccountID: "acct-1", TokenID: "0xabc",
			Title: "Legacy", SideLabel: "Yes", AvgEntryCents: 50, SizeShares: 10, CostUSD: 5,
			HighWaterCents: 50, StopLossPct: 20, Source: "test", Status: "open",
			CreatedAt: now.Add(-time.Hour).Format(time.RFC3339Nano),
			UpdatedAt: now.Add(-time.Hour).Format(time.RFC3339Nano),
		}
		return db.writeRiskPos(txn, doc)
	}); err != nil {
		t.Fatal(err)
	}

	if err := db.MigrateRiskPositionSeq(ctx); err != nil {
		t.Fatal(err)
	}
	legacy, err := db.GetRiskPosition(ctx, legacyID)
	if err != nil || legacy == nil {
		t.Fatalf("legacy position: %v %v", legacy, err)
	}
	if legacy.PositionSeq != 1 {
		t.Fatalf("legacy seq=%d want 1", legacy.PositionSeq)
	}

	if err := db.CreateRiskPosition(ctx, &RiskPosition{
		Platform: "polymarket", AccountID: "acct-1", TokenID: "0xdef",
		Title: "New", SideLabel: "No", AvgEntryCents: 40, SizeShares: 3, CostUSD: 1.2,
		HighWaterCents: 40, StopLossPct: 20, Source: "test", Status: "open",
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := db.ListOpenRiskPositionsByToken(ctx, "0xdef", "acct-1")
	if err != nil || len(rows) != 1 {
		t.Fatalf("new rows=%v err=%v", rows, err)
	}
	if rows[0].PositionSeq != 2 {
		t.Fatalf("new seq=%d want 2", rows[0].PositionSeq)
	}
}
