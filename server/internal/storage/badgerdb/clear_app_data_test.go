package badgerdb

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestClearAllAppDataPreservesConfigAndAccounts(t *testing.T) {
	db, err := Open(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	now := time.Now().UTC()

	if err := db.WriteBotConfigMap(ctx, map[string]string{"pollingInterval": "60"}); err != nil {
		t.Fatal(err)
	}
	if err := db.WriteAccountsSnapshot(ctx, []PolymarketAccount{
		{ID: "acct1", Name: "main", IsActive: true, CreatedAt: now, UpdatedAt: now},
	}, "acct1"); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateRiskPosition(ctx, &RiskPosition{
		ID: "pos1", Platform: "polymarket", AccountID: "acct1", TokenID: "tok1",
		Title: "T", SideLabel: "Yes", AvgEntryCents: 50, SizeShares: 10, CostUSD: 5,
		HighWaterCents: 50, StopLossPct: 20, Source: "test", Status: "open", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertRiskTask(ctx, &RiskTask{
		ID: "task1", Type: "close_position", Status: "succeeded", Attempts: 1,
		Reason: sql.NullString{String: "stop_loss", Valid: true},
		PositionID: sql.NullString{String: "pos1", Valid: true},
		NextRunAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	n, err := db.ClearAllAppData(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n < 2 {
		t.Fatalf("expected several keys deleted, got %d", n)
	}

	got, err := db.ReadBotConfigMap(ctx)
	if err != nil || got["pollingInterval"] != "60" {
		t.Fatalf("config should remain: map=%#v err=%v", got, err)
	}
	aid, err := db.ReadActiveAccountID(ctx)
	if err != nil || aid != "acct1" {
		t.Fatalf("active account should remain: id=%q err=%v", aid, err)
	}
	open, err := db.ListOpenOrClosingRiskPositions(ctx, "acct1")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 0 {
		t.Fatalf("positions should be cleared, got %d", len(open))
	}
	tasks, err := db.ListRiskTasksByReason(ctx, "close_position", "stop_loss", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("stop-loss tasks should be cleared, got %d", len(tasks))
	}
}
