package marketsvc

import (
	"context"

	"github.com/easyspace-ai/polybet/internal/bookcache"
	"github.com/easyspace-ai/polybet/internal/store"
)

func BuildMarketsPayload(ctx context.Context, st *store.Store, cache *bookcache.Cache) ([]map[string]any, error) {
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
			"outcomes":  arr,
		})
	}
	return out, nil
}
