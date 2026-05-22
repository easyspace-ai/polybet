package badgerdb

import (
	"context"
	"errors"

	badger "github.com/dgraph-io/badger/v4"
)

// appDataWipePrefixes are Badger key prefixes cleared by ClearAllAppData.
// Preserves config/bot, account/* (credentials), and meta/schema_version.
var appDataWipePrefixes = [][]byte{
	[]byte("market/"),
	[]byte("risk/position/"),
	[]byte("risk/open/"),
	[]byte("risk/closed/"),
	[]byte("risk/task/"),
	[]byte("risk/applied/"),
	[]byte("risk/hidden/"),
	[]byte("trade/record/"),
	[]byte("trade/byAccount/"),
	[]byte("trade/quality/"),
	[]byte("official/trade/"),
}

// ClearAllAppData deletes all persisted application data (markets, positions,
// trades, stop-loss tasks, official trade history). Bot config and Polymarket
// accounts are preserved. Resets meta/risk_position_seq.
func (d *DB) ClearAllAppData(ctx context.Context) (int, error) {
	if d == nil {
		return 0, errors.New("badgerdb: nil db")
	}
	var deleted int
	err := d.Update(func(txn *badger.Txn) error {
		n, err := deleteKeysWithPrefixes(ctx, txn, appDataWipePrefixes)
		deleted += n
		if err != nil {
			return err
		}
		if err := txn.Delete(KeyRiskPositionSeq()); err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}
		return nil
	})
	return deleted, err
}

func deleteKeysWithPrefixes(ctx context.Context, txn *badger.Txn, prefixes [][]byte) (int, error) {
	var deleted int
	for _, prefix := range prefixes {
		if err := ctx.Err(); err != nil {
			return deleted, err
		}
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		var keys [][]byte
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			keys = append(keys, append([]byte(nil), it.Item().Key()...))
		}
		it.Close()
		for _, k := range keys {
			if err := txn.Delete(k); err != nil {
				return deleted, err
			}
			deleted++
		}
	}
	return deleted, nil
}
