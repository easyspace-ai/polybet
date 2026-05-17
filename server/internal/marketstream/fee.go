package marketstream

import (
	"math"
	"strconv"
	"strings"
)

// NewMarketEventFeeRate extracts the taker fee fraction (e.g. 0.02 = 2%)
// from a CLOB WS new_market event.
//
// Polymarket's new_market wire shape carries fees in a few places. This
// helper picks the most authoritative non-zero value:
//
//  1. fee_schedule.rate    — string fraction ("0.0150" → 0.015) or bps
//                            ("150" → 0.0150). Heuristic: values >= 1 are
//                            interpreted as bps and divided by 10000.
//  2. taker_base_fee       — string fraction or bps under the same rule.
//
// Returns ok=false when fees_enabled is false, or no field decodes to a
// finite, non-negative, plausible (< 1.0) value. Operators that want a
// hard override should set syncDefaultTakerFeeRate which the sync engine
// applies as a fallback per-market — that path is unaffected by WS events.
func NewMarketEventFeeRate(ev *NewMarketEvent) (float64, bool) {
	if ev == nil {
		return 0, false
	}
	if !ev.FeesEnabled {
		return 0, true // fees explicitly disabled — emit 0 so callers cache "no fee"
	}
	if v, ok := parseFeeStringWithBpsHeuristic(ev.FeeSchedule.Rate); ok {
		return v, true
	}
	if v, ok := parseFeeStringWithBpsHeuristic(ev.TakerBaseFee); ok {
		return v, true
	}
	return 0, false
}

// parseFeeStringWithBpsHeuristic accepts both "0.02" (fraction) and "200"
// (basis points) shapes, picking the right scale based on whether the
// parsed value is >= 1. Values >= 1 are bps and divided by 10000.
//
// Rejects negative, non-finite, and >= 1 fractional values (the latter is
// implausible: 100% taker fee would mean every fill goes to fees).
func parseFeeStringWithBpsHeuristic(s string) (float64, bool) {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(t, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	if v < 0 {
		return 0, false
	}
	if v >= 1 {
		v = v / 10000.0
	}
	if v >= 1 {
		return 0, false
	}
	return v, true
}
