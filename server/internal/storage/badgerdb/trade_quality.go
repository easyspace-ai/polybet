package badgerdb

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	badger "github.com/dgraph-io/badger/v4"

	"github.com/easyspace-ai/polybet/internal/domain"
)

// TradeQuality mirrors store.TradeQuality for persistence.
type TradeQuality struct {
	ID              string
	CreatedAt       time.Time
	AccountID       string
	Side            string
	OrderType       string
	TokenID         string
	ExpectedOdds    float64
	FillOdds        float64
	LimitOdds       float64
	BestBid         float64
	BestAsk         float64
	SlippageBps     float64
	Size            float64
	SubmitLatencyMs int64
	TradeID         string
	RiskTaskID      string
	Notes           string
	RealizedPnLUSD  float64
}

// TradeQualityAggregate mirrors store.TradeQualityAggregate.
type TradeQualityAggregate struct {
	Count          int
	AvgSlippageBps float64
	MaxSlippageBps float64
	BuyCount       int
	SellCount      int
	BuyAvgBps      float64
	SellAvgBps     float64
	RealizedPnLUSD float64
}

// EventRealizedPnL mirrors store.EventRealizedPnL.
type EventRealizedPnL struct {
	PolyEventSlug  string
	RealizedPnLUSD float64
	Fills          int
}

func (d *DB) InsertTradeQuality(ctx context.Context, q *TradeQuality) error {
	if d == nil || q == nil {
		return nil
	}
	if strings.TrimSpace(q.ID) == "" {
		q.ID = domain.NewID()
	}
	if q.CreatedAt.IsZero() {
		q.CreatedAt = time.Now().UTC()
	}
	tsNano := q.CreatedAt.UTC().UnixNano()
	b, err := EncodeJSON(q)
	if err != nil {
		return err
	}
	return d.Update(func(txn *badger.Txn) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		k := KeyTradeQuality(q.AccountID, tsNano, q.ID)
		return txn.Set(k, b)
	})
}

func (d *DB) AggregateTradeQuality(ctx context.Context, accountID string, since time.Time) (TradeQualityAggregate, error) {
	out := TradeQualityAggregate{}
	var rows []*TradeQuality
	pfx := "trade/quality/"
	if strings.TrimSpace(accountID) != "" {
		pfx = "trade/quality/" + accountID + "/"
	}
	err := d.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Seek([]byte(pfx)); it.ValidForPrefix([]byte(pfx)); it.Next() {
			var q TradeQuality
			if err := it.Item().Value(func(v []byte) error { return DecodeJSON(v, &q) }); err != nil {
				continue
			}
			if !since.IsZero() && q.CreatedAt.Before(since) {
				continue
			}
			if math.IsNaN(q.SlippageBps) {
				continue
			}
			rows = append(rows, &q)
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	if len(rows) == 0 {
		return out, nil
	}
	out.Count = len(rows)
	var sumBps, maxBps float64
	var buyN, sellN int
	var buySum, sellSum float64
	for _, q := range rows {
		sumBps += q.SlippageBps
		if q.SlippageBps > maxBps {
			maxBps = q.SlippageBps
		}
		switch strings.ToLower(q.Side) {
		case "buy":
			buyN++
			buySum += q.SlippageBps
		case "sell":
			sellN++
			sellSum += q.SlippageBps
			out.RealizedPnLUSD += q.RealizedPnLUSD
		}
	}
	out.AvgSlippageBps = sumBps / float64(len(rows))
	out.MaxSlippageBps = maxBps
	out.BuyCount, out.SellCount = buyN, sellN
	if buyN > 0 {
		out.BuyAvgBps = buySum / float64(buyN)
	}
	if sellN > 0 {
		out.SellAvgBps = sellSum / float64(sellN)
	}
	return out, nil
}

func (d *DB) RealizedPnLByEvent(ctx context.Context, accountID string, since time.Time, limit int) ([]EventRealizedPnL, error) {
	if strings.TrimSpace(accountID) == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	type agg struct {
		sum   float64
		count int
	}
	m := make(map[string]*agg)
	err := d.View(func(txn *badger.Txn) error {
		pfx := []byte("trade/quality/" + accountID + "/")
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Seek(pfx); it.ValidForPrefix(pfx); it.Next() {
			var q TradeQuality
			if err := it.Item().Value(func(v []byte) error { return DecodeJSON(v, &q) }); err != nil {
				continue
			}
			if strings.ToLower(q.Side) != "sell" || q.RealizedPnLUSD == 0 {
				continue
			}
			if !since.IsZero() && q.CreatedAt.Before(since) {
				continue
			}
			slug := d.polySlugForToken(txn, q.TokenID)
			if slug == "" {
				continue
			}
			if m[slug] == nil {
				m[slug] = &agg{}
			}
			m[slug].sum += q.RealizedPnLUSD
			m[slug].count++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var out []EventRealizedPnL
	for slug, a := range m {
		out = append(out, EventRealizedPnL{PolyEventSlug: slug, RealizedPnLUSD: a.sum, Fills: a.count})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].RealizedPnLUSD < out[j].RealizedPnLUSD
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (d *DB) polySlugForToken(txn *badger.Txn, tokenID string) string {
	tok := NormalizeCLOBTokenID(tokenID)
	for _, c := range []string{tok, strings.TrimSpace(tokenID)} {
		if c == "" {
			continue
		}
		oid, ok, err := d.findOutcomeIDByTokenTxn(txn, c)
		if err != nil || !ok {
			continue
		}
		var od outcomeDoc
		if ok, err := d.getJSON(txn, KeyMarketOutcome(oid), &od); err != nil || !ok {
			continue
		}
		var md marketDoc
		if ok, err := d.getJSON(txn, KeyMarketMarket(od.MarketID), &md); err != nil || !ok {
			continue
		}
		var ed eventDoc
		if ok, err := d.getJSON(txn, KeyMarketEvent(md.EventID), &ed); err != nil || !ok {
			continue
		}
		s := strings.TrimSpace(ed.PolySlug)
		if s != "" {
			return s
		}
	}
	return ""
}

func (d *DB) ListRecentTradeQuality(ctx context.Context, accountID string, limit int) ([]TradeQuality, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var rows []TradeQuality
	pfx := []byte("trade/quality/")
	if strings.TrimSpace(accountID) != "" {
		pfx = []byte("trade/quality/" + accountID + "/")
	}
	err := d.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Seek(pfx); it.ValidForPrefix(pfx); it.Next() {
			var q TradeQuality
			if err := it.Item().Value(func(v []byte) error { return DecodeJSON(v, &q) }); err != nil {
				continue
			}
			rows = append(rows, q)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for i := 0; i < len(rows); i++ {
		for j := i + 1; j < len(rows); j++ {
			if rows[i].CreatedAt.Before(rows[j].CreatedAt) {
				rows[i], rows[j] = rows[j], rows[i]
			}
		}
	}
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}
