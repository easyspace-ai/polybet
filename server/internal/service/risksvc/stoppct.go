package risksvc

import (
	"context"
	"encoding/json"

	"github.com/easyspace-ai/polybet/internal/storage"
	"github.com/easyspace-ai/polybet/internal/store"
)

type priceRange struct {
	MinCents    float64 `json:"minCents"`
	MaxCents    float64 `json:"maxCents"`
	StopLossPct float64 `json:"stopLossPct"`
}

// defaultStopPct mirrors store.DefaultStopLossPct so the percent applied
// to a brand-new position with no priceStopLossRanges match equals the
// SQL fallback for legacy rows. Drift between these two constants used
// to silently downgrade legacy positions to a tighter stop than the
// dashboard reported.
const defaultStopPct = store.DefaultStopLossPct

// botKeyStopLossAbsCents holds the absolute cent-trail floor (0 = disabled).
// See TrailingStopCentsFromHWWithAbs for semantics.
const botKeyStopLossAbsCents = "priceStopLossAbsCents"

// TrailingStopActive reports whether trailing stop-loss should ratchet or fire.
func TrailingStopActive(avgEntryCents, stopLossPct float64) bool {
	return avgEntryCents > 0 && stopLossPct > 0
}

func resolveStopLossPct(ctx context.Context, st *storage.Backend, entryCents float64) float64 {
	if entryCents <= 0 {
		return 0
	}
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

// shouldActivateTrailingStop applies band stop % when avg entry becomes known.
func shouldActivateTrailingStop(posAvgEntryCents, posStopLossPct, entryCents float64) bool {
	if entryCents <= 0 {
		return false
	}
	if posAvgEntryCents <= 0 {
		return true
	}
	return posStopLossPct <= 0
}

// activateTrailingStopFromEntry sets stop % from price bands and seeds high-water.
func activateTrailingStopFromEntry(ctx context.Context, st *storage.Backend, positionID string, entryCents, existingHW float64) error {
	stop := resolveStopLossPct(ctx, st, entryCents)
	if stop <= 0 {
		return nil
	}
	hw := FloorCents1(entryCents)
	if existingHW > hw {
		hw = FloorCents1(existingHW)
	}
	return st.UpdateRiskPositionStop(ctx, positionID, &stop, &hw)
}

// stopLossAbsCents reads the configured absolute cent-trail floor (0 disables).
// Returns 0 on parse error so the legacy percent-only path is preserved.
func stopLossAbsCents(ctx context.Context, st *storage.Backend) float64 {
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
