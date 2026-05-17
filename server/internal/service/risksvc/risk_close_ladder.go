package risksvc

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/easyspace-ai/polybet/internal/logx"
	"github.com/easyspace-ai/polybet/internal/polywiring"
	"github.com/easyspace-ai/polybet/internal/store"
)

// riskCloseModeLadder activates a per-attempt close ladder. Tiers are read
// from bot_config.riskCloseLadderTiers (JSON). The default ladder is a
// trader-friendly progression that starts gentle, escalates aggressiveness,
// and ends with a hedge to lock losses if the book truly cannot be sold.
const riskCloseModeLadder = "ladder"

const botKeyRiskCloseLadderTiers = "riskCloseLadderTiers"

// ladderTier configures one attempt slot.
//
// Type:
//   - "fok_sell"      → call runCloseFOKSell with sellExtraTicks=ExtraTicks
//   - "fak_sell"      → call runCloseFAKSell with worstPrice derived from
//                       ExtraTicks (= bestBid - ExtraTicks*tick) when > 0,
//                       otherwise from WorstPriceAbs (0–1 absolute floor),
//                       otherwise the global riskCloseFakWorstPrice config.
//   - "hedge_fok_buy" → call runCloseHedgeFOKBuy (extra_ticks/worst_price ignored)
//
// Tier indexing: Tiers[i] applies to RiskTask.Attempts == i. When Attempts
// exceeds len(Tiers)-1, the LAST tier is sticky (so an indefinite hedge
// fallback is achieved by ending the slice with hedge_fok_buy).
type ladderTier struct {
	Type          string  `json:"type"`
	ExtraTicks    int     `json:"extraTicks,omitempty"`
	WorstPriceAbs float64 `json:"worstPriceAbs,omitempty"`
}

// defaultLadderTiers is the trader-recommended progression:
//
//	Attempt 0: FOK at bid - 2 ticks   — gentle probe (no slippage when book is real)
//	Attempt 1: FAK at bid - 5 ticks   — accept partial; sweep top of book
//	Attempt 2: FAK at bid - 15 ticks  — give up some price to clear remainder
//	Attempt 3+: Hedge FOK BUY         — lock losses when bid is gone
//
// Operators can override via bot_config.riskCloseLadderTiers.
func defaultLadderTiers() []ladderTier {
	return []ladderTier{
		{Type: riskCloseModeFOKSell, ExtraTicks: 2},
		{Type: riskCloseModeFAKSell, ExtraTicks: 5},
		{Type: riskCloseModeFAKSell, ExtraTicks: 15},
		{Type: riskCloseModeHedgeFOKBuy},
	}
}

// resolveLadderTiers reads operator overrides; falls back to defaults on any
// parse error or empty list. Invalid types in the JSON are dropped silently
// (operator typo should not break the ladder; logged at debug).
func resolveLadderTiers(ctx context.Context, st *store.Store) []ladderTier {
	raw, ok, err := st.GetBotConfig(ctx, botKeyRiskCloseLadderTiers)
	if err != nil || !ok || strings.TrimSpace(raw) == "" {
		return defaultLadderTiers()
	}
	var tiers []ladderTier
	if err := json.Unmarshal([]byte(raw), &tiers); err != nil {
		return defaultLadderTiers()
	}
	out := make([]ladderTier, 0, len(tiers))
	for _, t := range tiers {
		t.Type = strings.TrimSpace(strings.ToLower(t.Type))
		switch t.Type {
		case riskCloseModeFOKSell, riskCloseModeFAKSell, riskCloseModeHedgeFOKBuy:
			out = append(out, t)
		default:
			// Drop unknown tier types rather than crash the close path.
		}
	}
	if len(out) == 0 {
		return defaultLadderTiers()
	}
	return out
}

// tierForAttempt selects the tier for a given attempt count, with last-entry
// stickiness. Callers must ensure tiers is non-empty.
func tierForAttempt(tiers []ladderTier, attempts int) ladderTier {
	if attempts < 0 {
		attempts = 0
	}
	if attempts >= len(tiers) {
		return tiers[len(tiers)-1]
	}
	return tiers[attempts]
}

// activeCloseTierType returns the close mode that the next attempt at the
// given attempt count would dispatch to, given the current bot config.
// Returns "" when the operator has not switched to ladder mode (the legacy
// non-laddered modes are constant per process).
//
// Used by the retry-backoff logic so the parent goroutine can pick a
// longer floor when the ladder has reached the hedge tier — without the
// runClosePosition dispatch needing to thread tier info up through the
// error return.
func (s *Service) activeCloseTierType(ctx context.Context, attempts int) string {
	if effectiveRiskCloseExecutionMode(ctx, s.st) != riskCloseModeLadder {
		return effectiveRiskCloseExecutionMode(ctx, s.st)
	}
	tiers := resolveLadderTiers(ctx, s.st)
	return tierForAttempt(tiers, attempts).Type
}

// runCloseLadder dispatches to the appropriate underlying close routine for
// the configured ladder tier. It overrides the per-attempt aggressiveness
// (sell extra ticks, FAK worst price) so the global retry-amplification
// (effectiveFokSellExtraTicks) is bypassed in favour of the explicit tier.
func (s *Service) runCloseLadder(
	ctx context.Context,
	cl *polywiring.AuthedCLOB,
	task store.RiskTask,
	pos *store.RiskPosition,
	taskID, positionID, queueReason string,
	evalBidCents, evalAskCents, trailCents float64,
	modeExtra *closeAttemptExtras,
) error {
	tiers := resolveLadderTiers(ctx, s.st)
	tier := tierForAttempt(tiers, task.Attempts)

	if s.log != nil {
		s.log.WithFields(logx.Pairs(
			"task_id", taskID, "position_id", positionID, "attempts", task.Attempts,
			"tier_type", tier.Type, "tier_extra_ticks", tier.ExtraTicks, "tier_worst_price_abs", tier.WorstPriceAbs,
			"tier_count", len(tiers),
		)).Info("风控：平仓 ladder 选择 tier")
		logx.StopLoss().WithFields(logx.Pairs(
			"task_id", taskID, "position_id", positionID, "attempts", task.Attempts,
			"tier_type", tier.Type, "tier_extra_ticks", tier.ExtraTicks,
		)).Info("风控：平仓 ladder 选择 tier")
	}

	if modeExtra != nil {
		modeExtra.LadderTier = tier.Type
		modeExtra.LadderAttempt = task.Attempts
	}

	switch tier.Type {
	case riskCloseModeFOKSell:
		extra := tier.ExtraTicks
		if extra <= 0 {
			extra = 2
		}
		return s.runCloseFOKSell(ctx, cl, task, pos, taskID, positionID, queueReason, extra, evalBidCents, evalAskCents, trailCents, modeExtra)

	case riskCloseModeFAKSell:
		// Translate ExtraTicks into a worstPrice (0–1) floor: bestBid - N*tick.
		// Tick is unknown here without a book fetch; use a common 0.01 default
		// when bestBid is in cents. This is a coarse heuristic — operators who
		// need exact tick alignment should set WorstPriceAbs directly. The FAK
		// implementation truncates to the actual tick after fetching the book,
		// so a slightly off limit is corrected anyway.
		worst := s.resolveLadderFakWorstPrice(ctx, tier, evalBidCents)
		return s.runCloseFAKSellWithWorst(ctx, cl, task, pos, taskID, positionID, queueReason, tier.ExtraTicks, worst, evalBidCents, evalAskCents, trailCents, modeExtra)

	case riskCloseModeHedgeFOKBuy:
		// Hedge sizing is governed by riskHedgeBuySizing; tier doesn't
		// override it. Reuse the existing routine; the sellExtra parameter
		// is unused on the hedge path (it logs only).
		return s.runCloseHedgeFOKBuy(ctx, cl, task, pos, taskID, positionID, queueReason, 0, evalBidCents, evalAskCents, trailCents, modeExtra)

	default:
		// Defensive: should not be reachable thanks to resolveLadderTiers
		// filtering. Fall back to FOK sell with a moderate floor.
		return s.runCloseFOKSell(ctx, cl, task, pos, taskID, positionID, queueReason, 2, evalBidCents, evalAskCents, trailCents, modeExtra)
	}
}

// resolveLadderFakWorstPrice picks the per-tier FAK floor:
//  1. WorstPriceAbs if explicitly set (0 < val < 1)
//  2. bestBid - ExtraTicks * 0.01 when ExtraTicks > 0 and bestBid known
//     (clamped to [0.0001, 0.9999])
//  3. Global riskCloseFakWorstPrice as last resort
func (s *Service) resolveLadderFakWorstPrice(ctx context.Context, tier ladderTier, evalBidCents float64) float64 {
	if tier.WorstPriceAbs > 0 && tier.WorstPriceAbs < 1 {
		return tier.WorstPriceAbs
	}
	if tier.ExtraTicks > 0 && evalBidCents > 0 {
		// Use 0.01 as the assumed tick. The downstream FAK truncates to the
		// real tick after fetching the book, so this approximation only
		// affects the "how aggressive is this tier" intent, not validity.
		bid01 := evalBidCents / 100.0
		floor := bid01 - float64(tier.ExtraTicks)*0.01
		if floor < 0.0001 {
			floor = 0.0001
		}
		if floor > 0.9999 {
			floor = 0.9999
		}
		return floor
	}
	return s.st.GetBotConfigFloat(ctx, botKeyRiskCloseFakWorstPrice, 0.01)
}
