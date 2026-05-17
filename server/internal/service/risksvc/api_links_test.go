package risksvc

import (
	"testing"

	"github.com/easyspace-ai/polybet/internal/gammaclient"
	"github.com/easyspace-ai/polybet/internal/store"
)

func TestPolymarketLinksPrefersDataAPIEventSlug(t *testing.T) {
	eventURL, searchURL := polymarketLinks(
		store.RiskDisplayMeta{},
		gammaclient.TokenMarketDisplay{},
		"Sabres vs. Canadiens",
		"Sabres",
		"nhl-buf-mon-2026-05-16",
		"some-market-slug",
	)
	if searchURL != "" {
		t.Fatalf("searchURL = %q, want empty", searchURL)
	}
	want := "https://polymarket.com/event/nhl-buf-mon-2026-05-16?outcome=Sabres"
	if eventURL != want {
		t.Fatalf("eventURL = %q, want %q", eventURL, want)
	}
}

func TestPolymarketLinksNoSearchFallback(t *testing.T) {
	eventURL, searchURL := polymarketLinks(
		store.RiskDisplayMeta{},
		gammaclient.TokenMarketDisplay{},
		"Some unknown market",
		"Yes",
		"",
		"",
	)
	if searchURL != "" {
		t.Fatalf("searchURL = %q, want empty", searchURL)
	}
	if eventURL != "" {
		t.Fatalf("eventURL = %q, want empty", eventURL)
	}
}
