package autoorder

import (
	"database/sql"
	"testing"
	"time"

	"github.com/easyspace-ai/polybet/internal/store"
)

func TestMatchGroups_leagueAndTeam(t *testing.T) {
	cfg := Config{
		Enabled: true,
		Groups: []Group{{
			ID: "g1", Name: "NBA A", Enabled: true, League: "nba",
			Teams: []Team{{ID: 1, Name: "Los Angeles Lakers", Abbreviation: "lal"}},
		}},
	}
	markets := []store.MarketRow{{
		ID: "m1", EventID: "e1", BetType: "12", League: "nba",
		HomeTeam: "Los Angeles Lakers", AwayTeam: "Boston Celtics",
	}}
	outcomes := map[string][]store.OutcomeRow{
		"m1": {{ID: "o1"}, {ID: "o2"}},
	}
	got := MatchGroups(cfg, markets, outcomes)
	if len(got) != 1 {
		t.Fatalf("want 1 candidate, got %d", len(got))
	}
	if got[0].Group.ID != "g1" {
		t.Fatalf("group id %q", got[0].Group.ID)
	}
}

func TestMatchGroups_skipsDisabledGroup(t *testing.T) {
	cfg := Config{Groups: []Group{{ID: "g1", Enabled: false, League: "nba", Teams: []Team{{ID: 1, Name: "Lakers"}}}}}
	markets := []store.MarketRow{{ID: "m1", BetType: "12", League: "nba", HomeTeam: "Lakers", AwayTeam: "Celtics"}}
	outcomes := map[string][]store.OutcomeRow{"m1": {{ID: "o1"}, {ID: "o2"}}}
	if len(MatchGroups(cfg, markets, outcomes)) != 0 {
		t.Fatal("expected no matches when group disabled")
	}
}

func TestPriceInGate(t *testing.T) {
	gate := PriceGate{MinCents: 55, MaxCents: 75}
	if !PriceInGate(0.60, gate) {
		t.Fatal("0.60 should be in gate")
	}
	if PriceInGate(0.50, gate) {
		t.Fatal("0.50 should be out of gate")
	}
	if PriceInGate(0.76, gate) {
		t.Fatal("0.76 should be out of gate")
	}
}

func TestStakeFromBands(t *testing.T) {
	bands := []OddsBand{{MinCents: 55, MaxCents: 60, StakePct: 5}}
	stake, ok := StakeFromBands(0.57, bands, 100)
	if !ok || stake != 5 {
		t.Fatalf("want 5, got %v ok=%v", stake, ok)
	}
	_, ok = StakeFromBands(0.70, bands, 100)
	if ok {
		t.Fatal("expected no band match")
	}
}

func TestValidateConfig_fundAndTeams(t *testing.T) {
	c := DefaultConfig()
	c.Groups = []Group{{
		ID: "a", Name: "A", Enabled: true, League: "nba", FundUsd: 50,
		Teams:     []Team{{ID: 1, Name: "Lakers"}},
		PriceGate: PriceGate{MinCents: 55, MaxCents: 75},
		OddsBands: []OddsBand{{MinCents: 55, MaxCents: 60, StakePct: 5}},
		Triggers:  Triggers{MinutesBeforeStart: 30},
	}, {
		ID: "b", Name: "B", Enabled: true, League: "nba", FundUsd: 0,
		Teams:     []Team{{ID: 2, Name: "Celtics"}},
		PriceGate: PriceGate{MinCents: 55, MaxCents: 75},
		OddsBands: []OddsBand{{MinCents: 55, MaxCents: 60, StakePct: 5}},
		Triggers:  Triggers{MinutesBeforeStart: 30},
	}}
	if err := ValidateConfig(&c); err == nil {
		t.Fatal("expected fundUsd error for enabled group")
	}
	c.Groups[1].FundUsd = 30
	c.Groups[1].Teams = []Team{{ID: 1, Name: "Celtics"}}
	if err := ValidateConfig(&c); err == nil {
		t.Fatal("expected duplicate team error")
	}
}

func TestEvaluate_triggerAndPopular(t *testing.T) {
	start := time.Now().UTC().Add(20 * time.Minute)
	cfg := DefaultConfig()
	cfg.OutcomePolicy.MinImpliedOdds = 0.50
	g := Group{
		Enabled: true, League: "nba",
		PriceGate: PriceGate{MinCents: 55, MaxCents: 75},
		Triggers:  Triggers{MinutesBeforeStart: 30, MinEventVolumeUsd: 1000},
		OddsBands: []OddsBand{{MinCents: 55, MaxCents: 75, StakePct: 10}},
	}
	m := store.MarketRow{
		EventID: "e1", BetType: "12", Platform: "polymarket",
		StartTime: start.Format(time.RFC3339Nano), EventVolume: 5000,
		HomeTeam: "Lakers", AwayTeam: "Celtics",
	}
	outcomes := []store.OutcomeRow{
		{ID: "o1", Label: "Lakers", CurrentOdds: 0.45, ExternalID: sql.NullString{String: "t1", Valid: true}},
		{ID: "o2", Label: "Celtics", CurrentOdds: 0.58, ExternalID: sql.NullString{String: "t2", Valid: true}},
	}
	ev := Evaluate(time.Now().UTC(), cfg, MatchCandidate{Market: m, Outcomes: outcomes, Group: g}, nil)
	if !ev.OK {
		t.Fatalf("expected ok, got %q", ev.SkipReason)
	}
	if ev.Outcome.ID != "o2" {
		t.Fatalf("popular should be o2, got %s", ev.Outcome.ID)
	}
}

func TestPickPopularOutcome(t *testing.T) {
	outcomes := []store.OutcomeRow{
		{CurrentOdds: 0.40},
		{CurrentOdds: 0.62},
	}
	best, odds := PickPopularOutcome(outcomes, "polymarket", nil)
	if best == nil || odds != 0.62 {
		t.Fatalf("got %v %v", best, odds)
	}
}
