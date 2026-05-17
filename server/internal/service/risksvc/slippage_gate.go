package risksvc

import (
	"context"
	"errors"
)

// botKeyRiskCloseMaxSlippageBps caps the projected slippage on a SELL close
// attempt before any CLOB submission. <= 0 disables. Suggested production
// value: 1000 bps (= 10% drop from eval bid). The gate intentionally does
// NOT apply to the hedge_fok_buy tier — hedge is the "lock losses at any
// price" final fallback; capping its slippage would defeat its purpose.
const botKeyRiskCloseMaxSlippageBps = "riskCloseMaxSlippageBps"

// errCloseSlippageCap is returned by the SELL close paths when the projected
// limit price would drop more than riskCloseMaxSlippageBps below the eval
// bid recorded at decision time. The wrapping retry logic counts this as a
// failed attempt so the next ladder tier (or backoff) takes over.
var errCloseSlippageCap = errors.New("close_slippage_cap_exceeded")

// projectedSellSlippageBps approximates the slippage a SELL close would
// realize against the eval bid recorded at decision time. The result is in
// basis points where positive = WORSE than expected (received less).
//
// Formula:
//
//	slippage_bps = (evalBidCents - projectedLimitCents) / evalBidCents * 10000
//	             = (sellExtraTicks * tickCents / evalBidCents) * 10000
//
// We don't have the actual tick size here without an OrderBook fetch, so we
// approximate using 0.01 (= 1¢) which matches the dominant Polymarket sports
// market tick. The projection is deliberately CONSERVATIVE on smaller-tick
// markets: a 0.001 tick would generate less actual slippage than the
// approximation suggests, so the gate may abort earlier than strictly
// necessary — fail-safe in the trader's direction.
//
// Returns 0 when evalBidCents <= 0 (no bid → cannot compute) so the caller
// must short-circuit on that case rather than treating 0 as "no slippage".
func projectedSellSlippageBps(evalBidCents float64, sellExtraTicks int) float64 {
	if evalBidCents <= 0 {
		return 0
	}
	if sellExtraTicks < 0 {
		sellExtraTicks = 0
	}
	const approxTickCents = 1.0
	return float64(sellExtraTicks) * approxTickCents / evalBidCents * 10000.0
}

// checkCloseSlippage returns errCloseSlippageCap when the configured cap is
// active and the projected slippage exceeds it. Returns the projected bps
// in either branch so callers can log it.
func (s *Service) checkCloseSlippage(ctx context.Context, evalBidCents float64, sellExtraTicks int, modeOrTier string) (projectedBps float64, err error) {
	cap := s.st.GetBotConfigFloat(ctx, botKeyRiskCloseMaxSlippageBps, 0)
	projectedBps = projectedSellSlippageBps(evalBidCents, sellExtraTicks)
	if cap <= 0 {
		return projectedBps, nil
	}
	// Hedge tier is the lock-losses-at-any-price fallback; capping it
	// defeats its purpose. Operators who want a hedge slippage cap should
	// raise it on the FOK/FAK tiers and let the ladder route past those.
	if modeOrTier == riskCloseModeHedgeFOKBuy {
		return projectedBps, nil
	}
	if projectedBps > cap {
		return projectedBps, errCloseSlippageCap
	}
	return projectedBps, nil
}

// IsCloseSlippageCapErr reports whether the error is the slippage-cap
// signal. Exported indirection used by ProcessRiskTasksOnce so the retry
// path can pick the right log message + backoff floor.
func IsCloseSlippageCapErr(err error) bool {
	return errors.Is(err, errCloseSlippageCap)
}
