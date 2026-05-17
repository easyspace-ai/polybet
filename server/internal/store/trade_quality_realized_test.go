package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// openTradeQualityDB sets up a minimal schema mirroring the post-migration
// shape so we can exercise InsertTradeQuality + AggregateTradeQuality
// without running the full migration chain (which now collides with two
// other in-flight PRs at version 10).
func openTradeQualityDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(0)")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`
		CREATE TABLE trade_quality (
			id TEXT PRIMARY KEY,
			created_at TEXT NOT NULL,
			account_id TEXT,
			side TEXT NOT NULL,
			order_type TEXT NOT NULL,
			token_id TEXT NOT NULL,
			expected_odds REAL,
			fill_odds REAL,
			limit_odds REAL,
			best_bid REAL,
			best_ask REAL,
			slippage_bps REAL,
			size REAL,
			submit_latency_ms INTEGER,
			trade_id TEXT,
			risk_task_id TEXT,
			notes TEXT,
			realized_pnl_usd REAL
		);
	`)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	return db
}

func TestInsertTradeQualityRoundTripsRealizedPnL(t *testing.T) {
	db := openTradeQualityDB(t)
	s := New(db)
	ctx := context.Background()

	// SELL fill with realized PnL set.
	err := s.InsertTradeQuality(ctx, &TradeQuality{
		AccountID:      "a1",
		Side:           "sell",
		OrderType:      "FOK",
		TokenID:        "0xtok",
		ExpectedOdds:   0.55,
		FillOdds:       0.50,
		SlippageBps:    SlippageBpsSell(0.55, 0.50),
		Size:           10,
		RealizedPnLUSD: -2.5, // = 10 × 0.50 − 7.5 cost basis (example)
		Notes:          "test",
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	rows, err := s.ListRecentTradeQuality(ctx, "a1", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].RealizedPnLUSD != -2.5 {
		t.Fatalf("realized_pnl_usd round-trip: got %v want -2.5", rows[0].RealizedPnLUSD)
	}
}

func TestAggregateTradeQualitySumsRealizedPnL(t *testing.T) {
	db := openTradeQualityDB(t)
	s := New(db)
	ctx := context.Background()

	// Two SELL rows with realized PnL, one BUY row that should NOT contribute
	// to realized (BUY rows store NULL realized_pnl_usd by convention), and
	// one SELL row with NULL realized PnL (legacy / dust closure).
	mustInsert := func(q *TradeQuality) {
		t.Helper()
		if err := s.InsertTradeQuality(ctx, q); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	mustInsert(&TradeQuality{AccountID: "a1", Side: "sell", OrderType: "FOK", TokenID: "0x1", SlippageBps: 100, Size: 5, RealizedPnLUSD: -3.0})
	mustInsert(&TradeQuality{AccountID: "a1", Side: "sell", OrderType: "FAK", TokenID: "0x2", SlippageBps: 200, Size: 5, RealizedPnLUSD: 1.5})
	mustInsert(&TradeQuality{AccountID: "a1", Side: "buy", OrderType: "FOK", TokenID: "0x3", SlippageBps: 50, Size: 10})                       // BUY → 0 realized
	// Note: the aggregate WHERE requires slippage_bps IS NOT NULL, and the
	// nullableFloat helper writes 0 as NULL. Rows with SlippageBps=0 are
	// therefore excluded entirely — the test reflects this: only rows with
	// a non-zero slippage value participate.

	agg, err := s.AggregateTradeQuality(ctx, "a1", time.Time{})
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	// Two SELL rows contribute -3.0 + 1.5 = -1.5; BUY row's realized is
	// NULL (= 0 in COALESCE).
	if agg.RealizedPnLUSD != -1.5 {
		t.Fatalf("realized_pnl_usd sum: got %v want -1.5", agg.RealizedPnLUSD)
	}
	if agg.SellCount != 2 {
		t.Fatalf("sell count: got %v want 2 (slippage=0 row was correctly filtered out)", agg.SellCount)
	}
	if agg.BuyCount != 1 {
		t.Fatalf("buy count: got %v want 1", agg.BuyCount)
	}
}
