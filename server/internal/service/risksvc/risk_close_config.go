package risksvc

import (
	"context"
	"errors"
	"strings"

	"github.com/easyspace-ai/polybet/internal/store"
)

const (
	botKeyRiskCloseExecutionMode = "riskCloseExecutionMode"
	botKeyRiskCloseFakWorstPrice = "riskCloseFakWorstPrice"
	botKeyRiskHedgeBuySizing     = "riskHedgeBuySizing"
	botKeyRiskHedgeAutoHide      = "riskHedgeAutoHidePosition"

	riskCloseModeFOKSell     = "fok_sell"
	riskCloseModeFAKSell     = "fak_sell"
	riskCloseModeHedgeFOKBuy = "hedge_fok_buy"
)

// errPartialFillRemaining is returned when a FAK sell filled partially and the YES row should stay open for retry.
var errPartialFillRemaining = errors.New("partial_fill_remaining")

func effectiveRiskCloseExecutionMode(ctx context.Context, st *store.Store) string {
	v, _, _ := st.GetBotConfig(ctx, botKeyRiskCloseExecutionMode)
	switch strings.TrimSpace(strings.ToLower(v)) {
	case riskCloseModeFAKSell:
		return riskCloseModeFAKSell
	case riskCloseModeHedgeFOKBuy:
		return riskCloseModeHedgeFOKBuy
	default:
		return riskCloseModeFOKSell
	}
}

func effectiveRiskHedgeBuySizing(ctx context.Context, st *store.Store) string {
	v, _, _ := st.GetBotConfig(ctx, botKeyRiskHedgeBuySizing)
	if strings.TrimSpace(strings.ToLower(v)) == "shares" {
		return "shares"
	}
	return "notional"
}

func riskHedgeAutoHideDefaultTrue(ctx context.Context, st *store.Store) bool {
	v, ok, _ := st.GetBotConfig(ctx, botKeyRiskHedgeAutoHide)
	if !ok {
		return true
	}
	switch strings.TrimSpace(strings.ToLower(v)) {
	case "false", "0", "no":
		return false
	default:
		return true
	}
}

func markPrice01FromEvalCents(bidCents, askCents float64) float64 {
	m := maxCentsRatchet(bidCents, askCents)
	if m <= 0 {
		return 0
	}
	return m / 100.0
}
