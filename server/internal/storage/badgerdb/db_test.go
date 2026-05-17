package badgerdb

import (
	"context"
	"sync"
	"testing"
	"time"

	badger "github.com/dgraph-io/badger/v4"
)

func TestOpenClose(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentWrites(t *testing.T) {
	db, err := Open(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	const workers = 32
	const keysPerWorker = 50
	errCh := make(chan error, workers*keysPerWorker)
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		w := w
		go func() {
			defer wg.Done()
			for i := 0; i < keysPerWorker; i++ {
				k := []byte{byte('k'), byte(w), byte(i >> 8), byte(i)}
				v := []byte{byte(w), byte(i)}
				if err := db.Update(func(txn *badger.Txn) error {
					return txn.Set(k, v)
				}); err != nil {
					errCh <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}

	var n int
	_ = db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			n++
		}
		return nil
	})
	if n != workers*keysPerWorker {
		t.Fatalf("expected %d keys, got %d", workers*keysPerWorker, n)
	}
}

func TestBotConfigRoundTrip(t *testing.T) {
	db, err := Open(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	m := map[string]string{"a": "1", "b": "two"}
	if err := db.WriteBotConfigMap(ctx, m); err != nil {
		t.Fatal(err)
	}
	got, err := db.ReadBotConfigMap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got["a"] != "1" || got["b"] != "two" {
		t.Fatalf("unexpected map %#v", got)
	}
}

func TestAccountSnapshot(t *testing.T) {
	db, err := Open(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	accs := []struct {
		id, name string
		active   bool
	}{
		{"u1", "A", true},
		{"u2", "B", false},
	}
	var list []PolymarketAccount
	for _, x := range accs {
		list = append(list, PolymarketAccount{
			ID: x.id, Name: x.name, IsActive: x.active, CreatedAt: now, UpdatedAt: now,
		})
	}
	if err := db.WriteAccountsSnapshot(ctx, list, "u1"); err != nil {
		t.Fatal(err)
	}
	read, err := db.ReadAccounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(read) != 2 {
		t.Fatalf("len=%d", len(read))
	}
	aid, err := db.ReadActiveAccountID(ctx)
	if err != nil || aid != "u1" {
		t.Fatalf("active id %q err=%v", aid, err)
	}
}
