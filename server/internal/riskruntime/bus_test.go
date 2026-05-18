package riskruntime

import (
	"os"
	"testing"
	"time"

	"github.com/easyspace-ai/polybet/internal/wsrelay"
)

func TestMaybePublishMarketBookSummaryEmitsMarketData(t *testing.T) {
	t.Setenv("POLYBET_RUNTIME_BOOK_SUMMARY_DISABLE", "")
	t.Setenv("POLYBET_RUNTIME_BOOK_SUMMARY_MIN_GAP_MS", "100")

	hub := wsrelay.NewHub()
	bus := NewBus(hub, 16)

	bus.MaybePublishMarketBookSummary("0xabc", "acct-1", 45.2, 46.1, nil)

	logs := bus.ListChronological(10)
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}
	if logs[0].Category != "market_data" {
		t.Fatalf("category=%q want market_data", logs[0].Category)
	}
	if logs[0].Type != "market.book.summary_tick" {
		t.Fatalf("type=%q want market.book.summary_tick", logs[0].Type)
	}
	if logs[0].TokenID == nil || *logs[0].TokenID != "0xabc" {
		t.Fatalf("tokenId=%v", logs[0].TokenID)
	}
}

func TestMaybePublishMarketBookSummaryRespectsDisable(t *testing.T) {
	t.Setenv("POLYBET_RUNTIME_BOOK_SUMMARY_DISABLE", "true")

	bus := NewBus(wsrelay.NewHub(), 16)
	bus.MaybePublishMarketBookSummary("0xabc", "acct-1", 45.2, 46.1, nil)
	if len(bus.ListChronological(10)) != 0 {
		t.Fatal("expected no logs when disabled")
	}
}

func TestMaybePublishMarketBookSummaryThrottles(t *testing.T) {
	t.Setenv("POLYBET_RUNTIME_BOOK_SUMMARY_DISABLE", "")
	t.Setenv("POLYBET_RUNTIME_BOOK_SUMMARY_MIN_GAP_MS", "5000")

	bus := NewBus(wsrelay.NewHub(), 16)
	bus.MaybePublishMarketBookSummary("0xabc", "acct-1", 45.0, 46.0, nil)
	bus.MaybePublishMarketBookSummary("0xabc", "acct-1", 45.1, 46.1, nil)
	if len(bus.ListChronological(10)) != 1 {
		t.Fatalf("expected throttle to keep 1 entry, got %d", len(bus.ListChronological(10)))
	}
}

func TestSummaryTickMinGapDefault(t *testing.T) {
	os.Unsetenv("POLYBET_RUNTIME_BOOK_SUMMARY_MIN_GAP_MS")
	if got := summaryTickMinGap(); got != 3*time.Second {
		t.Fatalf("default gap=%v want 3s", got)
	}
}
