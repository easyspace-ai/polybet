package tradesvc

import (
	"context"
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/easyspace-ai/polybet/internal/bookcache"
	"github.com/easyspace-ai/polybet/internal/config"
	"github.com/easyspace-ai/polybet/internal/logx"
	"github.com/easyspace-ai/polybet/internal/polyexec"
	"github.com/easyspace-ai/polybet/internal/polywarm"
	"github.com/easyspace-ai/polybet/internal/service/polysession"
	"github.com/easyspace-ai/polybet/internal/service/risksvc"
	"github.com/easyspace-ai/polybet/internal/service/routersvc"
	"github.com/easyspace-ai/polybet/internal/store"
	"github.com/easyspace-ai/polybet/internal/tg"
)

type TradeResult struct {
	TradeID       string `json:"tradeId"`
	Status        string `json:"status"`
	Platform      string `json:"platform"`
	TxHash        string `json:"txHash,omitempty"`
	FailureReason string `json:"failureReason,omitempty"`
}

type TradeResponse struct {
	Status  string                    `json:"status"`
	Message string                    `json:"message,omitempty"` // first/all leg failure reasons for API consumers
	Trades  []TradeResult             `json:"trades"`
	Plan    *routersvc.AllocationPlan `json:"plan"`
}

// ExecutePlan runs allocations sequentially (Polymarket FOK buy only, matching Node).
func ExecutePlan(ctx context.Context, cfg *config.Config, st *store.Store, cache *bookcache.Cache, risk *risksvc.Service, plan *routersvc.AllocationPlan, side string) (*TradeResponse, int, error) {
	cl, err := polysession.ResolveAuthedCLOB(ctx, cfg, st)
	if err != nil {
		return nil, 0, err
	}
	acct, _ := st.GetActivePolymarketAccount(ctx)
	accountID := ""
	if acct != nil {
		accountID = acct.ID
	}
	buyExtra := st.GetBotConfigInt(ctx, "polymarketFokBuyExtraTicks", 5)
	results := make([]TradeResult, 0, len(plan.Allocations))
	for _, a := range plan.Allocations {
		_, mid, label, _, home, away, err := st.GetOutcomeWithMarket(ctx, a.OutcomeID)
		if err != nil {
			logrus.WithFields(logx.Pairs("outcome_id", a.OutcomeID, "platform", a.Platform, "err", err.Error())).Warn("交易：查询 outcome 失败")
			results = append(results, TradeResult{TradeID: "unknown", Status: "failed", Platform: a.Platform, FailureReason: "outcome_lookup: " + err.Error()})
			continue
		}
		tid, err := st.CreatePendingTrade(ctx, mid, a.OutcomeID, a.Platform, side, a.Size, a.ExpectedOdds, accountID)
		if err != nil {
			logrus.WithFields(logx.Pairs("outcome_id", a.OutcomeID, "market_id", mid, "err", err.Error())).Warn("交易：创建 pending 记录失败")
			results = append(results, TradeResult{TradeID: "unknown", Status: "failed", Platform: a.Platform, FailureReason: "create_trade: " + err.Error()})
			continue
		}
		if a.Platform != "polymarket" {
			_ = st.MarkTradeFailed(ctx, tid, "unsupported_platform")
			logrus.WithFields(logx.Pairs("trade_id", tid, "platform", a.Platform)).Warn("交易：不支持的平台")
			results = append(results, TradeResult{TradeID: tid, Status: "failed", Platform: a.Platform, FailureReason: "unsupported_platform"})
			continue
		}
		logrus.WithFields(logx.Pairs(
			"trade_id", tid, "outcome_id", a.OutcomeID, "token_id", a.ExternalOutcomeID,
			"size_usdc", a.Size, "expected_odds", a.ExpectedOdds, "extra_ticks", buyExtra,
			"match", home+" vs "+away, "label", label,
		)).Info("交易：发送 FOK 买单")
		orderID, fillOdds, err := polyexec.ExecuteFOKBuy(ctx, cl.Client, cl.Signer, a.ExternalOutcomeID, a.Size, a.ExpectedOdds, buyExtra)
		if err != nil {
			reason := err.Error()
			_ = st.MarkTradeFailed(ctx, tid, reason)
			logrus.WithFields(logx.Pairs("trade_id", tid, "outcome_id", a.OutcomeID, "token_id", a.ExternalOutcomeID, "err", reason)).Warn("交易：FOK 买单被拒")
			tg.Notify(ctx, cfg, st, logrus.StandardLogger(), fmt.Sprintf(
				"Polybet 开单失败\n%s vs %s · $%.2f @ 期望 %.1f¢\n%s",
				home, away, a.Size, a.ExpectedOdds*100, reason,
			))
			results = append(results, TradeResult{TradeID: tid, Status: "failed", Platform: a.Platform, FailureReason: reason})
			continue
		}
		_ = st.MarkTradeFilled(ctx, tid, orderID, a.Size, fillOdds)
		logrus.WithFields(logx.Pairs("trade_id", tid, "outcome_id", a.OutcomeID, "order_id", orderID, "fill_odds", fillOdds)).Info("交易：FOK 买单成交")
		tg.Notify(ctx, cfg, st, logrus.StandardLogger(), fmt.Sprintf(
			"Polybet 开单成交\n%s vs %s · %s · $%.2f @ 成交 %.1f¢\norder %s",
			home, away, label, a.Size, fillOdds*100, orderID,
		))
		tg.MaybeNotifyCollateralChanged(cfg, logrus.StandardLogger(), st)
		results = append(results, TradeResult{TradeID: tid, Status: "filled", Platform: a.Platform, TxHash: orderID})
		tok := a.ExternalOutcomeID
		go func() {
			_ = polywarm.RefreshFromREST(context.Background(), cfg.PolymarketAPIURL, cfg.HTTPPlatformProxy, tok, cache)
		}()
		if side == "buy" && tok != "" {
			go func() {
				_ = risk.SyncPositionsFromDataAPI(context.Background(), accountID)
			}()
		}
	}
	allFilled := true
	anyFilled := false
	for _, r := range results {
		if r.Status != "filled" {
			allFilled = false
		} else {
			anyFilled = true
		}
	}
	status := "failed"
	if allFilled {
		status = "filled"
	} else if anyFilled {
		status = "partial"
	}
	code := 422
	if anyFilled {
		code = 201
	}
	msg := tradeFailureSummary(results)
	logrus.WithFields(logx.Pairs("side", side, "allocations", len(plan.Allocations), "status", status, "http_status", code, "all_filled", allFilled, "any_filled", anyFilled, "failure_summary", msg)).Info("交易：计划执行汇总")
	return &TradeResponse{Status: status, Message: msg, Trades: results, Plan: plan}, code, nil
}

func tradeFailureSummary(rs []TradeResult) string {
	if len(rs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(rs))
	for _, r := range rs {
		if r.Status == "filled" || r.FailureReason == "" {
			continue
		}
		parts = append(parts, r.Platform+": "+r.FailureReason)
	}
	if len(parts) == 0 {
		return ""
	}
	out := strings.Join(parts, "; ")
	if len(out) > 800 {
		return out[:800] + "…"
	}
	return out
}
