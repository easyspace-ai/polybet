package store

import (
	"context"

	"github.com/easyspace-ai/polybet/internal/storage/badgerdb"
)

// Store is the persistence facade over BadgerDB.
type Store struct {
	KV *badgerdb.DB
}

func New(kv *badgerdb.DB) *Store {
	return &Store{KV: kv}
}

func (s *Store) kv() *badgerdb.DB {
	return s.KV
}

func (s *Store) CountActiveMarkets(ctx context.Context) (int, error) {
	return s.kv().CountActiveMarkets(ctx)
}

func (s *Store) CountActiveOutcomes(ctx context.Context) (int, error) {
	return s.kv().CountActiveOutcomes(ctx)
}
