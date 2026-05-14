package httpserver

import (
	"testing"
	"time"

	"github.com/easyspace-ai/polybet/internal/service/routersvc"
	"github.com/easyspace-ai/polybet/internal/service/tradesvc"
	"github.com/easyspace-ai/polybet/internal/store"
)

func TestSimpleCache_GetSet(t *testing.T) {
	c := newSimpleCache(100 * time.Millisecond)
	c.Set("key1", "value1")

	v, ok := c.Get("key1")
	if !ok {
		t.Fatal("expected key1 to exist")
	}
	if v.(string) != "value1" {
		t.Fatalf("expected value1, got %v", v)
	}
}

func TestSimpleCache_Expiry(t *testing.T) {
	c := newSimpleCache(10 * time.Millisecond)
	c.Set("key1", "value1")

	time.Sleep(50 * time.Millisecond)

	_, ok := c.Get("key1")
	if ok {
		t.Fatal("expected key1 to have expired")
	}
}

func TestSimpleCache_Delete(t *testing.T) {
	c := newSimpleCache(time.Minute)
	c.Set("key1", "value1")
	c.Delete("key1")

	_, ok := c.Get("key1")
	if ok {
		t.Fatal("expected key1 to be deleted")
	}
}

func TestSimpleCache_Miss(t *testing.T) {
	c := newSimpleCache(time.Minute)
	_, ok := c.Get("nonexistent")
	if ok {
		t.Fatal("expected nonexistent key to miss")
	}
}

func TestTradeResultsLog_empty(t *testing.T) {
	if got := tradeResultsLog(nil); got != "[]" {
		t.Fatalf("expected [], got %q", got)
	}
	if got := tradeResultsLog([]tradesvc.TradeResult{}); got != "[]" {
		t.Fatalf("expected [], got %q", got)
	}
}

func TestTradeResultsLog_single(t *testing.T) {
	rs := []tradesvc.TradeResult{
		{Platform: "polymarket", Status: "filled", TradeID: "tx123"},
	}
	got := tradeResultsLog(rs)
	want := "polymarket:filled:tx123"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestTradeResultsLog_withFailure(t *testing.T) {
	rs := []tradesvc.TradeResult{
		{Platform: "polymarket", Status: "failed", TradeID: "tx456", FailureReason: "insufficient_liquidity"},
	}
	got := tradeResultsLog(rs)
	want := "polymarket:failed:tx456:insufficient_liquidity"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestMapRouterErr_nil(t *testing.T) {
	if got := mapRouterErr(nil); got != 400 {
		t.Fatalf("expected 400 for nil, got %d", got)
	}
}

func TestMapRouterErr_codes(t *testing.T) {
	tests := []struct {
		code string
		want int
	}{
		{"outcome_not_found", 404},
		{"size_exceeds_max", 400},
		{"slippage_exceeded", 422},
		{"unknown_code", 400},
	}
	for _, tt := range tests {
		err := &routersvc.RouterError{Code: tt.code}
		if got := mapRouterErr(err); got != tt.want {
			t.Errorf("mapRouterErr(%q) = %d, want %d", tt.code, got, tt.want)
		}
	}
}

func TestRiskRowFromPosition(t *testing.T) {
	hw := 100.0
	p := &store.RiskPosition{
		ID: "pos1", Title: "Test Position", SideLabel: "Yes",
		AvgEntryCents: 50, SizeShares: 10, CostUSD: 5,
		HighWaterCents: hw, StopLossPct: 20,
		Status: "open", Source: "manual",
	}
	row := riskRowFromPosition(p)
	if row["id"] != "pos1" {
		t.Fatalf("expected pos1, got %v", row["id"])
	}
	if row["status"] != "open" {
		t.Fatalf("expected open, got %v", row["status"])
	}
	trail, ok := row["trailingStopCents"].(float64)
	if !ok {
		t.Fatal("expected trailingStopCents to be float64")
	}
	if trail != 80 {
		t.Fatalf("expected trailingStopCents 80, got %f", trail)
	}
}

func TestTradeResultsLog_multiple(t *testing.T) {
	rs := []tradesvc.TradeResult{
		{Platform: "poly", Status: "filled", TradeID: "a"},
		{Platform: "poly", Status: "failed", TradeID: "b", FailureReason: "slippage"},
	}
	got := tradeResultsLog(rs)
	want := "poly:filled:a;poly:failed:b:slippage"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
