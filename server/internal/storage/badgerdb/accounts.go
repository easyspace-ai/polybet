package badgerdb

import (
	"context"
	"errors"
	"strings"

	badger "github.com/dgraph-io/badger/v4"

	"github.com/easyspace-ai/polybet/internal/polymarketacct"
)

// PolymarketAccount is the shared credential document (JSON file; legacy Badger keys for migration).
type PolymarketAccount = polymarketacct.Account

// AccountPrefix is the prefix scan for legacy account documents (excludes account/active).
const AccountPrefix = "account/"

// WriteAccountsSnapshot writes all accounts and the active id pointer (used for one-time migration purge).
func (d *DB) WriteAccountsSnapshot(ctx context.Context, accounts []PolymarketAccount, activeID string) error {
	if d == nil {
		return errors.New("badgerdb: nil db")
	}
	return d.Update(func(txn *badger.Txn) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var keys [][]byte
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		prefix := []byte(AccountPrefix)
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			keys = append(keys, it.Item().KeyCopy(nil))
		}
		it.Close()
		for _, k := range keys {
			if err := txn.Delete(k); err != nil {
				return err
			}
		}

		for _, a := range accounts {
			if strings.TrimSpace(a.ID) == "" {
				continue
			}
			b, err := EncodeJSON(a)
			if err != nil {
				return err
			}
			if err := txn.Set(KeyAccount(a.ID), b); err != nil {
				return err
			}
		}
		if strings.TrimSpace(activeID) != "" {
			return txn.Set(KeyAccountActive(), []byte(activeID))
		}
		_ = txn.Delete(KeyAccountActive())
		return nil
	})
}

// ReadAccounts reads all Polymarket accounts from Badger (excluding active marker).
func (d *DB) ReadAccounts(ctx context.Context) ([]PolymarketAccount, error) {
	if d == nil {
		return nil, errors.New("badgerdb: nil db")
	}
	var out []PolymarketAccount
	err := d.View(func(txn *badger.Txn) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		prefix := []byte(AccountPrefix)
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			k := string(it.Item().Key())
			if k == string(KeyAccountActive()) {
				continue
			}
			if !strings.HasPrefix(k, AccountPrefix) {
				continue
			}
			var a PolymarketAccount
			if err := it.Item().Value(func(val []byte) error {
				return DecodeJSON(val, &a)
			}); err != nil {
				return err
			}
			out = append(out, a)
		}
		return nil
	})
	return out, err
}

// ReadActiveAccountID returns the active account id from account/active, if set.
func (d *DB) ReadActiveAccountID(ctx context.Context) (string, error) {
	if d == nil {
		return "", errors.New("badgerdb: nil db")
	}
	var id string
	err := d.View(func(txn *badger.Txn) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		item, err := txn.Get(KeyAccountActive())
		if errors.Is(err, badger.ErrKeyNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			id = string(val)
			return nil
		})
	})
	return strings.TrimSpace(id), err
}

// ReadAccount loads one account document by id.
func (d *DB) ReadAccount(ctx context.Context, id string) (*PolymarketAccount, error) {
	if d == nil {
		return nil, errors.New("badgerdb: nil db")
	}
	if strings.TrimSpace(id) == "" {
		return nil, nil
	}
	var a PolymarketAccount
	var notFound bool
	err := d.View(func(txn *badger.Txn) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		item, err := txn.Get(KeyAccount(id))
		if errors.Is(err, badger.ErrKeyNotFound) {
			notFound = true
			return nil
		}
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			return DecodeJSON(val, &a)
		})
	})
	if err != nil {
		return nil, err
	}
	if notFound {
		return nil, nil
	}
	return &a, nil
}
