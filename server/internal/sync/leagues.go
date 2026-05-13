package sync

import (
	"log/slog"
	"strings"
)

// League is the internal mapping needed to fetch Gamma events for a sport.
type League struct {
	Slug          string
	Sport         string
	League        string
	SeriesID      int
	TitleOrdering string // "home" | "away"
}

// LeagueFromSport resolves a user tag (e.g. "nba") to a League using the live Gamma /sports list.
func LeagueFromSport(tag string, sports []GammaSport) *League {
	key := strings.ToLower(strings.TrimSpace(tag))
	for _, s := range sports {
		if s.Sport == key {
			ordering := s.Ordering
			if ordering == "" {
				ordering = "away"
			}
			return &League{
				Slug:          s.Sport,
				Sport:         s.Sport,
				League:        s.Sport,
				SeriesID:      s.SeriesID,
				TitleOrdering: ordering,
			}
		}
	}
	return nil
}

// leaguesFromTags converts user-configured tags into League entries.
// Unknown tags are logged as warnings.
func leaguesFromTags(tags []string, sports []GammaSport) []League {
	var out []League
	seen := map[int]struct{}{}
	for _, t := range tags {
		lg := LeagueFromSport(t, sports)
		if lg == nil {
			slog.Warn("league_tag_unmatched", "tag", t)
			continue
		}
		if _, dup := seen[lg.SeriesID]; dup {
			continue
		}
		seen[lg.SeriesID] = struct{}{}
		out = append(out, *lg)
	}
	if len(out) == 0 {
		slog.Warn("league_no_tags_matched_fallback_nba", "tags", tags)
		if fallback := LeagueFromSport("nba", sports); fallback != nil {
			return []League{*fallback}
		}
	}
	return out
}
