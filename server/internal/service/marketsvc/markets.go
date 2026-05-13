package marketsvc

import (
	"context"
	"strings"

	"github.com/easyspace-ai/polybet/internal/bookcache"
	"github.com/easyspace-ai/polybet/internal/store"
	mktSync "github.com/easyspace-ai/polybet/internal/sync"
)

// BuildSportIconMap converts GammaSport slice to sport->iconURL lookup.
func BuildSportIconMap(sports []mktSync.GammaSport) map[string]string {
	m := make(map[string]string, len(sports))
	for _, s := range sports {
		key := strings.ToLower(strings.TrimSpace(s.Sport))
		if img := strings.TrimSpace(s.Image); img != "" {
			m[key] = img
		}
	}
	return m
}

func BuildMarketsPayload(ctx context.Context, st *store.Store, cache *bookcache.Cache, sportIcons map[string]string) ([]map[string]any, error) {
	mrows, orows, err := st.ListActiveMarketsFlat(ctx)
	if err != nil {
		return nil, err
	}
	outcomesByMarket := make(map[string][]store.OutcomeRow)
	for _, o := range orows {
		outcomesByMarket[o.MarketID] = append(outcomesByMarket[o.MarketID], o)
	}
	// Non-nil slice serializes to [] in JSON; nil slice becomes null and breaks dashboard.
	out := make([]map[string]any, 0, len(mrows))
	for _, m := range mrows {
		os := outcomesByMarket[m.ID]
		arr := make([]map[string]any, 0, len(os))
		for _, o := range os {
			implied := o.CurrentOdds
			if m.Platform == "polymarket" && o.ExternalID.Valid && o.ExternalID.String != "" {
				if v, ok := cache.TakerOdds(o.ExternalID.String); ok {
					implied = v
				}
			}
			ext := interface{}(nil)
			if o.ExternalID.Valid && o.ExternalID.String != "" {
				ext = o.ExternalID.String
			}
			ck := interface{}(nil)
			if o.CanonicalKey.Valid && o.CanonicalKey.String != "" {
				ck = o.CanonicalKey.String
			}
			arr = append(arr, map[string]any{
				"id": o.ID, "label": o.Label, "platform": m.Platform, "externalId": ext,
				"impliedOdds": implied, "availableSize": o.LiquidityDepth, "lastUpdated": o.LastUpdated,
				"canonicalKey": ck,
			})
		}
		var line any
		if m.Line.Valid {
			line = m.Line.Float64
		}
		var polySlug string
		if strings.TrimSpace(m.PolySlug) != "" {
			polySlug = strings.TrimPrefix(strings.TrimSpace(m.PolySlug), "event/")
		}
		sportKey := strings.ToLower(strings.TrimSpace(m.Sport))
		iconUrl := sportIcons[sportKey]
		out = append(out, map[string]any{
			"id": m.ID, "eventId": m.EventID, "platform": m.Platform, "externalId": m.ExternalID,
			"sport": m.Sport, "league": m.League,
			"homeTeam": m.HomeTeam, "awayTeam": m.AwayTeam,
			"name": m.HomeTeam + " vs " + m.AwayTeam,
			"startTime": m.StartTime,
			"status":    m.Status,
			"betType":   m.BetType,
			"line":      line,
			"mainLine":  m.MainLine != 0,
			"polySlug":  polySlug,
			"iconUrl":   iconUrl,
			"outcomes":  arr,
		})
	}
	return out, nil
}
