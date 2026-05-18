package badgerdb

import (
	"context"
	"errors"
	"strings"
	"time"

	badger "github.com/dgraph-io/badger/v4"
)

// DefaultStopLossPct mirrors store.DefaultStopLossPct.
const DefaultStopLossPct = 20.0

// RiskPosDoc is persisted at risk/position/{id} (merged config fields).
type RiskPosDoc struct {
	ID             string   `json:"id"`
	Platform       string   `json:"platform"`
	AccountID      string   `json:"accountId"`
	OutcomeID      string   `json:"outcomeId,omitempty"`
	TokenID        string   `json:"tokenId"`
	Title          string   `json:"title"`
	SideLabel      string   `json:"sideLabel"`
	PolyEventSlug  string   `json:"polyEventSlug"`
	PolyMarketSlug string   `json:"polyMarketSlug"`
	AvgEntryCents  float64  `json:"avgEntryCents"`
	SizeShares     float64  `json:"sizeShares"`
	CostUSD        float64  `json:"costUsd"`
	HighWaterCents float64  `json:"highWaterCents"`
	StopLossPct    float64  `json:"stopLossPct"`
	Source         string   `json:"source"`
	Status         string   `json:"status"`
	PositionSeq    int64    `json:"positionSeq,omitempty"`
	RealizedPnLUSD *float64 `json:"realizedPnlUsd,omitempty"`
	ClosedAt       *string  `json:"closedAt,omitempty"`
	CreatedAt      string   `json:"createdAt"`
	UpdatedAt      string   `json:"updatedAt"`
}

// RiskTaskDoc persisted at risk/task/{id}.
type RiskTaskDoc struct {
	ID                string `json:"id"`
	Type              string `json:"type"`
	PositionID        string `json:"positionId,omitempty"`
	Status            string `json:"status"`
	Attempts          int    `json:"attempts"`
	LastError         string `json:"lastError,omitempty"`
	Reason            string `json:"reason,omitempty"`
	LastAttemptDetail string `json:"lastAttemptDetail,omitempty"`
	NextRunAt         string `json:"nextRunAt"`
	CreatedAt         string `json:"createdAt"`
	UpdatedAt         string `json:"updatedAt"`
}

func nowRFC() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func (d *DB) readRiskPos(txn *badger.Txn, id string) (*RiskPosDoc, error) {
	var p RiskPosDoc
	ok, err := d.getJSON(txn, KeyRiskPosition(id), &p)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return &p, nil
}

func (d *DB) writeRiskPos(txn *badger.Txn, p *RiskPosDoc) error {
	if p == nil {
		return errors.New("nil position")
	}
	b, err := EncodeJSON(p)
	if err != nil {
		return err
	}
	return txn.Set(KeyRiskPosition(p.ID), b)
}

func riskOpenKey(accountID, tokenID, side string) []byte {
	return KeyRiskOpen(strings.TrimSpace(accountID), NormalizeCLOBTokenID(tokenID), strings.TrimSpace(side))
}

// --- Applied trade dedupe ---

func (d *DB) InsertRiskAppliedTrade(ctx context.Context, id, accountID string) (bool, error) {
	if d == nil {
		return false, errors.New("badgerdb: nil db")
	}
	var inserted bool
	err := d.Update(func(txn *badger.Txn) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		k := KeyRiskApplied(accountID, id)
		_, getErr := txn.Get(k)
		if getErr == nil {
			inserted = false
			return nil
		}
		if !errors.Is(getErr, badger.ErrKeyNotFound) {
			return getErr
		}
		inserted = true
		return txn.Set(k, []byte{1})
	})
	return inserted, err
}

// --- Hidden ---

type hiddenDoc struct {
	CreatedAt string `json:"createdAt"`
}

func (d *DB) UpsertRiskHiddenPosition(ctx context.Context, accountID, tokenID, sideLabel string) error {
	if d == nil {
		return errors.New("badgerdb: nil db")
	}
	tid := NormalizeCLOBTokenID(tokenID)
	return d.Update(func(txn *badger.Txn) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		h := hiddenDoc{CreatedAt: nowRFC()}
		b, err := EncodeJSON(h)
		if err != nil {
			return err
		}
		return txn.Set(KeyRiskHidden(accountID, tid, sideLabel), b)
	})
}

func (d *DB) DeleteRiskHiddenPosition(ctx context.Context, accountID, tokenID, sideLabel string) error {
	if d == nil {
		return errors.New("badgerdb: nil db")
	}
	tid := NormalizeCLOBTokenID(tokenID)
	return d.Update(func(txn *badger.Txn) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		return txn.Delete(KeyRiskHidden(accountID, tid, sideLabel))
	})
}

// RiskHiddenRow is one hidden marker.
type RiskHiddenRow struct {
	AccountID string
	TokenID   string
	SideLabel string
	CreatedAt string
}

func (d *DB) ListRiskHiddenPositions(ctx context.Context, accountID string) ([]RiskHiddenRow, error) {
	if d == nil || strings.TrimSpace(accountID) == "" {
		return nil, nil
	}
	prefix := []byte("risk/hidden/" + accountID + "/")
	var out []RiskHiddenRow
	err := d.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			k := string(it.Item().Key())
			parts := strings.Split(k, "/")
			if len(parts) < 5 {
				continue
			}
			tok := parts[len(parts)-2]
			side := parts[len(parts)-1]
			var h hiddenDoc
			_ = it.Item().Value(func(v []byte) error { return DecodeJSON(v, &h) })
			out = append(out, RiskHiddenRow{AccountID: accountID, TokenID: tok, SideLabel: side, CreatedAt: h.CreatedAt})
		}
		return nil
	})
	return out, err
}

func (d *DB) IsRiskPositionHidden(ctx context.Context, accountID, tokenID, sideLabel string) (bool, error) {
	if d == nil || strings.TrimSpace(accountID) == "" {
		return false, nil
	}
	tid := NormalizeCLOBTokenID(tokenID)
	var found bool
	err := d.View(func(txn *badger.Txn) error {
		_, err := txn.Get(KeyRiskHidden(accountID, tid, sideLabel))
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
