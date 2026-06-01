package autoorder

import (
	"strings"

	"github.com/easyspace-ai/polybet/internal/store"
)

// MatchCandidate ties an active moneyline market to a configured group.
type MatchCandidate struct {
	Market   store.MarketRow
	Outcomes []store.OutcomeRow
	Group    Group
}

func normTeam(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func teamMatches(eventTeam string, gt Team) bool {
	et := normTeam(eventTeam)
	if et == normTeam(gt.Name) {
		return true
	}
	if ab := normTeam(gt.Abbreviation); ab != "" && et == ab {
		return true
	}
	return false
}

func groupMatchesEvent(g Group, m store.MarketRow) bool {
	if !g.Enabled {
		return false
	}
	if normTeam(m.League) != normTeam(g.League) {
		return false
	}
	for _, t := range g.Teams {
		if teamMatches(m.HomeTeam, t) || teamMatches(m.AwayTeam, t) {
			return true
		}
	}
	return false
}

// MatchGroups scans active markets and returns candidates for enabled groups.
func MatchGroups(cfg Config, markets []store.MarketRow, outcomesByMarket map[string][]store.OutcomeRow) []MatchCandidate {
	if !cfg.AnyGroupEnabled() {
		return nil
	}
	var out []MatchCandidate
	for _, m := range markets {
		if m.BetType != "12" {
			continue
		}
		os := outcomesByMarket[m.ID]
		if len(os) < 2 {
			continue
		}
		for _, g := range cfg.Groups {
			if !groupMatchesEvent(g, m) {
				continue
			}
			out = append(out, MatchCandidate{
				Market:   m,
				Outcomes: os,
				Group:    g,
			})
		}
	}
	return out
}
