package risksvc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/easyspace-ai/polybet/internal/logx"
)

// killSwitchSnapshot is the input/output of one EvaluateKillSwitch run.
// Exposed for tests and dashboard observability.
type killSwitchSnapshot struct {
	ThresholdUSD     float64 // configured threshold (positive USD); 0 disables
	UnrealizedUSD    float64 // sum of mark-to-market for open positions (negative = loss)
	OpenPositions    int
	BookCovered      int     // positions with a usable bid for MtM
	BookMissing      int     // positions where we couldn't price (skipped)
	WorstPositionUSD float64 // most negative single-position PnL (informational)
	Tripped          bool
	Reason           string
}

// EvaluateKillSwitch computes mark-to-market unrealized PnL on currently-open
// risk positions and flips the auto-halt flag when loss exceeds the configured
// threshold.
//
// Design notes:
//   - Realized PnL (closed positions, partial fills) is NOT included in v1.
//     SELL prices are not yet stored in the local trades table, and counting
//     closures-without-fill-price as "worst case = -cost" would over-halt.
//     Adding realized PnL is on the P1 list (separate trade_quality table).
//   - Bids are sourced from BestBidAskCents which checks the WS cache first
//     and falls back to REST. When neither source has a bid, the position is
//     skipped (NOT counted as zero PnL): a token with no buyers is the most
//     critical case but we lack a reliable price → conservative skip avoids
//     false-positive halts. Skipped count is surfaced for monitoring.
//   - Once tripped, the auto-halt flag does NOT auto-clear. Operators must
//     explicitly clear it (set bot config riskTradingHalted=false plus call
//     ClearAutoHalt). This avoids flapping if PnL oscillates around the bar.
func (s *Service) EvaluateKillSwitch(ctx context.Context) (killSwitchSnapshot, error) {
	out := killSwitchSnapshot{}
	if s == nil || s.st == nil {
		return out, nil
	}
	threshold := s.st.GetBotConfigFloat(ctx, botKeyMaxDailyLossUSD, 0)
	out.ThresholdUSD = threshold
	if threshold <= 0 {
		// Disabled — but if we previously auto-halted, leave the flag alone.
		// Operator can clear by setting riskTradingHalted=false explicitly.
		return out, nil
	}
	if halted, _ := s.AutoHaltStatus(); halted {
		// Already halted; don't re-publish the trip event. Still compute the
		// number for visibility.
		// (Falls through so callers can read the snapshot.)
	}
	acct, err := s.st.GetActivePolymarketAccount(ctx)
	if err != nil || acct == nil {
		return out, err
	}
	min := s.minShares(ctx)
	rows, err := s.st.ListOpenRiskPositionsMinShares(ctx, min, acct.ID)
	if err != nil {
		return out, err
	}
	out.OpenPositions = len(rows)

	for _, p := range rows {
		bid, _, ok := s.BestBidAskCents(ctx, p.TokenID)
		if !ok || bid <= 0 {
			out.BookMissing++
			continue
		}
		out.BookCovered++
		mark := bid / 100.0
		pnl := p.SizeShares*mark - p.CostUSD
		out.UnrealizedUSD += pnl
		if pnl < out.WorstPositionUSD {
			out.WorstPositionUSD = pnl
		}
	}

	// Auto-trip when unrealized loss is more negative than -threshold.
	if out.UnrealizedUSD <= -threshold {
		out.Tripped = true
		out.Reason = fmt.Sprintf("unrealized=%.2f USD (threshold=%.2f, open=%d, priced=%d, missing=%d)",
			out.UnrealizedUSD, threshold, out.OpenPositions, out.BookCovered, out.BookMissing)
		s.SetAutoHalted(true, out.Reason)
		if s.log != nil {
			fields := logx.Pairs(
				"unrealized_usd", out.UnrealizedUSD, "threshold_usd", threshold,
				"open_positions", out.OpenPositions, "book_covered", out.BookCovered,
				"book_missing", out.BookMissing, "worst_position_usd", out.WorstPositionUSD,
			)
			s.log.WithFields(fields).Warn("风控：KILL SWITCH 自动触发（已超过单日亏损阈值）")
			logx.StopLoss().WithFields(fields).Warn("风控：KILL SWITCH 自动触发（已超过单日亏损阈值）")
		}
	}
	return out, nil
}

// ClearAutoHalt removes the auto-halt flag. Manual halt (bot_config
// riskTradingHalted=true) is unaffected and still gates new opens.
func (s *Service) ClearAutoHalt(ctx context.Context, who string) {
	who = strings.TrimSpace(who)
	s.SetAutoHalted(false, "")
	if s.log != nil && who != "" {
		s.log.WithFields(logx.Pairs("who", who, "at", time.Now().UTC().Format(time.RFC3339))).Info("风控：自动 KILL SWITCH 已被手动清除")
	}
}
