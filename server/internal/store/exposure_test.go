package store

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func openExposureDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(0)")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	// Mirror the post-migration shape of the columns the helpers read.
	// Foreign keys disabled to keep the test schema minimal.
	_, err = db.Exec(`
		CREATE TABLE risk_positions (
			id TEXT PRIMARY KEY,
			account_id TEXT NOT NULL DEFAULT '',
			token_id TEXT NOT NULL,
			poly_event_slug TEXT,
			poly_market_slug TEXT,
			cost_usd REAL NOT NULL DEFAULT 0,
			size_shares REAL NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'open'
		);
	`)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	return db
}

func insertRiskPos(t *testing.T, db *sql.DB, id, accountID, tokenID, slug string, cost float64, status string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO risk_positions(id, account_id, token_id, poly_event_slug, cost_usd, size_shares, status)
	                  VALUES (?, ?, ?, ?, ?, 100, ?)`, id, accountID, tokenID, slug, cost, status)
	if err != nil {
		t.Fatalf("insert risk_pos: %v", err)
	}
}

func TestAccountOpenExposureUSD(t *testing.T) {
	db := openExposureDB(t)
	s := New(db)
	ctx := context.Background()

	// Empty account → 0.
	if got, err := s.AccountOpenExposureUSD(ctx, "acct1"); err != nil || got != 0 {
		t.Fatalf("empty: got=%v err=%v", got, err)
	}
	insertRiskPos(t, db, "p1", "acct1", "0xtok1", "game-a", 50.5, "open")
	insertRiskPos(t, db, "p2", "acct1", "0xtok2", "game-b", 30.0, "open")
	// closing positions should still count as exposure (capital is committed).
	// Per spec we only count status='open'; closing is fine to exclude
	// because the close path is in flight and any successful fill drops
	// the row. Confirmed by AccountOpenExposureUSD's WHERE clause.
	insertRiskPos(t, db, "p3", "acct1", "0xtok3", "game-c", 99.0, "closing")
	insertRiskPos(t, db, "p4", "acct1", "0xtok4", "game-d", 99.0, "closed")
	insertRiskPos(t, db, "p5", "acct2", "0xtok5", "game-e", 1000.0, "open") // different account

	got, err := s.AccountOpenExposureUSD(ctx, "acct1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != 80.5 {
		t.Fatalf("expected 80.5 (50.5 + 30, excluding closing/closed/other-account), got %v", got)
	}

	// Empty accountID returns 0 without query.
	if got, err := s.AccountOpenExposureUSD(ctx, ""); err != nil || got != 0 {
		t.Fatalf("empty acct: got=%v err=%v", got, err)
	}
}

func TestMarketOpenExposureUSD(t *testing.T) {
	db := openExposureDB(t)
	s := New(db)
	ctx := context.Background()

	insertRiskPos(t, db, "p1", "acct1", "0xtok1", "lakers-vs-celtics", 40.0, "open")
	insertRiskPos(t, db, "p2", "acct1", "0xtok2", "lakers-vs-celtics", 25.0, "open") // opposite outcome same game
	insertRiskPos(t, db, "p3", "acct1", "0xtok3", "lakers-vs-celtics", 10.0, "closed")
	insertRiskPos(t, db, "p4", "acct1", "0xtok4", "warriors-vs-bucks", 100.0, "open")
	insertRiskPos(t, db, "p5", "acct2", "0xtok5", "lakers-vs-celtics", 999.0, "open") // different account

	got, err := s.MarketOpenExposureUSD(ctx, "acct1", "lakers-vs-celtics")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != 65.0 {
		t.Fatalf("expected 65 (40+25 same game, both YES and NO), got %v", got)
	}

	// "event/" prefix should be stripped before query (matches sync's
	// stored format).
	got, err = s.MarketOpenExposureUSD(ctx, "acct1", "event/lakers-vs-celtics/")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != 65.0 {
		t.Fatalf("prefix-stripped slug: got %v want 65", got)
	}

	// Unknown event → 0 (no error).
	got, err = s.MarketOpenExposureUSD(ctx, "acct1", "no-such-game")
	if err != nil || got != 0 {
		t.Fatalf("unknown event: got=%v err=%v", got, err)
	}

	// Empty inputs return 0 without query.
	if got, err := s.MarketOpenExposureUSD(ctx, "", "anything"); err != nil || got != 0 {
		t.Fatalf("empty acct: got=%v err=%v", got, err)
	}
	if got, err := s.MarketOpenExposureUSD(ctx, "acct1", ""); err != nil || got != 0 {
		t.Fatalf("empty slug: got=%v err=%v", got, err)
	}
}
