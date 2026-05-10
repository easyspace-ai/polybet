package risksvc

import (
	"context"
	"encoding/json"

	"github.com/easyspace-ai/polybet/internal/store"
)

type priceRange struct {
	MinCents    float64 `json:"minCents"`
	MaxCents    float64 `json:"maxCents"`
	StopLossPct float64 `json:"stopLossPct"`
}

const defaultStopPct = 20.0

func resolveStopLossPct(ctx context.Context, st *store.Store, entryCents float64) float64 {
	raw, ok, err := st.GetBotConfig(ctx, "priceStopLossRanges")
	if err != nil || !ok || raw == "" {
		return defaultStopPct
	}
	var ranges []priceRange
	if err := json.Unmarshal([]byte(raw), &ranges); err != nil {
		return defaultStopPct
	}
	for _, r := range ranges {
		if entryCents >= r.MinCents && entryCents < r.MaxCents {
			return r.StopLossPct
		}
	}
	return defaultStopPct
}
