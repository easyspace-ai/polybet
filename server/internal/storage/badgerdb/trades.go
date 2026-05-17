package badgerdb

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	badger "github.com/dgraph-io/badger/v4"

	"github.com/easyspace-ai/polybet/internal/domain"
)

type tradeDoc struct {
	ID            string   `json:"id"`
	MarketID      string   `json:"marketId"`
	OutcomeID     string   `json:"outcomeId"`
	Side          string   `json:"side"`
	RequestedSize float64  `json:"requestedSize"`
	RequestedOdds float64  `json:"requestedOdds"`
	ExecutedSize  *float64 `json:"executedSize,omitempty"`
	FillOdds      *float64 `json:"fillOdds,omitempty"`
	Platform      string   `json:"platform"`
	Status        string   `json:"status"`
	TxHash        string   `json:"txHash,omitempty"`
	FailureReason string   `json:"failureReason,omitempty"`
	CreatedAt     string   `json:"createdAt"`
	AccountID     string   `json:"accountId"`
}

// LastTradeSummary mirrors store.LastTradeSummary.
type LastTradeSummary struct {
	Status        string
	Platform      string
	RequestedSize float64
	RequestedOdds float64
	FillOdds      sql.NullFloat64
}

func (d *DB) CreatePendingTrade(ctx context.Context, marketID, outcomeID, platform, side string, reqSize, reqOdds float64, accountID string) (string, error) {
	if d == nil {
		return "", errors.New("badgerdb: nil db")
	}
	id := domain.NewID()
	now := nowRFC()
	doc := tradeDoc{
		ID: id, MarketID: marketID, OutcomeID: outcomeID, Side: side,
		RequestedSize: reqSize, RequestedOdds: reqOdds, Platform: platform, Status: "pending",
		CreatedAt: now, AccountID: accountID,
	}
	b, err := EncodeJSON(doc)
	if err != nil {
		return "", err
	}
	nano := ParseTimeFlexible(now).UnixNano()
	err = d.Update(func(txn *badger.Txn) error {
		if err := txn.Set(KeyTrade(id), b); err != nil {
			return err
		}
		return txn.Set(KeyTradeByAccount(accountID, nano, id), []byte(id))
	})
	return id, err
}

func (d *DB) MarkTradeFilled(ctx context.Context, id, txHash string, execSize, fillOdds float64) error {
	return d.mutateTrade(ctx, id, func(doc *tradeDoc) {
		doc.Status = "filled"
		es, fo := execSize, fillOdds
		doc.ExecutedSize, doc.FillOdds = &es, &fo
		doc.TxHash = txHash
	})
}

func (d *DB) MarkTradeFailed(ctx context.Context, id, reason string) error {
	return d.mutateTrade(ctx, id, func(doc *tradeDoc) {
		doc.Status = "failed"
		doc.FailureReason = reason
	})
}

func (d *DB) mutateTrade(ctx context.Context, id string, fn func(*tradeDoc)) error {
	return d.Update(func(txn *badger.Txn) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var doc tradeDoc
		ok, err := d.getJSON(txn, KeyTrade(id), &doc)
		if err != nil || !ok {
			return err
		}
		fn(&doc)
		b, err := EncodeJSON(doc)
		if err != nil {
			return err
		}
		return txn.Set(KeyTrade(id), b)
	})
}

// ListTrades returns total count and rows as maps (same shape as legacy SQL handler).
func (d *DB) ListTrades(ctx context.Context, page, limit int, accountID string) (total int, trades []map[string]any, err error) {
	if d == nil {
		return 0, nil, errors.New("badgerdb: nil db")
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	var docs []tradeDoc
	err = d.View(func(txn *badger.Txn) error {
		pfx := []byte("trade/byAccount/" + accountID + "/")
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Seek(pfx); it.ValidForPrefix(pfx); it.Next() {
			var tid string
			_ = it.Item().Value(func(v []byte) error {
				tid = string(v)
				return nil
			})
			var doc tradeDoc
			if ok, err := d.getJSON(txn, KeyTrade(tid), &doc); err != nil || !ok {
				continue
			}
			docs = append(docs, doc)
		}
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	total = len(docs)
	for i := 0; i < len(docs); i++ {
		for j := i + 1; j < len(docs); j++ {
			if ParseTimeFlexible(docs[i].CreatedAt).Before(ParseTimeFlexible(docs[j].CreatedAt)) {
				docs[i], docs[j] = docs[j], docs[i]
			}
		}
	}
	offset := (page - 1) * limit
	if offset > len(docs) {
		return total, []map[string]any{}, nil
	}
	end := offset + limit
	if end > len(docs) {
		end = len(docs)
	}
	pageDocs := docs[offset:end]
	trades = make([]map[string]any, 0, len(pageDocs))
	for _, t := range pageDocs {
		_, _, label, _, home, away, _ := d.GetOutcomeWithMarket(ctx, t.OutcomeID)
		marketName := home + " vs " + away
		var officialURL string
		if ps := strings.TrimSpace(d.polySlugForOutcome(ctx, t.OutcomeID)); ps != "" {
			slug := strings.Trim(strings.TrimPrefix(ps, "event/"), "/")
			if slug != "" {
				officialURL = "https://polymarket.com/event/" + slug
			}
		}
		sport := d.sportForOutcome(ctx, t.OutcomeID)
		trades = append(trades, map[string]any{
			"id": t.ID, "createdAt": t.CreatedAt, "side": t.Side, "requestedSize": t.RequestedSize,
			"executedSize": nullFloatPtr(t.ExecutedSize), "requestedOdds": t.RequestedOdds,
			"fillOdds": nullFloatPtr(t.FillOdds), "platform": t.Platform, "status": t.Status,
			"txHash": nullStr(t.TxHash), "failureReason": nullStrPtr(t.FailureReason),
			"outcomeLabel": label, "marketName": marketName, "officialUrl": officialURL, "sport": nullStr(sport),
		})
	}
	return total, trades, err
}

func (d *DB) polySlugForOutcome(ctx context.Context, outcomeID string) string {
	var slug string
	_ = d.View(func(txn *badger.Txn) error {
		var od outcomeDoc
		if ok, err := d.getJSON(txn, KeyMarketOutcome(outcomeID), &od); err != nil || !ok {
			return nil
		}
		var md marketDoc
		if ok, err := d.getJSON(txn, KeyMarketMarket(od.MarketID), &md); err != nil || !ok {
			return nil
		}
		var ed eventDoc
		if ok, err := d.getJSON(txn, KeyMarketEvent(md.EventID), &ed); err != nil || !ok {
			return nil
		}
		slug = ed.PolySlug
		return nil
	})
	return slug
}

func (d *DB) sportForOutcome(ctx context.Context, outcomeID string) string {
	var sport string
	_ = d.View(func(txn *badger.Txn) error {
		var od outcomeDoc
		if ok, err := d.getJSON(txn, KeyMarketOutcome(outcomeID), &od); err != nil || !ok {
			return nil
		}
		var md marketDoc
		if ok, err := d.getJSON(txn, KeyMarketMarket(od.MarketID), &md); err != nil || !ok {
			return nil
		}
		var ed eventDoc
		if ok, err := d.getJSON(txn, KeyMarketEvent(md.EventID), &ed); err != nil || !ok {
			return nil
		}
		sport = strings.TrimSpace(strings.ToLower(ed.Sport))
		return nil
	})
	return sport
}

func nullFloatPtr(f *float64) any {
	if f == nil {
		return nil
	}
	return *f
}

func nullStr(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func nullStrPtr(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func (d *DB) GetLastTradeSummary(ctx context.Context) (*LastTradeSummary, error) {
	if d == nil {
		return nil, errors.New("badgerdb: nil db")
	}
	var best *tradeDoc
	err := d.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		pfx := []byte("trade/record/")
		for it.Seek(pfx); it.ValidForPrefix(pfx); it.Next() {
			var doc tradeDoc
			if err := it.Item().Value(func(v []byte) error { return DecodeJSON(v, &doc) }); err != nil {
				continue
			}
			if best == nil || ParseTimeFlexible(doc.CreatedAt).After(ParseTimeFlexible(best.CreatedAt)) {
				dc := doc
				best = &dc
			}
		}
		return nil
	})
	if err != nil || best == nil {
		return nil, err
	}
	out := &LastTradeSummary{
		Status: best.Status, Platform: best.Platform, RequestedSize: best.RequestedSize, RequestedOdds: best.RequestedOdds,
	}
	if best.FillOdds != nil {
		out.FillOdds = sql.NullFloat64{Float64: *best.FillOdds, Valid: true}
	}
	return out, nil
}
