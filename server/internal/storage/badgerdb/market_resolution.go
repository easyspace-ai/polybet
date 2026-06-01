package badgerdb

import (
	"context"
	"errors"
	"strings"
	"time"

	badger "github.com/dgraph-io/badger/v4"
)

// MarketResolutionSource identifies how resolvedAt was recorded.
const (
	ResolutionSourceWS        = "ws"
	ResolutionSourceGammaSync = "gamma_sync"
)

// MarketResolutionDoc is persisted settlement metadata for analytics bucketing.
type MarketResolutionDoc struct {
	ConditionID    string   `json:"conditionId"`
	ResolvedAt     string   `json:"resolvedAt"`
	WinningOutcome string   `json:"winningOutcome,omitempty"`
	WinningAssetID string   `json:"winningAssetId,omitempty"`
	TokenIDs       []string `json:"tokenIds,omitempty"`
	Source         string   `json:"source"`
}

// UpsertMarketResolution records or updates settlement time. WS events take precedence
// over gamma_sync when an existing row was written from the websocket.
func (d *DB) UpsertMarketResolution(ctx context.Context, doc *MarketResolutionDoc) error {
	if d == nil || doc == nil {
		return nil
	}
	cid := strings.TrimSpace(doc.ConditionID)
	if cid == "" {
		return errors.New("badgerdb: conditionId required")
	}
	if strings.TrimSpace(doc.ResolvedAt) == "" {
		doc.ResolvedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if strings.TrimSpace(doc.Source) == "" {
		doc.Source = ResolutionSourceGammaSync
	}
	return d.Update(func(txn *badger.Txn) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var existing MarketResolutionDoc
		if ok, err := d.getJSON(txn, KeyMarketResolution(cid), &existing); err != nil {
			return err
		} else if ok && existing.Source == ResolutionSourceWS && doc.Source == ResolutionSourceGammaSync {
			doc.ResolvedAt = existing.ResolvedAt
			doc.Source = existing.Source
			if doc.WinningOutcome == "" {
				doc.WinningOutcome = existing.WinningOutcome
			}
			if doc.WinningAssetID == "" {
				doc.WinningAssetID = existing.WinningAssetID
			}
		}
		b, err := EncodeJSON(doc)
		if err != nil {
			return err
		}
		if err := txn.Set(KeyMarketResolution(cid), b); err != nil {
			return err
		}
		for _, tok := range doc.TokenIDs {
			tid := NormalizeCLOBTokenID(tok)
			if tid == "" {
				continue
			}
			if err := txn.Set(KeyMarketResolutionToken(tid), []byte(cid)); err != nil {
				return err
			}
		}
		return nil
	})
}

// ResolvedAtForToken returns official settlement instant when known.
func (d *DB) ResolvedAtForToken(ctx context.Context, tokenID string) (time.Time, string, bool) {
	if d == nil {
		return time.Time{}, "", false
	}
	var out time.Time
	var source string
	var ok bool
	_ = d.View(func(txn *badger.Txn) error {
		cid, has, err := d.resolutionConditionForTokenTxn(txn, tokenID)
		if err != nil || !has {
			return err
		}
		var doc MarketResolutionDoc
		if hasDoc, err := d.getJSON(txn, KeyMarketResolution(cid), &doc); err != nil || !hasDoc {
			return err
		}
		t := ParseTimeFlexible(doc.ResolvedAt)
		if t.IsZero() {
			return nil
		}
		out, source, ok = t, doc.Source, true
		return nil
	})
	return out, source, ok
}

func (d *DB) resolutionConditionForTokenTxn(txn *badger.Txn, tokenID string) (string, bool, error) {
	for _, candidate := range CLOBTokenLookupVariants(tokenID) {
		item, err := txn.Get(KeyMarketResolutionToken(candidate))
		if errors.Is(err, badger.ErrKeyNotFound) {
			continue
		}
		if err != nil {
			return "", false, err
		}
		var cid string
		if err := item.Value(func(v []byte) error {
			cid = string(v)
			return nil
		}); err != nil {
			return "", false, err
		}
		if strings.TrimSpace(cid) != "" {
			return cid, true, nil
		}
	}
	// Fall back: token → outcome → market external id (condition id on many sports ML markets).
	oid, has, err := d.findOutcomeIDByTokenTxn(txn, tokenID)
	if err != nil || !has {
		return "", false, err
	}
	var od outcomeDoc
	if ok, err := d.getJSON(txn, KeyMarketOutcome(oid), &od); err != nil || !ok {
		return "", false, err
	}
	var md marketDoc
	if ok, err := d.getJSON(txn, KeyMarketMarket(od.MarketID), &md); err != nil || !ok {
		return "", false, err
	}
	ext := strings.TrimSpace(md.ExternalID)
	if ext == "" {
		return "", false, nil
	}
	return ext, true, nil
}

// settlementDateKeyET returns YYYY-MM-DD in America/New_York for bucketing.
func settlementDateKeyET(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		loc = time.UTC
	}
	local := t.In(loc)
	return local.Format("2006-01-02")
}
