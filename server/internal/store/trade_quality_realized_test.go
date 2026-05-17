package store

import (
	"context"
	"testing"
	"time"
)

func TestInsertTradeQualityRoundTripsRealizedPnL(t *testing.T) {
	s := New(openTestBadger(t))
	ctx := context.Background()

	err := s.InsertTradeQuality(ctx, &TradeQuality{
		AccountID:      "a1",
		Side:           "sell",
		OrderType:      "FOK",
		TokenID:        "0xtok",
		ExpectedOdds:   0.55,
		FillOdds:       0.50,
		SlippageBps:    SlippageBpsSell(0.55, 0.50),
		Size:           10,
		RealizedPnLUSD: -2.5,
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
	s := New(openTestBadger(t))
	ctx := context.Background()

	mustInsert := func(q *TradeQuality) {
		t.Helper()
		if err := s.InsertTradeQuality(ctx, q); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	mustInsert(&TradeQuality{AccountID: "a1", Side: "sell", OrderType: "FOK", TokenID: "0x1", SlippageBps: 100, Size: 5, RealizedPnLUSD: -3.0})
	mustInsert(&TradeQuality{AccountID: "a1", Side: "sell", OrderType: "FAK", TokenID: "0x2", SlippageBps: 200, Size: 5, RealizedPnLUSD: 1.5})
	mustInsert(&TradeQuality{AccountID: "a1", Side: "buy", OrderType: "FOK", TokenID: "0x3", SlippageBps: 50, Size: 10})

	agg, err := s.AggregateTradeQuality(ctx, "a1", time.Time{})
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if agg.RealizedPnLUSD != -1.5 {
		t.Fatalf("realized_pnl_usd sum: got %v want -1.5", agg.RealizedPnLUSD)
	}
	if agg.SellCount != 2 {
		t.Fatalf("sell count: got %v want 2", agg.SellCount)
	}
	if agg.BuyCount != 1 {
		t.Fatalf("buy count: got %v want 1", agg.BuyCount)
	}
}
