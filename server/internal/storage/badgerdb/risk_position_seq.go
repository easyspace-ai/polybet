package badgerdb

import (
	"context"
	"errors"
	"sort"
	"strconv"

	badger "github.com/dgraph-io/badger/v4"
)

func readRiskPositionSeq(txn *badger.Txn) (int64, error) {
	item, err := txn.Get(KeyRiskPositionSeq())
	if errors.Is(err, badger.ErrKeyNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var n int64
	err = item.Value(func(v []byte) error {
		parsed, perr := strconv.ParseInt(string(v), 10, 64)
		if perr != nil {
			return perr
		}
		n = parsed
		return nil
	})
	return n, err
}

func writeRiskPositionSeq(txn *badger.Txn, seq int64) error {
	return txn.Set(KeyRiskPositionSeq(), []byte(strconv.FormatInt(seq, 10)))
}

func (d *DB) nextRiskPositionSeq(txn *badger.Txn) (int64, error) {
	cur, err := readRiskPositionSeq(txn)
	if err != nil {
		return 0, err
	}
	next := cur + 1
	if err := writeRiskPositionSeq(txn, next); err != nil {
		return 0, err
	}
	return next, nil
}

// MigrateRiskPositionSeq assigns display sequence numbers to legacy rows missing
// positionSeq, ordered by createdAt, and advances the global counter.
func (d *DB) MigrateRiskPositionSeq(ctx context.Context) error {
	if d == nil {
		return nil
	}
	type pending struct {
		id        string
		createdAt string
	}
	var need []pending
	err := d.View(func(txn *badger.Txn) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		pfx := []byte("risk/position/")
		for it.Seek(pfx); it.ValidForPrefix(pfx); it.Next() {
			var p RiskPosDoc
			if err := it.Item().Value(func(v []byte) error { return DecodeJSON(v, &p) }); err != nil {
				continue
			}
			if p.PositionSeq > 0 {
				continue
			}
			need = append(need, pending{id: p.ID, createdAt: p.CreatedAt})
		}
		return nil
	})
	if err != nil || len(need) == 0 {
		return err
	}
	sort.Slice(need, func(i, j int) bool {
		ti := ParseTimeFlexible(need[i].createdAt)
		tj := ParseTimeFlexible(need[j].createdAt)
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		return need[i].id < need[j].id
	})
	return d.Update(func(txn *badger.Txn) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		for _, row := range need {
			p, err := d.readRiskPos(txn, row.id)
			if err != nil || p == nil || p.PositionSeq > 0 {
				continue
			}
			seq, err := d.nextRiskPositionSeq(txn)
			if err != nil {
				return err
			}
			p.PositionSeq = seq
			if err := d.writeRiskPos(txn, p); err != nil {
				return err
			}
		}
		return nil
	})
}
