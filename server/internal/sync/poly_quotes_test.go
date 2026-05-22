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

func TestStartTimeFromEventParsesEndDateWhenNoGameStartTime(t *testing.T) {
	ev := gammaEvent{
		EndDate: "2026-05-12 07:00:00+00",
		Markets: []gammaMarket{{Question: "ML", SportsMarketType: ptrStr("moneyline")}},
	}

	got := startTimeFromEvent(ev, "nba", nil)
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

func TestParseGammaTimeSportsBareAssumesEastern(t *testing.T) {
	raw := "2026-05-20 08:30:00"
	got, ok := parseGammaTimeSports(raw)
	if !ok {
		t.Fatalf("parseGammaTimeSports(%q) failed", raw)
	}
	loc, _ := time.LoadLocation("America/New_York")
	want := time.Date(2026, time.May, 20, 8, 30, 0, 0, loc).UTC()
	if !got.Equal(want) {
		t.Fatalf("parseGammaTimeSports(%q) = %s, want %s", raw, got.Format(time.RFC3339), want.Format(time.RFC3339))
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
	got := startTimeFromEvent(ev, "nba", nil)
	if !got.IsZero() {
		t.Fatalf("expected zero time when no field decodes, got %s", got.Format(time.RFC3339))
	}
}

func TestStartTimeFromEventSasOkcPrefersEndDate(t *testing.T) {
	// nba-sas-okc-2026-05-20: Poly endDate = 8:30 PM ET; stray morning gameStartTime must not win.
	endDate := "2026-05-21T00:30:00.000Z"
	gst := "2026-05-20 08:30:00"
	ev := gammaEvent{
		ID:      "ev-sas-okc",
		Title:   "Spurs vs. Thunder",
		EndDate: endDate,
		Markets: []gammaMarket{
			{Question: "ML", GameStartTime: &gst, SportsMarketType: ptrStr("moneyline")},
		},
	}
	got := startTimeFromEvent(ev, "nba", nil)
	want := time.Date(2026, time.May, 21, 0, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("startTimeFromEvent() = %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

func TestStartTimeFromEventMLBPrefersMoneylineGameStart(t *testing.T) {
	// mlb-chc-hou-2026-05-22: Poly card shows 2:20 AM ET; endDate is ~7d later at 2:20 PM.
	endDate := "2026-05-29 14:20:00"
	gst := "2026-05-22 02:20:00"
	ev := gammaEvent{
		ID:      "ev-mlb-chc-hou",
		Title:   "Chicago Cubs @ Houston Astros",
		EndDate: endDate,
		Markets: []gammaMarket{
			{Question: "ML", GameStartTime: &gst, SportsMarketType: ptrStr("moneyline")},
		},
	}
	got := startTimeFromEvent(ev, "mlb", &ev.Markets[0])
	loc, _ := time.LoadLocation("America/New_York")
	want := time.Date(2026, time.May, 22, 2, 20, 0, 0, loc).UTC()
	if !got.Equal(want) {
		t.Fatalf("startTimeFromEvent() = %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

func TestParseKickoffCandidateBareUsesEastern(t *testing.T) {
	got, ok := parseKickoffCandidate("2026-05-20 08:30:00")
	if !ok {
		t.Fatal("expected ok")
	}
	loc, _ := time.LoadLocation("America/New_York")
	want := time.Date(2026, time.May, 20, 8, 30, 0, 0, loc).UTC()
	if !got.Equal(want) {
		t.Fatalf("parseKickoffCandidate() = %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

func TestStartTimeFromEventFallsBackToMoneylineGameStartTime(t *testing.T) {
	gstML := "2026-05-20 08:30:00"
	ev := gammaEvent{
		ID:    "ev-ml",
		Title: "A vs B",
		Markets: []gammaMarket{
			{Question: "ML", GameStartTime: &gstML, SportsMarketType: ptrStr("moneyline")},
		},
	}
	got := startTimeFromEvent(ev, "nba", nil)
	loc, _ := time.LoadLocation("America/New_York")
	want := time.Date(2026, time.May, 20, 8, 30, 0, 0, loc).UTC()
	if !got.Equal(want) {
		t.Fatalf("startTimeFromEvent() = %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

func TestStartTimeFromEventMLBRejectsDistantEndDateUsesSlug(t *testing.T) {
	// Refresh instability: Gamma sometimes omits gameStartTime; endDate is +7d window.
	endDate := "2026-05-29 14:20:00"
	ev := gammaEvent{
		ID:      "ev-mlb",
		Slug:    "mlb-chc-hou-2026-05-22",
		Title:   "Chicago Cubs @ Houston Astros",
		EndDate: endDate,
		Markets: []gammaMarket{
			{Question: "ML", SportsMarketType: ptrStr("moneyline"), Active: true},
		},
	}
	got := startTimeFromEvent(ev, "mlb", &ev.Markets[0])
	loc, _ := time.LoadLocation("America/New_York")
	want := time.Date(2026, time.May, 22, 0, 0, 0, 0, loc).UTC()
	if !got.Equal(want) {
		t.Fatalf("startTimeFromEvent() = %s, want slug day %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

func TestStartTimeFromEventMLBGameStartOnSpreadMarket(t *testing.T) {
	gst := "2026-05-22 02:20:00"
	ev := gammaEvent{
		Slug:    "mlb-chc-hou-2026-05-22",
		EndDate: "2026-05-29 14:20:00",
		Markets: []gammaMarket{
			{Question: "ML", SportsMarketType: ptrStr("moneyline"), Active: true},
			{Question: "Spread", SportsMarketType: ptrStr("spreads"), GameStartTime: &gst, Active: true},
		},
	}
	got := startTimeFromEvent(ev, "mlb", &ev.Markets[0])
	loc, _ := time.LoadLocation("America/New_York")
	want := time.Date(2026, time.May, 22, 2, 20, 0, 0, loc).UTC()
	if !got.Equal(want) {
		t.Fatalf("startTimeFromEvent() = %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

func TestExtractTeamsAtSign(t *testing.T) {
	home, away, ok := extractTeams("New York Yankees @ Boston Red Sox", "away")
	if !ok {
		t.Fatal("expected ok")
	}
	if home != "Boston Red Sox" || away != "New York Yankees" {
		t.Fatalf("teams %q vs %q", home, away)
	}
}

func ptrStr(s string) *string { return &s }
