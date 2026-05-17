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

// botKeyStopLossAbsCents holds the absolute cent-trail floor (0 = disabled).
// See TrailingStopCentsFromHWWithAbs for semantics.
const botKeyStopLossAbsCents = "priceStopLossAbsCents"

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

// stopLossAbsCents reads the configured absolute cent-trail floor (0 disables).
// Returns 0 on parse error so the legacy percent-only path is preserved.
func stopLossAbsCents(ctx context.Context, st *store.Store) float64 {
	if st == nil {
		return 0
	}
	v := st.GetBotConfigFloat(ctx, botKeyStopLossAbsCents, 0)
	if v < 0 {
		return 0
	}
	return v
}

// trailingStopCents wraps TrailingStopCentsFromHWWithAbs with the configured
// absolute cent floor. Use this everywhere a fresh trail is computed so the
// abs-floor takes effect uniformly (UI display, stop trigger, snapshot).
func (s *Service) trailingStopCents(ctx context.Context, hwCents, stopLossPct float64) float64 {
	return TrailingStopCentsFromHWWithAbs(hwCents, stopLossPct, stopLossAbsCents(ctx, s.st))
}
