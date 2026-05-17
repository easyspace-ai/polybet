package badgerdb

import (
	"errors"
	"fmt"

	badger "github.com/dgraph-io/badger/v4"
)

// DB wraps a BadgerDB v4 handle (ADR Phase 1).
type DB struct {
	inner *badger.DB
	dir   string
}

// Open opens or creates a database at dir with durability tuned for operator safety.
func Open(dir string, syncWrites bool) (*DB, error) {
	if dir == "" {
		return nil, errors.New("badgerdb: empty dir")
	}
	opts := badger.DefaultOptions(dir).
		WithSyncWrites(syncWrites).
		WithLoggingLevel(badger.ERROR)
	inner, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("badgerdb open %q: %w", dir, err)
	}
	return &DB{inner: inner, dir: dir}, nil
}

// Dir returns the on-disk directory.
func (d *DB) Dir() string {
	if d == nil {
		return ""
	}
	return d.dir
}

// Close releases the database handle.
func (d *DB) Close() error {
	if d == nil || d.inner == nil {
		return nil
	}
	return d.inner.Close()
}

// Inner exposes the raw handle for advanced callers (e.g. subscriptions).
func (d *DB) Inner() *badger.DB {
	if d == nil {
		return nil
	}
	return d.inner
}

// View runs a read-only transaction.
func (d *DB) View(fn func(txn *badger.Txn) error) error {
	if d == nil || d.inner == nil {
		return errors.New("badgerdb: nil db")
	}
	return d.inner.View(fn)
}

// Update runs a read-write transaction.
func (d *DB) Update(fn func(txn *badger.Txn) error) error {
	if d == nil || d.inner == nil {
		return errors.New("badgerdb: nil db")
	}
	return d.inner.Update(fn)
}
