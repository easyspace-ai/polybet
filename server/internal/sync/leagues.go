package sync

import "strings"

// Static league Polymarket series (subset of bot/src/leagues.ts).
type League struct {
	Slug          string
	Sport         string
	League        string
	SeriesID      int
	TitleOrdering string // "home" | "away"
}

var polyLeaguesByTag = map[string]League{
	"nba": {Slug: "nba", Sport: "basketball", League: "nba", SeriesID: 10345, TitleOrdering: "away"},
	"nhl": {Slug: "nhl", Sport: "hockey", League: "nhl", SeriesID: 10346, TitleOrdering: "away"},
	"mlb": {Slug: "mlb", Sport: "baseball", League: "mlb", SeriesID: 3, TitleOrdering: "away"},
}

func leaguesFromTags(tags []string) []League {
	var out []League
	seen := map[int]struct{}{}
	for _, t := range tags {
		key := strings.ToLower(strings.TrimSpace(t))
		if lg, ok := polyLeaguesByTag[key]; ok {
			if _, dup := seen[lg.SeriesID]; dup {
				continue
			}
			seen[lg.SeriesID] = struct{}{}
			out = append(out, lg)
		}
	}
	if len(out) == 0 {
		return []League{polyLeaguesByTag["nba"]}
	}
	return out
}
