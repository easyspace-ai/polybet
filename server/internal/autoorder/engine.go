package autoorder

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/easyspace-ai/polybet/internal/bookcache"
	"github.com/easyspace-ai/polybet/internal/config"
	"github.com/easyspace-ai/polybet/internal/logx"
	"github.com/easyspace-ai/polybet/internal/memcache"
	"github.com/easyspace-ai/polybet/internal/service/balancesvc"
	"github.com/easyspace-ai/polybet/internal/service/risksvc"
	"github.com/easyspace-ai/polybet/internal/service/routersvc"
	"github.com/easyspace-ai/polybet/internal/service/tradesvc"
	"github.com/easyspace-ai/polybet/internal/storage"
	"github.com/easyspace-ai/polybet/internal/store"
	"github.com/easyspace-ai/polybet/internal/tg"
)

// Engine runs periodic auto-order evaluation and optional execution.
type Engine struct {
	cfg          *config.Config
	st           *storage.Backend
	cache        *bookcache.Cache
	risk         *risksvc.Service
	balanceCache *memcache.BalanceCache
	log          *logrus.Logger
}

// NewEngine constructs the auto-order scheduler.
func NewEngine(cfg *config.Config, st *storage.Backend, cache *bookcache.Cache, risk *risksvc.Service, balanceCache *memcache.BalanceCache, log *logrus.Logger) *Engine {
	if log == nil {
		log = logrus.StandardLogger()
	}
	return &Engine{cfg: cfg, st: st, cache: cache, risk: risk, balanceCache: balanceCache, log: log}
}

// Tick evaluates all candidates once.
func (e *Engine) Tick(ctx context.Context) {
	if e == nil || e.st == nil {
		return
	}
	cfg, err := LoadConfig(ctx, e.st)
	if err != nil {
		e.log.WithFields(logx.Pairs("err", err.Error())).Warn("自动下单：加载配置失败")
		return
	}
	if !cfg.AnyGroupEnabled() {
		return
	}
	now := time.Now().UTC()
	nyDate := NYDateString(now)
	ledger, err := LedgerForToday(ctx, e.st, now)
	if err != nil {
		e.log.WithFields(logx.Pairs("err", err.Error())).Warn("自动下单：加载账本失败")
		return
	}
	dailyPool, err := e.dailyPoolUSD(ctx, cfg)
	if err != nil {
		e.log.WithFields(logx.Pairs("err", err.Error())).Info("自动下单：跳过（无法计算日池）")
		return
	}

	markets, outcomes, err := e.st.ListActiveMarketsFlat(ctx)
	if err != nil {
		e.log.WithFields(logx.Pairs("err", err.Error())).Warn("自动下单：列举市场失败")
		return
	}
	outByMkt := map[string][]store.OutcomeRow{}
	for _, o := range outcomes {
		outByMkt[o.MarketID] = append(outByMkt[o.MarketID], o)
	}
	candidates := MatchGroups(cfg, markets, outByMkt)
	dryRun := IsDryRun(ctx, e.st)
	readOnly := e.cfg != nil && e.cfg.ReadOnlyMode

	for _, cand := range candidates {
		e.processCandidate(ctx, cfg, cand, now, nyDate, dailyPool, &ledger, dryRun, readOnly)
	}
	if ledger.Date == "" {
		ledger.Date = nyDate
	}
	_ = PersistLedger(ctx, e.st, ledger)
}

func (e *Engine) dailyPoolUSD(ctx context.Context, cfg Config) (float64, error) {
	switch cfg.DailyPool.Mode {
	case "fixed_usd":
		return cfg.DailyPool.Value, nil
	case "percent_balance":
		var bal float64
		if e.balanceCache != nil {
			sum, _, err := e.balanceCache.GetWithRefresh(ctx)
			if err != nil {
				return 0, err
			}
			if sum != nil && sum.Polymarket != nil {
				bal = *sum.Polymarket
			}
		}
		if bal <= 0 {
			sum, err := balancesvc.Fetch(ctx, e.cfg, e.st)
			if err != nil {
				return 0, err
			}
			if sum.Polymarket != nil {
				bal = *sum.Polymarket
			}
		}
		return bal * cfg.DailyPool.Value / 100.0, nil
	default:
		return 0, fmt.Errorf("unknown daily pool mode %q", cfg.DailyPool.Mode)
	}
}

func (e *Engine) processCandidate(ctx context.Context, cfg Config, cand MatchCandidate, now time.Time, nyDate string, dailyPool float64, ledger *Ledger, dryRun, readOnly bool) {
	g := cand.Group
	m := cand.Market
	matchLabel := m.HomeTeam + " vs " + m.AwayTeam

	if ledger.AlreadyExecuted(g.ID, m.EventID, nyDate) {
		e.logSkip(g, m.EventID, matchLabel, "already_executed_today")
		return
	}

	ev := Evaluate(now, cfg, cand, e.cache)
	if !ev.OK {
		e.logSkip(g, m.EventID, matchLabel, ev.SkipReason)
		return
	}

	groupBudget := GroupBudgetForGroup(dailyPool, g)
	remaining := GroupRemainingUSD(groupBudget, ledger.GroupSpentUSD(g.ID))
	stake, ok := StakeFromBands(ev.ImpliedOdds, g.OddsBands, remaining)
	if !ok || stake <= 0 {
		e.logSkip(g, m.EventID, matchLabel, "no_matching_odds_band_or_no_budget")
		return
	}

	maxTrade := e.st.GetBotConfigFloat(ctx, "maxTradeSize", 100)
	if stake > maxTrade {
		stake = maxTrade
	}

	fields := logx.Pairs(
		"group", g.Name, "event_id", m.EventID, "match", matchLabel,
		"outcome_id", ev.Outcome.ID, "odds", ev.ImpliedOdds, "stake_usd", stake,
		"dry_run", dryRun, "read_only", readOnly,
	)
	e.log.WithFields(fields).Info("自动下单：候选通过")

	if readOnly {
		e.log.WithFields(fields).Info("自动下单：跳过（只读模式）")
		_ = RecordAttempt(ctx, e.st, RunRecord{
			GroupID: g.ID, GroupName: g.Name, EventID: m.EventID, Match: matchLabel,
			OutcomeID: ev.Outcome.ID, OutcomeLabel: ev.Outcome.Label, SizeUSD: stake, Odds: ev.ImpliedOdds,
			Status: "skipped", Reason: "read_only_mode",
		})
		return
	}

	if dryRun {
		e.log.WithFields(fields).Info("自动下单：dry-run 模拟成交")
		ledger.RecordSpend(g.ID, m.EventID, nyDate, stake)
		_ = RecordAttempt(ctx, e.st, RunRecord{
			GroupID: g.ID, GroupName: g.Name, EventID: m.EventID, Match: matchLabel,
			OutcomeID: ev.Outcome.ID, OutcomeLabel: ev.Outcome.Label, SizeUSD: stake, Odds: ev.ImpliedOdds,
			Status: "dry_run", Reason: "autoOrderDryRun=true",
		})
		tg.Notify(ctx, e.cfg, e.st, e.log, fmt.Sprintf(
			"Polybet 自动下单 [模拟]\n组 %s · %s · %s\n$%.2f @ %.1f¢",
			g.Name, matchLabel, ev.Outcome.Label, stake, ev.ImpliedOdds*100,
		))
		return
	}

	planRes := routersvc.BuildAllocationPlan(ctx, e.st, e.cache, ev.Outcome.ID, "buy", stake)
	if !planRes.OK {
		reason := "router:" + planRes.Error.Code
		e.log.WithFields(logx.Pairs("group", g.Name, "reason", reason)).Info("自动下单：路由失败")
		_ = RecordAttempt(ctx, e.st, RunRecord{
			GroupID: g.ID, GroupName: g.Name, EventID: m.EventID, Match: matchLabel,
			OutcomeID: ev.Outcome.ID, OutcomeLabel: ev.Outcome.Label, SizeUSD: stake, Odds: ev.ImpliedOdds,
			Status: "failed", Reason: reason,
		})
		tg.Notify(ctx, e.cfg, e.st, e.log, fmt.Sprintf(
			"Polybet 自动下单失败\n组 %s · %s\n路由: %s",
			g.Name, matchLabel, planRes.Error.Message,
		))
		return
	}

	resp, _, err := tradesvc.ExecutePlan(ctx, e.cfg, e.st, e.cache, e.risk, planRes.Plan, "buy")
	if err != nil {
		e.log.WithFields(logx.Pairs("err", err.Error())).Info("自动下单：执行错误")
		_ = RecordAttempt(ctx, e.st, RunRecord{
			GroupID: g.ID, GroupName: g.Name, EventID: m.EventID, Match: matchLabel,
			Status: "failed", Reason: err.Error(),
		})
		return
	}

	filled := false
	var tradeID string
	for _, t := range resp.Trades {
		if t.Status == "filled" {
			filled = true
			tradeID = t.TradeID
			break
		}
	}
	if filled {
		ledger.RecordSpend(g.ID, m.EventID, nyDate, stake)
		_ = RecordAttempt(ctx, e.st, RunRecord{
			GroupID: g.ID, GroupName: g.Name, EventID: m.EventID, Match: matchLabel,
			OutcomeID: ev.Outcome.ID, OutcomeLabel: ev.Outcome.Label, SizeUSD: stake, Odds: ev.ImpliedOdds,
			Status: "filled", TradeID: tradeID,
		})
		e.log.WithFields(logx.Pairs("group", g.Name, "trade_id", tradeID)).Info("自动下单：成交")
		tg.Notify(ctx, e.cfg, e.st, e.log, fmt.Sprintf(
			"Polybet 自动下单成交\n组 %s · %s · %s\n$%.2f · order %s",
			g.Name, matchLabel, ev.Outcome.Label, stake, tradeID,
		))
	} else {
		reason := resp.Message
		if reason == "" {
			reason = resp.Status
		}
		_ = RecordAttempt(ctx, e.st, RunRecord{
			GroupID: g.ID, GroupName: g.Name, EventID: m.EventID, Match: matchLabel,
			OutcomeID: ev.Outcome.ID, OutcomeLabel: ev.Outcome.Label, SizeUSD: stake, Odds: ev.ImpliedOdds,
			Status: "failed", Reason: reason,
		})
		tg.Notify(ctx, e.cfg, e.st, e.log, fmt.Sprintf(
			"Polybet 自动下单失败\n组 %s · %s · $%.2f\n%s",
			g.Name, matchLabel, stake, reason,
		))
	}
}

func (e *Engine) logSkip(g Group, eventID, match, reason string) {
	e.log.WithFields(logx.Pairs("group", g.Name, "event_id", eventID, "match", match, "reason", reason)).Info("自动下单：跳过")
}

// FormatCents for logging.
func FormatCents(odds float64) string {
	return strconv.FormatFloat(odds*100, 'f', 1, 64)
}

// ParseBoolConfig helper for tests.
func ParseBoolConfig(v string) bool {
	v = strings.TrimSpace(strings.ToLower(v))
	return v != "false" && v != "0" && v != ""
}
