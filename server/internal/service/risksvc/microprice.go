package risksvc

import (
	"context"
	"math"

	"github.com/easyspace-ai/polybet/internal/logx"
	"github.com/easyspace-ai/polybet/internal/store"
)

// Bot config keys controlling the high-water ratchet model.
//
//   - riskHwUseMicroPrice ("true"/"false"): when truthy, the high-water mark is
//     ratcheted using the depth-weighted micro-price instead of max(bid, ask).
//     Default false preserves legacy behaviour for operators that have not
//     opted in.
//
//   - riskHwMinDepthUsd (float, USD): when > 0, the high-water ratchet is
//     gated by minimum top-of-book USD on at least one side. A single-share
//     "$0.50 ask flicker" cannot inflate HW past the next real liquidity.
//     Default 0 disables the depth filter.
const (
	botKeyHwUseMicroPrice = "riskHwUseMicroPrice"
	botKeyHwMinDepthUsd   = "riskHwMinDepthUsd"
)

// microPriceCents returns the depth-weighted blend of best bid and best ask:
//
//	microPrice = (bid * askSize + ask * bidSize) / (bidSize + askSize)
//
// Inputs are cent-prices and USD sizes. When one side is missing (size <= 0),
// the function falls back to the available side. Returns 0 when both sides
// are absent.
//
// Why this formula: the side with HEAVIER depth pulls the mark toward the
// OTHER side's price. Intuitively, if there is a wall of bids ready to lift
// the ask, the executable price for a market sell is closer to the ask and
// vice versa — i.e. the mark is pulled toward where the next trade is most
// likely to happen, not the geometric midpoint that ignores order flow.
func microPriceCents(bidCents, askCents, bidSizeUSD, askSizeUSD float64) float64 {
	bidValid := bidCents > 0 && isFiniteValue(bidCents) && bidSizeUSD > 0
	askValid := askCents > 0 && isFiniteValue(askCents) && askSizeUSD > 0
	switch {
	case bidValid && askValid:
		return (bidCents*askSizeUSD + askCents*bidSizeUSD) / (bidSizeUSD + askSizeUSD)
	case bidValid:
		return bidCents
	case askValid:
		return askCents
	default:
		return 0
	}
}

func isFiniteValue(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

// topOfBookSizesUSD returns the size in USD at best bid / best ask, or zero
// when the cache has no entry. Used by the ratchet to decide whether the top
// is "real" enough to push HW.
func (s *Service) topOfBookSizesUSD(tokenID string) (bidSizeUSD, askSizeUSD float64) {
	if s == nil || s.cache == nil {
		return 0, 0
	}
	tid := store.NormalizeRiskCLOBTokenID(tokenID)
	if tid == "" {
		return 0, 0
	}
	bids, asks := s.cache.GetBidsAsks(tid, 1)
	if len(bids) > 0 {
		bidSizeUSD = bids[0].Size
	}
	if len(asks) > 0 {
		askSizeUSD = asks[0].Size
	}
	return bidSizeUSD, askSizeUSD
}

// ratchetMarkCents picks the HW ratchet reference based on bot config:
//
//   - When riskHwUseMicroPrice is truthy AND both sides have positive depth,
//     use the depth-weighted micro-price (which never exceeds max(bid,ask)
//     and is robust to single-side thin tops).
//   - Otherwise fall back to max(bid, ask) for backwards compatibility.
//
// Independently, when riskHwMinDepthUsd > 0, the ratchet is suppressed when
// neither top-of-book level meets the depth threshold; the function returns
// (mark=0, allowed=false) so the caller does NOT advance the HW.
//
// allowed=false means "do not ratchet HW", but the trail still uses the
// previous HW; existing positions are not deleveraged by depth quality alone.
func (s *Service) ratchetMarkCents(ctx context.Context, tokenID string, bidCents, askCents float64) (mark float64, allowed bool) {
	useMicro := isBotConfigTruthy(ctx, s.st, botKeyHwUseMicroPrice)
	minDepthUSD := s.st.GetBotConfigFloat(ctx, botKeyHwMinDepthUsd, 0)
	if minDepthUSD < 0 {
		minDepthUSD = 0
	}

	if !useMicro && minDepthUSD <= 0 {
		// Legacy path: max(bid, ask). Always allowed.
		return maxCentsRatchet(bidCents, askCents), true
	}

	bidSizeUSD, askSizeUSD := s.topOfBookSizesUSD(tokenID)

	// Depth gate: at least one side must meet the threshold. We accept either
	// side because the high-water mark is tracking the QUOTE range, not the
	// fillable side; both walls of liquidity are evidence of a real top.
	if minDepthUSD > 0 {
		eligible := false
		if bidSizeUSD >= minDepthUSD || askSizeUSD >= minDepthUSD {
			eligible = true
		}
		if !eligible {
			if s.log != nil {
				s.log.WithFields(logx.Pairs(
					"token_id", tokenID, "best_bid_cents", bidCents, "best_ask_cents", askCents,
					"bid_size_usd", bidSizeUSD, "ask_size_usd", askSizeUSD, "min_depth_usd", minDepthUSD,
				)).Debug("风控：HW 棘轮被深度门控压制（top-of-book 太薄）")
			}
			return 0, false
		}
	}

	if useMicro {
		mp := microPriceCents(bidCents, askCents, bidSizeUSD, askSizeUSD)
		if mp > 0 {
			return mp, true
		}
		// Micro-price requested but unavailable (no depth on either side):
		// fall back to legacy max(bid,ask) so the ratchet still functions.
	}
	return maxCentsRatchet(bidCents, askCents), true
}

