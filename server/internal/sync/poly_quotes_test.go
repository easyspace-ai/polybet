package sync

import "testing"

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

func ptrStr(s string) *string { return &s }
