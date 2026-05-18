package badgerdb

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	badger "github.com/dgraph-io/badger/v4"
)

// OfficialTradeDoc is a cached Polymarket Data API fill for history UI.
type OfficialTradeDoc struct {
	ID          string  `json:"id"`
	AccountID   string  `json:"accountId"`
	Side        string  `json:"side"`
	Title       string  `json:"title"`
	Outcome     string  `json:"outcome"`
	Size        float64 `json:"size"`
	Price       float64 `json:"price"`
	PriceCents  float64 `json:"priceCents"`
	Timestamp   string  `json:"timestamp"`
	Icon        string  `json:"icon"`
	PolySlug    string  `json:"polySlug,omitempty"`
	OfficialURL string  `json:"officialUrl,omitempty"`
	SyncedAt    string  `json:"syncedAt"`
}

func KeyOfficialTrade(accountID string, tsNano int64, id string) []byte {
	return []byte("official/trade/" + accountID + "/" + strconv.FormatInt(tsNano, 10) + "/" + id)
}

// UpsertOfficialTrades replaces the account's cached official fills (newest-first cap).
func (d *DB) UpsertOfficialTrades(ctx context.Context, accountID string, rows []OfficialTradeDoc) error {
	if d == nil {
		return errors.New("badgerdb: nil db")
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return errors.New("badgerdb: accountID required")
	}
	pfx := []byte("official/trade/" + accountID + "/")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return d.Update(func(txn *badger.Txn) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Seek(pfx); it.ValidForPrefix(pfx); it.Next() {
			if err := txn.Delete(it.Item().KeyCopy(nil)); err != nil {
				return err
			}
		}
		for _, row := range rows {
			row.AccountID = accountID
			if row.SyncedAt == "" {
				row.SyncedAt = now
			}
			ts := ParseTimeFlexible(row.Timestamp).UnixNano()
			if ts <= 0 {
				ts = time.Now().UTC().UnixNano()
			}
			b, err := EncodeJSON(row)
			if err != nil {
				return err
			}
			if err := txn.Set(KeyOfficialTrade(accountID, ts, row.ID), b); err != nil {
				return err
			}
		}
		return nil
	})
}

func (d *DB) ListOfficialTrades(ctx context.Context, accountID string, limit int) ([]OfficialTradeDoc, error) {
	if d == nil {
		return nil, errors.New("badgerdb: nil db")
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	pfx := []byte("official/trade/" + accountID + "/")
	var rows []OfficialTradeDoc
	err := d.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Seek(pfx); it.ValidForPrefix(pfx); it.Next() {
			var doc OfficialTradeDoc
			if err := it.Item().Value(func(v []byte) error { return DecodeJSON(v, &doc) }); err != nil {
				continue
			}
			rows = append(rows, doc)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(rows, func(i, j int) bool {
		ti := ParseTimeFlexible(rows[i].Timestamp)
		tj := ParseTimeFlexible(rows[j].Timestamp)
		return ti.After(tj)
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}
