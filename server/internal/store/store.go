package store

import (
	"context"
	"database/sql"
)

// Store wraps database access.
type Store struct {
	db *sql.DB
}

func New(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) CountActiveMarkets(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM markets WHERE status='active'`).Scan(&n)
	return n, err
}

func (s *Store) CountActiveOutcomes(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM outcomes o JOIN markets m ON o.market_id=m.id WHERE m.status='active'`).Scan(&n)
	return n, err
}
