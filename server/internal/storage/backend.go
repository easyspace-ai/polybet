package storage

import (
	"github.com/easyspace-ai/polybet/internal/storage/badgerdb"
	"github.com/easyspace-ai/polybet/internal/store"
)

// Backend is the application storage handle: a Badger-backed store plus the
// same DB pointer duplicated for callers that still expect .Badger.
type Backend struct {
	*store.Store
	Badger *badgerdb.DB
}

// NewBackend wraps a Badger-opened store.
func NewBackend(kv *badgerdb.DB) *Backend {
	if kv == nil {
		return nil
	}
	return &Backend{Store: store.New(kv), Badger: kv}
}

// Legacy returns the embedded store (historical name from the SQLite era).
func (b *Backend) Legacy() *store.Store {
	if b == nil {
		return nil
	}
	return b.Store
}
