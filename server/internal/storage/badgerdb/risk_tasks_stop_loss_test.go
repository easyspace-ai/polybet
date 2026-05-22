package badgerdb

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestDeleteRiskTasksStopLossPreservesOtherTasks(t *testing.T) {
	db, err := Open(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	now := time.Now().UTC()

	stopLoss := &RiskTask{
		ID: "sl-1", Type: "close_position", Status: "succeeded", Attempts: 1,
		Reason: sql.NullString{String: "stop_loss", Valid: true},
		PositionID: sql.NullString{String: "pos-1", Valid: true},
		NextRunAt: now, CreatedAt: now, UpdatedAt: now,
	}
	manual := &RiskTask{
		ID: "man-1", Type: "close_position", Status: "succeeded", Attempts: 1,
		Reason: sql.NullString{String: "manual", Valid: true},
		PositionID: sql.NullString{String: "pos-2", Valid: true},
		NextRunAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.InsertRiskTask(ctx, stopLoss); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertRiskTask(ctx, manual); err != nil {
		t.Fatal(err)
	}

	n, err := db.DeleteRiskTasksStopLoss(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("deleted=%d want 1", n)
	}

	sl, err := db.ListRiskTasksByReason(ctx, "close_position", "stop_loss", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sl) != 0 {
		t.Fatalf("stop_loss tasks left=%d", len(sl))
	}

	recent, err := db.ListRiskTasksRecent(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 || recent[0].ID != "man-1" {
		t.Fatalf("recent=%+v want manual only", recent)
	}
}
