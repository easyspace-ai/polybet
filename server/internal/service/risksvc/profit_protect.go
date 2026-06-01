package risksvc

import (
	"context"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/easyspace-ai/polybet/internal/logx"
	"github.com/easyspace-ai/polybet/internal/store"
	"github.com/easyspace-ai/polybet/internal/storage/badgerdb"
)

const (
	botKeyProfitProtectEnabled     = "profitProtectEnabled"
	botKeyProfitProtectMode        = "profitProtectMode"
	botKeyProfitProtectArmPct      = "profitProtectArmPct"
	botKeyProfitProtectDrawdownPct = "profitProtectDrawdownPct"
	botKeyProfitProtectArmCents    = "profitProtectArmCents"
	botKeyProfitProtectStopCents   = "profitProtectStopCents"
)

type profitProtectEffective struct {
	mode          string
	armPct        float64
	drawdownPct   float64
	armCents      float64
	stopCents     float64
	custom        bool
}

func (s *Service) profitProtectEnabled(ctx context.Context) bool {
	v, ok, _ := s.st.GetBotConfig(ctx, botKeyProfitProtectEnabled)
	if !ok {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func (s *Service) profitProtectEnabledForPosition(ctx context.Context, p store.RiskPosition) bool {
	if p.ProfitProtectUseEnableOverride {
		return p.ProfitProtectEnableOverride
	}
	// Legacy: custom thresholds implied enabled (before per-position enable override).
	if p.ProfitProtectCustom {
		return true
	}
	return s.profitProtectEnabled(ctx)
}

func (s *Service) profitProtectMode(ctx context.Context) string {
	v, ok, _ := s.st.GetBotConfig(ctx, botKeyProfitProtectMode)
	if !ok {
		return "pct"
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "cents", "cent", "price":
		return "cents"
	default:
		return "pct"
	}
}

func (s *Service) profitProtectArmPct(ctx context.Context) float64 {
	v := s.st.GetBotConfigFloat(ctx, botKeyProfitProtectArmPct, 30)
	if v < 0 {
		return 0
	}
	return v
}

func (s *Service) profitProtectDrawdownPct(ctx context.Context) float64 {
	v := s.st.GetBotConfigFloat(ctx, botKeyProfitProtectDrawdownPct, 10)
	if v <= 0 {
		return 10
	}
	if v > 100 {
		return 100
	}
	return v
}

func (s *Service) profitProtectArmCents(ctx context.Context) float64 {
	v := s.st.GetBotConfigFloat(ctx, botKeyProfitProtectArmCents, 95)
	if v <= 0 {
		return 95
	}
	if v > 100 {
		return 100
	}
	return FloorCents1(v)
}

func (s *Service) profitProtectStopCents(ctx context.Context) float64 {
	v := s.st.GetBotConfigFloat(ctx, botKeyProfitProtectStopCents, 85)
	if v <= 0 {
		return 85
	}
	if v > 100 {
		return 100
	}
	return FloorCents1(v)
}

func (s *Service) resolveProfitProtect(ctx context.Context, p store.RiskPosition) profitProtectEffective {
	e := profitProtectEffective{
		mode:        s.profitProtectMode(ctx),
		armPct:      s.profitProtectArmPct(ctx),
		drawdownPct: s.profitProtectDrawdownPct(ctx),
		armCents:    s.profitProtectArmCents(ctx),
		stopCents:   s.profitProtectStopCents(ctx),
	}
	if p.ProfitProtectCustom {
		e.custom = true
		if p.ProfitProtectArmPctOverride > 0 {
			e.armPct = p.ProfitProtectArmPctOverride
		}
		if p.ProfitProtectDrawdownOverride > 0 {
			e.drawdownPct = p.ProfitProtectDrawdownOverride
		}
		if p.ProfitProtectArmCentsOverride > 0 {
			e.armCents = FloorCents1(p.ProfitProtectArmCentsOverride)
		}
		if p.ProfitProtectStopCentsOverride > 0 {
			e.stopCents = FloorCents1(p.ProfitProtectStopCentsOverride)
		}
	}
	return e
}

// profitPctFromMark returns unrealized return % vs cost basis (matches dashboard pnlUsd / costUsd).
func profitPctFromMark(costUsd, sizeShares, markCents float64) (float64, bool) {
	if costUsd <= 0 || sizeShares <= 0 || markCents <= 0 {
		return 0, false
	}
	markUsd := sizeShares * markCents / 100
	profitUsd := markUsd - costUsd
	return profitUsd / costUsd * 100, true
}

func profitProtectMarkCents(bidCents, askCents float64) float64 {
	mark := bidCents
	if mark <= 0 && askCents > 0 {
		mark = askCents
	}
	return FloorCents1(mark)
}

func (s *Service) persistProfitProtectState(ctx context.Context, id string, armed bool, peakPct, peakMark float64) error {
	return s.st.UpdateRiskPositionProfitProtectState(ctx, id, armed, peakPct, peakMark)
}

func (s *Service) evaluateProfitProtectPct(ctx context.Context, p store.RiskPosition, mark float64, cfg profitProtectEffective) error {
	profitPct, ok := profitPctFromMark(p.CostUSD, p.SizeShares, mark)
	if !ok {
		return nil
	}
	armed := p.ProfitProtectArmed
	peak := p.PeakProfitPct

	if !armed && profitPct >= cfg.armPct {
		armed = true
		peak = profitPct
		if err := s.persistProfitProtectState(ctx, p.ID, armed, peak, p.PeakMarkCents); err != nil {
			return err
		}
	} else if armed {
		if profitPct > peak {
			peak = profitPct
			if err := s.persistProfitProtectState(ctx, p.ID, armed, peak, p.PeakMarkCents); err != nil {
				return err
			}
		}
		triggerFloor := peak * (1 - cfg.drawdownPct/100)
		if peak > 0 && profitPct <= triggerFloor {
			return s.triggerProfitProtectClose(ctx, p, map[string]any{
				"mode": "pct", "profitPct": profitPct, "peakProfitPct": peak, "drawdownPct": cfg.drawdownPct,
			}, logx.Pairs(
				"position_id", p.ID, "profit_pct", profitPct, "peak_profit_pct", peak,
				"drawdown_pct", cfg.drawdownPct, "trigger_floor_pct", triggerFloor,
			))
		}
	}
	return nil
}

func (s *Service) evaluateProfitProtectCents(ctx context.Context, p store.RiskPosition, mark float64, cfg profitProtectEffective) error {
	if mark <= 0 || cfg.armCents <= 0 || cfg.stopCents <= 0 || cfg.stopCents >= cfg.armCents {
		return nil
	}
	armed := p.ProfitProtectArmed
	peakMark := p.PeakMarkCents

	if !armed && mark >= cfg.armCents {
		armed = true
		peakMark = mark
		if err := s.persistProfitProtectState(ctx, p.ID, armed, p.PeakProfitPct, peakMark); err != nil {
			return err
		}
	} else if armed && mark <= cfg.stopCents {
		return s.triggerProfitProtectClose(ctx, p, map[string]any{
			"mode": "cents", "markCents": mark, "stopCents": cfg.stopCents, "armCents": cfg.armCents,
		}, logx.Pairs(
			"position_id", p.ID, "mark_cents", mark, "stop_cents", cfg.stopCents, "arm_cents", cfg.armCents,
		))
	}
	return nil
}

func (s *Service) triggerProfitProtectClose(ctx context.Context, p store.RiskPosition, rtPayload map[string]any, fields logrus.Fields) error {
	if err := s.ensureCloseTask(ctx, p.ID, "profit_protect"); err != nil {
		return err
	}
	if s.log != nil {
		s.log.WithFields(fields).Info("风控：收益保护触发，已排队止盈")
		logx.StopLoss().WithFields(fields).Info("风控：收益保护触发，已排队止盈")
	}
	if s.rt != nil {
		s.rt.Publish("position", "warn", "position.profit_protect_triggered", p.AccountID, "", p.TokenID, p.ID, rtPayload)
	}
	return nil
}

// EvaluateProfitProtect arms trailing profit protection and queues take-profit on drawdown.
func (s *Service) EvaluateProfitProtect(ctx context.Context, p store.RiskPosition, bidCents, askCents float64) error {
	if !s.profitProtectEnabledForPosition(ctx, p) {
		return nil
	}
	if p.Status != "open" || p.SizeShares < s.minShares(ctx) {
		return nil
	}
	mark := profitProtectMarkCents(bidCents, askCents)
	cfg := s.resolveProfitProtect(ctx, p)
	if cfg.mode == "cents" {
		return s.evaluateProfitProtectCents(ctx, p, mark, cfg)
	}
	return s.evaluateProfitProtectPct(ctx, p, mark, cfg)
}

// ProfitProtectDisplayFields builds read-only UI fields for monitor/risk lists.
func (s *Service) ProfitProtectDisplayFields(ctx context.Context, p store.RiskPosition, pnl *float64, curPtr *float64) map[string]any {
	return profitProtectDisplayFields(ctx, s, p, pnl, curPtr)
}

// profitProtectDisplayFields builds read-only UI fields for monitor/risk lists.
func profitProtectDisplayFields(ctx context.Context, s *Service, p store.RiskPosition, pnl *float64, curPtr *float64) map[string]any {
	cfg := s.resolveProfitProtect(ctx, p)
	globalOn := s.profitProtectEnabled(ctx)
	effectiveOn := s.profitProtectEnabledForPosition(ctx, p)
	out := map[string]any{
		"profitProtectEnabled":           globalOn,
		"profitProtectEnabledEffective":  effectiveOn,
		"profitProtectUseEnableOverride": p.ProfitProtectUseEnableOverride,
		"profitProtectEnableOverride":    p.ProfitProtectEnableOverride,
		"profitProtectMode":              cfg.mode,
		"profitProtectCustom":            cfg.custom,
		"profitProtectArmed":   p.ProfitProtectArmed,
		"peakProfitPct":        p.PeakProfitPct,
		"peakMarkCents":        p.PeakMarkCents,
	}
	if cfg.mode == "cents" {
		out["profitProtectArmCents"] = cfg.armCents
		out["profitProtectStopCents"] = cfg.stopCents
		if p.ProfitProtectCustom {
			out["profitProtectArmCentsOverride"] = p.ProfitProtectArmCentsOverride
			out["profitProtectStopCentsOverride"] = p.ProfitProtectStopCentsOverride
		}
		if p.ProfitProtectArmed {
			out["profitProtectTriggerCents"] = cfg.stopCents
		} else if curPtr != nil && *curPtr >= cfg.armCents {
			out["profitProtectTriggerCents"] = cfg.stopCents
		}
	} else {
		out["profitProtectArmPct"] = cfg.armPct
		out["profitProtectDrawdownPct"] = cfg.drawdownPct
		if p.ProfitProtectCustom {
			out["profitProtectArmPctOverride"] = p.ProfitProtectArmPctOverride
			out["profitProtectDrawdownOverride"] = p.ProfitProtectDrawdownOverride
		}
		if p.ProfitProtectArmed && p.PeakProfitPct > 0 {
			floor := p.PeakProfitPct * (1 - cfg.drawdownPct/100)
			out["profitProtectTriggerPct"] = floor
		}
	}
	var profitPct *float64
	if pnl != nil && p.CostUSD > 0 {
		v := *pnl / p.CostUSD * 100
		profitPct = &v
	} else if curPtr != nil {
		if v, ok := profitPctFromMark(p.CostUSD, p.SizeShares, *curPtr); ok {
			profitPct = &v
		}
	}
	if profitPct != nil {
		out["profitPct"] = *profitPct
	}
	if cfg.mode == "cents" && curPtr != nil && cfg.armCents > 0 && *curPtr >= cfg.armCents {
		out["profitProtectEffectiveArmed"] = true
	} else {
		out["profitProtectEffectiveArmed"] = p.ProfitProtectArmed
	}
	if !effectiveOn {
		out["profitProtectEffectiveArmed"] = false
	}
	return out
}

// ValidateProfitProtectSettingsPatch validates monitor PATCH values for the active global mode.
func ValidateProfitProtectSettingsPatch(ctx context.Context, st *store.Store, patch badgerdb.ProfitProtectSettingsPatch) error {
	mode := "pct"
	if st != nil {
		v, ok, _ := st.GetBotConfig(ctx, botKeyProfitProtectMode)
		if ok && strings.EqualFold(strings.TrimSpace(v), "cents") {
			mode = "cents"
		}
	}
	if patch.ArmCents != nil || patch.StopCents != nil {
		if mode != "cents" {
			return store.ErrProfitProtectWrongMode
		}
		arm := 95.0
		stop := 85.0
		if patch.ArmCents != nil {
			arm = FloorCents1(*patch.ArmCents)
		}
		if patch.StopCents != nil {
			stop = FloorCents1(*patch.StopCents)
		}
		if arm <= 0 || arm > 100 || stop <= 0 || stop > 100 || stop >= arm {
			return store.ErrProfitProtectInvalidCents
		}
	}
	if patch.ArmPct != nil || patch.DrawdownPct != nil {
		if mode != "pct" {
			return store.ErrProfitProtectWrongMode
		}
		if patch.ArmPct != nil && *patch.ArmPct < 0 {
			return store.ErrProfitProtectInvalidPct
		}
		if patch.DrawdownPct != nil && (*patch.DrawdownPct <= 0 || *patch.DrawdownPct > 100) {
			return store.ErrProfitProtectInvalidPct
		}
	}
	return nil
}
