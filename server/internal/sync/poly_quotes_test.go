package sync

import (
	"testing"
	"time"
)

func TestOutcomeIndexForTitleTeam(t *testing.T) {
	labels := []string{"Lakers", "Celtics"}
	if i := outcomeIndexForTitleTeam("Los Angeles Lakers", labels); i != 0 {
		t.Fatalf("home long title: got %d want 0", i)
	}
	if i := outcomeIndexForTitleTeam("Boston Celtics", labels); i != 1 {
		t.Fatalf("away long title: got %d want 1", i)
	}
	if i := outcomeIndexForTitleTeam("Lakers", labels); i != 0 {
		t.Fatalf("exact: got %d want 0", i)
	}
}

func TestQuoteFromMoneyline12_combinedMarket(t *testing.T) {
	outcomes := `["Los Angeles Lakers","Boston Celtics"]`
	tokens := `["tok-lal-yes","tok-bos-yes"]`
	prices := `["0.55","0.45"]`
	ev := gammaEvent{
		ID:    "ev1",
		Title: "Lakers vs. Celtics",
		Markets: []gammaMarket{
			{
				Question:         "Who will win?",
				ClobTokenIDs:     tokens,
				Outcomes:         outcomes,
				OutcomePrices:    prices,
				Active:           true,
				Closed:           false,
				Liquidity:        "1000",
				SportsMarketType: ptrStr("moneyline"),
			},
		},
	}
	// titleOrdering "home" => first team in title is home (matches Gamma outcome order in this fixture).
	lg := League{Slug: "nba", Sport: "basketball", League: "nba", SeriesID: 10345, TitleOrdering: "home"}
	q, err := quoteFromMoneyline12(ev, lg)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Outcomes) != 2 {
		t.Fatalf("outcomes len %d", len(q.Outcomes))
	}
	if q.HomeTeam != "Los Angeles Lakers" || q.AwayTeam != "Boston Celtics" {
		t.Fatalf("teams %q vs %q", q.HomeTeam, q.AwayTeam)
	}
	if q.Outcomes[0].ExternalID != "tok-lal-yes" || q.Outcomes[1].ExternalID != "tok-bos-yes" {
		t.Fatalf("tokens %+v", q.Outcomes)
	}
}

func TestStartTimeFromEventPrefersGameStartTimeWithShortUTCOffset(t *testing.T) {
	gameStart := "2026-05-12 07:00:00+00"
	ev := gammaEvent{
		StartDate: "2026-05-11T00:00:00Z",
		EndDate:   "2026-05-13T07:00:00Z",
		Markets: []gammaMarket{
			{
				GameStartTime: &gameStart,
			},
		},
	}

	got := startTimeFromEvent(ev)
	want := time.Date(2026, time.May, 12, 7, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("startTimeFromEvent() = %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

func TestParseGammaTimeAcceptsCommonGammaFormats(t *testing.T) {
	cases := []string{
		"2026-05-12 07:00:00+00",
		"2026-05-12 07:00:00+00:00",
		"2026-05-12T07:00:00Z",
	}
	for _, raw := range cases {
		got, ok := parseGammaTime(raw)
		if !ok {
			t.Fatalf("parseGammaTime(%q) failed", raw)
		}
		if got.UTC().Format(time.RFC3339) != "2026-05-12T07:00:00Z" {
			t.Fatalf("parseGammaTime(%q) = %s", raw, got.UTC().Format(time.RFC3339))
		}
	}
}

func TestParseGammaTimeNoTimezoneAssumesUTC(t *testing.T) {
	cases := []string{
		"2026-05-12 19:00:00",
		"2026-05-12T19:00:00",
		"2026-05-12 19:00:00.000",
	}
	for _, raw := range cases {
		got, ok := parseGammaTime(raw)
		if !ok {
			t.Fatalf("parseGammaTime(%q) failed — should treat bare time as UTC", raw)
		}
		want := "2026-05-12T19:00:00Z"
		if got.UTC().Format(time.RFC3339) != want {
			t.Fatalf("parseGammaTime(%q) = %s, want %s", raw, got.UTC().Format(time.RFC3339), want)
		}
	}
}

func TestStartTimeFromEventReturnsZeroWhenNoFieldDecodes(t *testing.T) {
	// All time fields missing or unparseable → return zero, NOT time.Now().
	// The post-kickoff open-block gate relies on this so it can detect the
	// unknown case via IsKnownStartTime instead of being silently bypassed.
	bad := "not-a-timestamp"
	ev := gammaEvent{
		ID:        "bad-time",
		Title:     "X vs Y",
		StartDate: "",
		EndDate:   "",
		Markets:   []gammaMarket{{GameStartTime: &bad}},
	}
	got := startTimeFromEvent(ev)
	if !got.IsZero() {
		t.Fatalf("expected zero time when no field decodes, got %s", got.Format(time.RFC3339))
	}
}

func TestStartTimeFromEventFallsBackToEndDateViaParseGammaTime(t *testing.T) {
	// EndDate with short offset — parseGammaTime handles it, bare time.Parse(RFC3339) would fail.
	gameStart := ""
	ev := gammaEvent{
		ID:        "test1",
		Title:     "Team A vs. Team B",
		StartDate: "2026-05-11T00:00:00Z",
		EndDate:   "2026-05-12 00:00:00+00",
		Markets: []gammaMarket{
			{
				GameStartTime: &gameStart,
				Active:        true,
				Closed:        false,
			},
		},
	}
	got := startTimeFromEvent(ev)
	want := time.Date(2026, time.May, 12, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("startTimeFromEvent() = %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

func ptrStr(s string) *string { return &s }
