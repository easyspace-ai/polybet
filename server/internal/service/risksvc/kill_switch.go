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
	RealizedUSD      float64 // sum of realized_pnl_usd for positions closed in the window
	TotalPnLUSD      float64 // unrealized + realized; this is what trips the kill switch
	OpenPositions    int
	BookCovered      int     // positions with a usable bid for MtM
	BookMissing      int     // positions where we couldn't price (skipped)
	WorstPositionUSD float64 // most negative single-position PnL among open (informational)
	WindowSec        int     // realized-PnL window in seconds (24h by default)
	Tripped          bool
	Reason           string
}

// EvaluateKillSwitch computes mark-to-market unrealized PnL on currently-open
// risk positions PLUS realized PnL on positions closed within the rolling
// window, and flips the auto-halt flag when total loss exceeds the
// configured threshold.
//
// Design notes:
//   - Unrealized: bids are sourced from BestBidAskCents which checks the WS
//     cache first and falls back to REST. When neither source has a bid, the
//     position is skipped (NOT counted as zero PnL): a token with no buyers
//     is the most critical case but we lack a reliable price → conservative
//     skip avoids false-positive halts. Skipped count is surfaced for
//     monitoring.
//   - Realized: positions whose realized_pnl_usd is NULL (legacy / dust /
//     ghost-balance closures) are excluded so missing data does not poison
//     the aggregate. Operators relying on dust-heavy closures should track
//     those out-of-band.
//   - Window: configurable via riskKillSwitchWindowSec (default 86400 = 24h).
//     Setting it to 0 effectively disables realized PnL contribution
//     (no closed-position rows match a future cutoff).
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
		return out, nil
	}
	windowSec := s.st.GetBotConfigInt(ctx, "riskKillSwitchWindowSec", 86400)
	if windowSec < 0 {
		windowSec = 0
	}
	out.WindowSec = windowSec

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

	// Realized PnL across positions closed inside the window. Only rows
	// with explicit realized_pnl_usd are counted (NULL = unknown fill).
	if windowSec > 0 {
		since := time.Now().UTC().Add(-time.Duration(windowSec) * time.Second)
		realized, rerr := s.st.AccountRealizedPnLSince(ctx, acct.ID, since)
		if rerr != nil && s.log != nil {
			s.log.WithFields(logx.Pairs("err", rerr.Error())).Warn("风控：读取已实现 PnL 失败，KILL SWITCH 仅按未实现评估")
		} else {
			out.RealizedUSD = realized
		}
	}
	out.TotalPnLUSD = out.UnrealizedUSD + out.RealizedUSD

	// Auto-trip when total loss is more negative than -threshold.
	if out.TotalPnLUSD <= -threshold {
		out.Tripped = true
		out.Reason = fmt.Sprintf("total=%.2f (unrealized=%.2f realized=%.2f, threshold=%.2f, open=%d, priced=%d, missing=%d, window=%ds)",
			out.TotalPnLUSD, out.UnrealizedUSD, out.RealizedUSD, threshold,
			out.OpenPositions, out.BookCovered, out.BookMissing, windowSec)
		s.SetAutoHalted(true, out.Reason)
		if s.log != nil {
			fields := logx.Pairs(
				"total_pnl_usd", out.TotalPnLUSD, "unrealized_usd", out.UnrealizedUSD,
				"realized_usd", out.RealizedUSD, "threshold_usd", threshold,
				"open_positions", out.OpenPositions, "book_covered", out.BookCovered,
				"book_missing", out.BookMissing, "worst_position_usd", out.WorstPositionUSD,
				"window_sec", windowSec,
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
