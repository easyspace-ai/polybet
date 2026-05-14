package store

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	// Create tables needed for count queries
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS markets (
			id TEXT PRIMARY KEY,
			slug TEXT,
			title TEXT,
			status TEXT,
			event_start_date TEXT,
			sport TEXT,
			league TEXT,
			home_team TEXT,
			away_team TEXT,
			created_at TEXT DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS outcomes (
			id TEXT PRIMARY KEY,
			market_id TEXT REFERENCES markets(id),
			title TEXT,
			side TEXT,
			odds REAL,
			external_id TEXT
		);
	`)
	if err != nil {
		t.Fatalf("failed to create test tables: %v", err)
	}
	return db
}

func TestCountActiveMarkets(t *testing.T) {
	db := openTestDB(t)
	s := New(db)

	n, err := s.CountActiveMarkets(context.Background())
	if err != nil {
		t.Fatalf("CountActiveMarkets: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 active markets, got %d", n)
	}

	_, err = db.Exec(`INSERT INTO markets (id, slug, title, status) VALUES ('m1', 'test', 'Test', 'active')`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	n, err = s.CountActiveMarkets(context.Background())
	if err != nil {
		t.Fatalf("CountActiveMarkets: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 active market, got %d", n)
	}
}

func TestCountActiveOutcomes(t *testing.T) {
	db := openTestDB(t)
	s := New(db)

	n, err := s.CountActiveOutcomes(context.Background())
	if err != nil {
		t.Fatalf("CountActiveOutcomes: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 active outcomes, got %d", n)
	}

	_, err = db.Exec(`INSERT INTO markets (id, slug, title, status) VALUES ('m1', 'test', 'Test', 'active')`)
	if err != nil {
		t.Fatalf("insert market: %v", err)
	}
	_, err = db.Exec(`INSERT INTO outcomes (id, market_id, title, side) VALUES ('o1', 'm1', 'Yes', 'buy')`)
	if err != nil {
		t.Fatalf("insert outcome: %v", err)
	}

	n, err = s.CountActiveOutcomes(context.Background())
	if err != nil {
		t.Fatalf("CountActiveOutcomes: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 active outcome, got %d", n)
	}
}

func TestStore_New(t *testing.T) {
	db := openTestDB(t)
	s := New(db)
	if s == nil {
		t.Fatal("expected non-nil store")
	}
}
