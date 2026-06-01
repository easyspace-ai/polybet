package badgerdb

import (
	"context"
	"testing"
	"time"

	badger "github.com/dgraph-io/badger/v4"
)

func TestAggregateAnalyticsDaily(t *testing.T) {
	rows := []AnalyticsTradeRow{
		{SettlementDate: "2026-05-20", InvestedUSD: 10, RealizedPnLUSD: 2},
		{SettlementDate: "2026-05-20", InvestedUSD: 5, RealizedPnLUSD: -1},
		{SettlementDate: "2026-05-21", InvestedUSD: 8, RealizedPnLUSD: 3},
	}
	daily := AggregateAnalyticsDaily(rows, "2026-05-20", "2026-05-21")
	if len(daily) != 2 {
		t.Fatalf("got %d days want 2", len(daily))
	}
	if daily[0].Date != "2026-05-21" || daily[1].Date != "2026-05-20" {
		t.Fatalf("desc order: %+v", daily)
	}
	if daily[1].TradeCount != 2 || daily[1].WinCount != 1 {
		t.Fatalf("day0: %+v", daily[1])
	}
	if daily[1].TotalInvestedUSD != 15 || daily[1].ProfitUSD != 1 {
		t.Fatalf("day0 amounts: %+v", daily[1])
	}
}

func TestFilterAnalyticsTrades_dateRange(t *testing.T) {
	rows := []AnalyticsTradeRow{
		{SettlementDate: "2026-05-20", RealizedPnLUSD: 2, InvestedUSD: 10, ClosedAt: time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)},
		{SettlementDate: "", RealizedPnLUSD: -1, InvestedUSD: 5, ClosedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)},
	}
	filtered, tot := FilterAnalyticsTrades(rows, "all", "2026-05-02", "2026-05-31")
	if len(filtered) != 1 || tot.TradeCount != 1 {
		t.Fatalf("range filter: %d tot=%+v", len(filtered), tot)
	}
}

func TestFilterAnalyticsTrades_winLoss(t *testing.T) {
	rows := []AnalyticsTradeRow{
		{SettlementDate: "2026-05-20", RealizedPnLUSD: 2, InvestedUSD: 10},
		{SettlementDate: "2026-05-20", RealizedPnLUSD: -1, InvestedUSD: 5},
	}
	wins, tot := FilterAnalyticsTrades(rows, "win", "", "")
	if len(wins) != 1 || tot.TradeCount != 1 {
		t.Fatalf("wins: %d tot=%+v", len(wins), tot)
	}
	losses, _ := FilterAnalyticsTrades(rows, "loss", "", "")
	if len(losses) != 1 {
		t.Fatalf("losses: %d", len(losses))
	}
}

func TestListClosedPositionsForAnalytics_investedSnapshot(t *testing.T) {
	ctx := context.Background()
	kv, err := Open(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	defer kv.Close()

	doc := &RiskPosDoc{
		ID: "p1", Platform: "polymarket", AccountID: "a1",
		TokenID: "0x0000000000000000000000000000000000000000000000000000000000000001",
		Title: "Game", AvgEntryCents: 50, SizeShares: 10, CostUSD: 8,
		Status: "open", StopLossPct: 20, HighWaterCents: 50,
		CreatedAt: nowRFC(), UpdatedAt: nowRFC(),
	}
	if err := kv.Update(func(txn *badger.Txn) error { return kv.writeRiskPos(txn, doc) }); err != nil {
		t.Fatal(err)
	}
	if err := kv.CloseRiskPositionPnL(ctx, "p1", 1.5); err != nil {
		t.Fatal(err)
	}
	rows, err := kv.ListClosedPositionsForAnalytics(ctx, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].InvestedUSD != 8 || rows[0].RealizedPnLUSD != 1.5 {
		t.Fatalf("rows: %+v", rows)
	}
	_ = time.Now()
}
