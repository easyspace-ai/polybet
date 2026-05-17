package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// openRealizedDB creates an in-memory schema mirroring the post-migration-011
// shape so we can test AccountRealizedPnLSince without running the full
// migration chain. Foreign keys are off so tests can insert minimal rows.
func openRealizedDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(0)")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`
		CREATE TABLE risk_positions (
			id TEXT PRIMARY KEY,
			account_id TEXT NOT NULL DEFAULT '',
			token_id TEXT NOT NULL,
			cost_usd REAL NOT NULL DEFAULT 0,
			size_shares REAL NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'open',
			realized_pnl_usd REAL,
			closed_at TEXT
		);
	`)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	return db
}

func insertRealizedRow(t *testing.T, db *sql.DB, id, accountID, status string, pnl *float64, closedAt time.Time) {
	t.Helper()
	var pnlAny any
	if pnl != nil {
		pnlAny = *pnl
	}
	var closedAny any
	if !closedAt.IsZero() {
		closedAny = closedAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := db.Exec(`INSERT INTO risk_positions(id, account_id, token_id, cost_usd, size_shares, status, realized_pnl_usd, closed_at)
	                  VALUES(?, ?, '0xtok', 10.0, 0, ?, ?, ?)`,
		id, accountID, status, pnlAny, closedAny)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
}

func TestAccountRealizedPnLSince(t *testing.T) {
	db := openRealizedDB(t)
	s := New(db)
	ctx := context.Background()
	now := time.Now().UTC()
	pnl := func(v float64) *float64 { return &v }

	// Setup: a mix of rows to verify the WHERE filters.
	insertRealizedRow(t, db, "p1", "acct1", "closed", pnl(-5.0), now.Add(-1*time.Hour))   // in window, count
	insertRealizedRow(t, db, "p2", "acct1", "closed", pnl(-2.5), now.Add(-30*time.Minute)) // in window, count
	insertRealizedRow(t, db, "p3", "acct1", "closed", pnl(7.0), now.Add(-2*time.Hour))    // in window winner, count
	insertRealizedRow(t, db, "p4", "acct1", "closed", pnl(-100.0), now.Add(-26*time.Hour)) // outside 24h window, exclude
	insertRealizedRow(t, db, "p5", "acct1", "closed", nil, now.Add(-10*time.Minute))      // NULL pnl (legacy/dust), exclude
	insertRealizedRow(t, db, "p6", "acct1", "open", pnl(-50.0), time.Time{})              // still open, exclude
	insertRealizedRow(t, db, "p7", "acct2", "closed", pnl(-999.0), now.Add(-1*time.Hour)) // wrong account, exclude

	since := now.Add(-24 * time.Hour)
	got, err := s.AccountRealizedPnLSince(ctx, "acct1", since)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := -5.0 + -2.5 + 7.0 // = -0.5
	if got != want {
		t.Fatalf("got %v want %v", got, want)
	}

	// Empty inputs short-circuit to 0 without query.
	if got, err := s.AccountRealizedPnLSince(ctx, "", since); err != nil || got != 0 {
		t.Fatalf("empty acct: got=%v err=%v", got, err)
	}
	if got, err := s.AccountRealizedPnLSince(ctx, "acct1", time.Time{}); err != nil || got != 0 {
		t.Fatalf("zero since: got=%v err=%v", got, err)
	}
}
