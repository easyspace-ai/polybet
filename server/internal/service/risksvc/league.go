package risksvc

import (
	"context"
	"strings"

	"github.com/easyspace-ai/polybet/internal/gammaclient"
	"github.com/easyspace-ai/polybet/internal/store"
)

var knownLeagueTags = map[string]struct{}{
	"nba": {}, "nhl": {}, "mlb": {}, "nfl": {}, "wnba": {},
	"cbb": {}, "cfb": {}, "epl": {}, "mls": {},
}

func leagueFromPolySlug(slug string) string {
	slug = strings.ToLower(normalizePolySlug(slug))
	if slug == "" {
		return ""
	}
	if i := strings.Index(slug, "-"); i > 0 {
		prefix := slug[:i]
		if _, ok := knownLeagueTags[prefix]; ok {
			return prefix
		}
	}
	return ""
}

// ResolveRiskLeague returns a lowercase league tag (nba/nhl/mlb) for filtering.
func ResolveRiskLeague(dm store.RiskDisplayMeta, gm gammaclient.TokenMarketDisplay, slugs ...string) string {
	if lg := strings.ToLower(strings.TrimSpace(dm.League)); lg != "" {
		return lg
	}
	for _, slug := range slugs {
		if lg := leagueFromPolySlug(slug); lg != "" {
			return lg
		}
	}
	if cat := strings.ToLower(strings.TrimSpace(gm.Category)); cat != "" {
		if _, ok := knownLeagueTags[cat]; ok {
			return cat
		}
	}
	if sp := strings.ToLower(strings.TrimSpace(dm.Sport)); sp != "" {
		if _, ok := knownLeagueTags[sp]; ok {
			return sp
		}
	}
	return ""
}

// LeagueForRiskPosition resolves league/category for monitor and history filters.
func (s *Service) LeagueForRiskPosition(ctx context.Context, p *store.RiskPosition) string {
	if p == nil || strings.TrimSpace(p.TokenID) == "" {
		return ""
	}
	disp, _ := s.st.RiskDisplayMetaForPositions(ctx, []store.RiskPosition{*p})
	dm := disp[p.TokenID]
	gm := s.gammaMetaBatch(ctx, []string{p.TokenID})[p.TokenID]
	return ResolveRiskLeague(dm, gm, p.PolyEventSlug, p.PolyMarketSlug, dm.PolySlug)
}
