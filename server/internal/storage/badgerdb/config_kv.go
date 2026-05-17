package badgerdb

import (
	"context"
	"errors"

	badger "github.com/dgraph-io/badger/v4"
)

// ReadBotConfigMap returns the full bot config map stored at config/bot, or
// (nil, nil) when the key is absent.
func (d *DB) ReadBotConfigMap(ctx context.Context) (map[string]string, error) {
	if d == nil {
		return nil, errors.New("badgerdb: nil db")
	}
	var out map[string]string
	err := d.View(func(txn *badger.Txn) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		item, err := txn.Get(KeyConfigBot())
		if errors.Is(err, badger.ErrKeyNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			return DecodeJSON(val, &out)
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// WriteBotConfigMap replaces config/bot with a JSON object of string values.
func (d *DB) WriteBotConfigMap(ctx context.Context, m map[string]string) error {
	if d == nil {
		return errors.New("badgerdb: nil db")
	}
	if m == nil {
		m = map[string]string{}
	}
	b, err := EncodeJSON(m)
	if err != nil {
		return err
	}
	return d.Update(func(txn *badger.Txn) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		return txn.Set(KeyConfigBot(), b)
	})
}

// HasConfigBot reports whether config/bot exists.
func (d *DB) HasConfigBot(ctx context.Context) (bool, error) {
	if d == nil {
		return false, errors.New("badgerdb: nil db")
	}
	var found bool
	err := d.View(func(txn *badger.Txn) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		_, err := txn.Get(KeyConfigBot())
		if errors.Is(err, badger.ErrKeyNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		return nil
	})
	return found, err
}
