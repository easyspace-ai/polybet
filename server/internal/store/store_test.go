package store

import (
	"context"
	"testing"

	"github.com/easyspace-ai/polybet/internal/storage/badgerdb"
)

func openTestBadger(t *testing.T) *badgerdb.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := badgerdb.Open(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestCountActiveMarkets(t *testing.T) {
	s := New(openTestBadger(t))
	n, err := s.CountActiveMarkets(context.Background())
	if err != nil {
		t.Fatalf("CountActiveMarkets: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 active markets on empty db, got %d", n)
	}
}

func TestCountActiveOutcomes(t *testing.T) {
	s := New(openTestBadger(t))
	n, err := s.CountActiveOutcomes(context.Background())
	if err != nil {
		t.Fatalf("CountActiveOutcomes: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 active outcomes on empty db, got %d", n)
	}
}

func TestStore_New(t *testing.T) {
	s := New(openTestBadger(t))
	if s == nil || s.KV == nil {
		t.Fatal("expected non-nil store")
	}
}
