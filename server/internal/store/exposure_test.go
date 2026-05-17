package store

import (
	"context"
	"testing"
)

func TestAccountOpenExposureUSD(t *testing.T) {
	kv := openTestBadger(t)
	s := New(kv)
	ctx := context.Background()

	if got, err := s.AccountOpenExposureUSD(ctx, "acct1"); err != nil || got != 0 {
		t.Fatalf("empty: got=%v err=%v", got, err)
	}

	mustPos := func(accountID, id, tok, slug string, cost float64, status string) {
		t.Helper()
		p := &RiskPosition{
			ID: id, Platform: "polymarket", AccountID: accountID, TokenID: tok, SideLabel: "Y",
			PolyEventSlug: slug, CostUSD: cost, Status: status, SizeShares: 100,
			AvgEntryCents: 50, Source: "test",
		}
		if err := s.CreateRiskPosition(ctx, p); err != nil {
			t.Fatal(err)
		}
	}

	mustPos("acct1", "p1", "0xtok1", "game-a", 50.5, "open")
	mustPos("acct1", "p2", "0xtok2", "game-b", 30.0, "open")
	mustPos("acct1", "p3", "0xtok3", "game-c", 99.0, "closing")
	mustPos("acct1", "p4", "0xtok4", "game-d", 99.0, "closed")
	mustPos("acct2", "p5", "0xtok5", "game-e", 1000.0, "open")

	got, err := s.AccountOpenExposureUSD(ctx, "acct1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != 80.5 {
		t.Fatalf("expected 80.5 (50.5 + 30, open only), got %v", got)
	}

	if got, err := s.AccountOpenExposureUSD(ctx, ""); err != nil || got != 0 {
		t.Fatalf("empty acct: got=%v err=%v", got, err)
	}
}

func TestMarketOpenExposureUSD(t *testing.T) {
	kv := openTestBadger(t)
	s := New(kv)
	ctx := context.Background()

	mustPos := func(accountID, id, tok, slug string, cost float64, status string) {
		t.Helper()
		p := &RiskPosition{
			ID: id, Platform: "polymarket", AccountID: accountID, TokenID: tok, SideLabel: "Y",
			PolyEventSlug: slug, CostUSD: cost, Status: status, SizeShares: 100,
			AvgEntryCents: 50, Source: "test",
		}
		if err := s.CreateRiskPosition(ctx, p); err != nil {
			t.Fatal(err)
		}
	}

	mustPos("acct1", "p1", "0xtok1", "lakers-vs-celtics", 40.0, "open")
	mustPos("acct1", "p2", "0xtok2", "lakers-vs-celtics", 25.0, "open")
	mustPos("acct1", "p3", "0xtok3", "lakers-vs-celtics", 10.0, "closed")
	mustPos("acct1", "p4", "0xtok4", "warriors-vs-bucks", 100.0, "open")
	mustPos("acct2", "p5", "0xtok5", "lakers-vs-celtics", 999.0, "open")

	got, err := s.MarketOpenExposureUSD(ctx, "acct1", "lakers-vs-celtics")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != 65.0 {
		t.Fatalf("expected 65, got %v", got)
	}

	got, err = s.MarketOpenExposureUSD(ctx, "acct1", "event/lakers-vs-celtics/")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != 65.0 {
		t.Fatalf("prefix-stripped slug: got %v want 65", got)
	}

	got, err = s.MarketOpenExposureUSD(ctx, "acct1", "no-such-game")
	if err != nil || got != 0 {
		t.Fatalf("unknown event: got=%v err=%v", got, err)
	}

	if got, err := s.MarketOpenExposureUSD(ctx, "", "anything"); err != nil || got != 0 {
		t.Fatalf("empty acct: got=%v err=%v", got, err)
	}
	if got, err := s.MarketOpenExposureUSD(ctx, "acct1", ""); err != nil || got != 0 {
		t.Fatalf("empty slug: got=%v err=%v", got, err)
	}
}
