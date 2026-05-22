package risksvc

import (
	"testing"

	"github.com/easyspace-ai/polybet/internal/gammaclient"
	"github.com/easyspace-ai/polybet/internal/store"
)

func TestResolveRiskLeague_displayMeta(t *testing.T) {
	got := ResolveRiskLeague(store.RiskDisplayMeta{League: "MLB"}, gammaclient.TokenMarketDisplay{})
	if got != "mlb" {
		t.Fatalf("got %q want mlb", got)
	}
}

func TestResolveRiskLeague_polySlug(t *testing.T) {
	got := ResolveRiskLeague(
		store.RiskDisplayMeta{},
		gammaclient.TokenMarketDisplay{},
		"mlb-nyy-bos-2026-05-22",
	)
	if got != "mlb" {
		t.Fatalf("got %q want mlb", got)
	}
}

func TestResolveRiskLeague_gammaCategory(t *testing.T) {
	got := ResolveRiskLeague(
		store.RiskDisplayMeta{},
		gammaclient.TokenMarketDisplay{Category: "nba"},
	)
	if got != "nba" {
		t.Fatalf("got %q want nba", got)
	}
}

func TestLeagueFromPolySlug(t *testing.T) {
	if got := leagueFromPolySlug("nhl-tor-bos-2026-05-22"); got != "nhl" {
		t.Fatalf("got %q want nhl", got)
	}
	if got := leagueFromPolySlug("unknown-event"); got != "" {
		t.Fatalf("got %q want empty", got)
	}
}
