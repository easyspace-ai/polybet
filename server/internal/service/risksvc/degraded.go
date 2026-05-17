package risksvc

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/easyspace-ai/polybet/internal/logx"
	"github.com/easyspace-ai/polybet/internal/store"
)

// Bot config keys for kill-switch and gating.
const (
	// botKeyTradingHalted, when truthy, blocks all new opens via /api/trade.
	// Risk close (stop_loss, manual close, close_all) continues to operate.
	botKeyTradingHalted = "riskTradingHalted"

	// botKeyMaxDailyLossUSD is the realized + unrealized P&L floor (negative
	// is loss). When configured > 0 and current daily PnL drops below
	// -value, the kill switch flips to halted automatically. <= 0 disables.
	botKeyMaxDailyLossUSD = "riskMaxDailyLossUSD"

	// botKeyMaxOpenPositions caps the open-position count the trade gate
	// allows. <= 0 disables.
	botKeyMaxOpenPositions = "riskMaxOpenPositions"

	// botKeyBookMaxAgeMs is the upper bound on book staleness (cache age in
	// ms) accepted by the router gate. <= 0 disables.
	botKeyBookMaxAgeMs = "riskBookMaxAgeMs"

	// botKeyMaxReconcileGapSec is the upper bound on time since the last
	// successful market WS message before the trade gate refuses opens.
	// <= 0 disables.
	botKeyMaxReconcileGapSec = "riskMaxReconcileGapSec"
)

// degradedReason captures the most recent self-degradation cause.
type degradedReason struct {
	source  string // "ws_market" | "ws_user" | "auto_halt" | "manual"
	message string
	at      time.Time
}

// degradedState holds runtime gating signals that do not belong in bot config
// (transient: WS health, last book tick).
type degradedState struct {
	mu              sync.RWMutex
	wsMarketDown    bool   // true when market WS reached fatal state
	wsMarketReason  string // last error surface from MarketStream
	manualHalted    atomic.Bool
	autoHalted      atomic.Bool
	autoHaltReason  string
	autoHaltMu      sync.Mutex
	lastBookTickMs  atomic.Int64
	lastReason      degradedReason
}

func newDegradedState() *degradedState { return &degradedState{} }

// SetWSMarketDown is called by the engine when the upstream market WS reaches a
// fatal state (max retries exhausted, repeated dial failure, etc.).
func (s *Service) SetWSMarketDown(reason string) {
	s.deg.mu.Lock()
	s.deg.wsMarketDown = true
	s.deg.wsMarketReason = strings.TrimSpace(reason)
	s.deg.lastReason = degradedReason{source: "ws_market", message: reason, at: time.Now().UTC()}
	s.deg.mu.Unlock()
	if s.log != nil {
		s.log.WithFields(logx.Pairs("source", "ws_market", "reason", reason)).Warn("风控：行情链路降级（标记为 degraded）")
	}
	if s.rt != nil {
		s.rt.Publish("transport", "warn", "ws.market.degraded", "", "", "", "", map[string]any{"reason": reason})
	}
}

// ClearWSMarketDown is called when the engine has restarted the upstream stream
// and at least one tick has flowed.
func (s *Service) ClearWSMarketDown() {
	s.deg.mu.Lock()
	wasDown := s.deg.wsMarketDown
	s.deg.wsMarketDown = false
	s.deg.wsMarketReason = ""
	s.deg.mu.Unlock()
	if wasDown && s.log != nil {
		s.log.Info("风控：行情链路已恢复（degraded 已清除）")
	}
	if wasDown && s.rt != nil {
		s.rt.Publish("transport", "info", "ws.market.recovered", "", "", "", "", nil)
	}
}

// MarkBookTick records that we just received a fresh market book tick.
// Used by the trade gate to require non-stale market data when opening.
func (s *Service) MarkBookTick() {
	s.deg.lastBookTickMs.Store(time.Now().UnixMilli())
}

// LastBookTickAt returns the wall time of the last MarkBookTick (zero if none).
func (s *Service) LastBookTickAt() time.Time {
	ms := s.deg.lastBookTickMs.Load()
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

// WSMarketDown reports the cached "fatal upstream WS" signal.
func (s *Service) WSMarketDown() (bool, string) {
	s.deg.mu.RLock()
	defer s.deg.mu.RUnlock()
	return s.deg.wsMarketDown, s.deg.wsMarketReason
}

// SetAutoHalted flips the auto-halt flag (e.g. daily loss kill switch).
// Manual halt set via bot config takes precedence when truthy.
func (s *Service) SetAutoHalted(halted bool, reason string) {
	s.deg.autoHaltMu.Lock()
	defer s.deg.autoHaltMu.Unlock()
	prev := s.deg.autoHalted.Load()
	s.deg.autoHalted.Store(halted)
	s.deg.autoHaltReason = strings.TrimSpace(reason)
	if !prev && halted {
		if s.log != nil {
			s.log.WithFields(logx.Pairs("reason", reason)).Warn("风控：自动 KILL SWITCH 已触发，开仓将被阻止")
		}
		if s.rt != nil {
			s.rt.Publish("position", "warn", "risk.kill_switch_tripped", "", "", "", "", map[string]any{"reason": reason})
		}
	}
	if prev && !halted {
		if s.log != nil {
			s.log.Info("风控：自动 KILL SWITCH 已解除")
		}
		if s.rt != nil {
			s.rt.Publish("position", "info", "risk.kill_switch_cleared", "", "", "", "", nil)
		}
	}
}

// AutoHaltStatus returns whether the auto-halt flag is set and the recorded reason.
func (s *Service) AutoHaltStatus() (bool, string) {
	s.deg.autoHaltMu.Lock()
	defer s.deg.autoHaltMu.Unlock()
	return s.deg.autoHalted.Load(), s.deg.autoHaltReason
}

// TradeGateError captures a structured rejection from EnsureTradeAllowed.
type TradeGateError struct {
	Code    string // machine-readable: trading_halted | kill_switch_tripped | book_stale | ws_market_down | too_many_open_positions
	Message string
	Detail  map[string]any
}

func (e *TradeGateError) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Message
}

// EnsureTradeAllowed inspects manual halt, auto-halt, market WS health, and
// per-token book freshness. tokenID may be empty when the caller only wants
// account-level gating; non-empty enables stale-book protection.
//
// Returns a *TradeGateError when the trade should be refused (and a human
// reason). Returns nil when allowed.
func (s *Service) EnsureTradeAllowed(ctx context.Context, tokenID string) *TradeGateError {
	if s == nil || s.st == nil {
		return nil
	}

	// Manual halt has highest priority.
	if isBotConfigTruthy(ctx, s.st, botKeyTradingHalted) {
		return &TradeGateError{
			Code:    "trading_halted",
			Message: "trading is halted by bot_config riskTradingHalted",
		}
	}

	if halted, reason := s.AutoHaltStatus(); halted {
		return &TradeGateError{
			Code:    "kill_switch_tripped",
			Message: "auto kill switch tripped: " + reason,
			Detail:  map[string]any{"reason": reason},
		}
	}

	// Per-account open-position cap.
	if cap := s.st.GetBotConfigInt(ctx, botKeyMaxOpenPositions, 0); cap > 0 {
		acct, err := s.st.GetActivePolymarketAccount(ctx)
		if err == nil && acct != nil {
			min := s.minShares(ctx)
			n, cerr := s.st.CountOpenRiskPositionsMinShares(ctx, min, acct.ID)
			if cerr == nil && int(n) >= cap {
				return &TradeGateError{
					Code:    "too_many_open_positions",
					Message: "open position count exceeds riskMaxOpenPositions",
					Detail:  map[string]any{"open": n, "cap": cap},
				}
			}
		}
	}

	// Upstream market WS health.
	if down, reason := s.WSMarketDown(); down {
		return &TradeGateError{
			Code:    "ws_market_down",
			Message: "market data feed degraded: " + reason,
			Detail:  map[string]any{"reason": reason},
		}
	}
	if gapSec := s.st.GetBotConfigInt(ctx, botKeyMaxReconcileGapSec, 0); gapSec > 0 {
		last := s.LastBookTickAt()
		if !last.IsZero() && time.Since(last) > time.Duration(gapSec)*time.Second {
			return &TradeGateError{
				Code:    "ws_market_stale",
				Message: "market WS has not delivered a tick within riskMaxReconcileGapSec",
				Detail:  map[string]any{"lastTickAt": last.Format(time.RFC3339), "maxGapSec": gapSec},
			}
		}
	}

	// Per-token book staleness (only when caller knows the token).
	if maxAgeMs := s.st.GetBotConfigInt(ctx, botKeyBookMaxAgeMs, 0); maxAgeMs > 0 && strings.TrimSpace(tokenID) != "" {
		tid := store.NormalizeRiskCLOBTokenID(tokenID)
		if tid != "" && s.cache != nil {
			age, ok := s.cache.BookAge(tid)
			if !ok {
				return &TradeGateError{
					Code:    "book_unavailable",
					Message: "no cached book for token; refusing trade until WS or REST refresh succeeds",
					Detail:  map[string]any{"tokenId": tid},
				}
			}
			if age.Milliseconds() > int64(maxAgeMs) {
				return &TradeGateError{
					Code:    "book_stale",
					Message: "cached book older than riskBookMaxAgeMs",
					Detail:  map[string]any{"tokenId": tid, "ageMs": age.Milliseconds(), "maxAgeMs": maxAgeMs},
				}
			}
		}
	}
	return nil
}

func isBotConfigTruthy(ctx context.Context, st *store.Store, key string) bool {
	v, ok, err := st.GetBotConfig(ctx, key)
	if err != nil || !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
