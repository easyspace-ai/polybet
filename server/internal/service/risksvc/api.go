package risksvc

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/easyspace-ai/polysdk/pkg/clob/clobtypes"

	"github.com/easyspace-ai/polybet/internal/polyexec"
	"github.com/easyspace-ai/polybet/internal/service/polysession"
)

const reconcileMinInterval = 60 * time.Second

type Meta struct {
	UserWsConnected         bool    `json:"userWsConnected"`
	UserWsConnecting        bool    `json:"userWsConnecting"`
	UserWsLastMessageAt     *string `json:"userWsLastMessageAt"`
	RestTradesSyncLastAt    *string `json:"restTradesSyncLastAt"`
	UserWsLastIssue         *string `json:"userWsLastIssue"`
	OutboundProxyConfigured bool    `json:"outboundProxyConfigured"`
	MinOpenRiskShares       float64 `json:"minOpenRiskShares"`
}

func (s *Service) ListRiskPositionsEnriched(ctx context.Context, meta Meta) ([]map[string]any, Meta, error) {
	meta = s.fillMeta(meta)
	_ = s.st.NormalizeDustRisk(ctx, 1e-9)
	if err := s.maybeReconcileBalances(ctx); err != nil {
		s.log.Warn("reconcile", "err", err)
	}
	min := s.minShares(ctx)
	rows, err := s.st.ListOpenOrClosingRiskPositions(ctx)
	if err != nil {
		return nil, meta, err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, p := range rows {
		if p.SizeShares < min && p.Status == "open" {
			continue
		}
		bid, ok := s.bestBidCents(ctx, p.TokenID)
		var hw, trail float64
		var curPtr *float64
		var err error
		if ok {
			hw, trail, curPtr, err = s.UpdateHighWaterAndMaybeQueueStop(ctx, p, bid)
			if err != nil {
				s.log.Warn("hw", "err", err)
			}
		} else {
			hw = p.HighWaterCents
			trail = hw * (1 - p.StopLossPct/100)
		}
		var valUsd, pnl *float64
		if curPtr != nil {
			v := *curPtr / 100
			vv := p.SizeShares * v
			valUsd = &vv
			pv := vv - p.CostUSD
			pnl = &pv
		}
		maxPay := p.SizeShares * 1
		pot := maxPay - p.CostUSD
		m := map[string]any{
			"id": p.ID, "title": p.Title, "sideLabel": p.SideLabel,
			"avgEntryCents": p.AvgEntryCents, "currentCents": curPtr,
			"sizeShares": p.SizeShares, "costUsd": p.CostUSD,
			"highWaterCents": hw, "stopLossPct": p.StopLossPct,
			"trailingStopCents": trail, "valueUsd": valUsd, "pnlUsd": pnl,
			"maxPayoffUsd": maxPay, "potentialProfitUsd": pot,
			"status": p.Status, "source": p.Source,
		}
		out = append(out, m)
	}
	meta.MinOpenRiskShares = min
	return out, meta, nil
}

var lastReconcile time.Time

func (s *Service) maybeReconcileBalances(ctx context.Context) error {
	if time.Since(lastReconcile) < reconcileMinInterval {
		return nil
	}
	lastReconcile = time.Now()
	return s.ReconcileOpenRiskPositionsWithClobBalances(ctx)
}

func (s *Service) ReconcileOpenRiskPositionsWithClobBalances(ctx context.Context) error {
	cl, err := polysession.ResolveAuthedCLOB(ctx, s.cfg, s.st)
	if err != nil {
		return err
	}
	min := s.minShares(ctx)
	rows, err := s.st.ListRiskPositionsOpenClosing(ctx)
	if err != nil {
		return err
	}
	for _, p := range rows {
		bal, err := cl.Client.BalanceAllowance(ctx, &clobtypes.BalanceAllowanceRequest{
			AssetType: clobtypes.AssetTypeConditional,
			TokenID:   p.TokenID,
		})
		if err != nil {
			continue
		}
		onChain := polyexec.ConditionalBalanceShares(bal.Balance)
		if onChain < min {
			_ = s.st.CloseRiskPosition(ctx, p.ID)
			continue
		}
		if onChain+1e-9 < p.SizeShares {
			ratio := onChain / p.SizeShares
			newCost := max(0, p.CostUSD*ratio)
			_ = s.st.UpdateRiskPositionSharesCost(ctx, p.ID, onChain, newCost)
		}
	}
	return nil
}

func (s *Service) SyncRiskFromRESTTrades(ctx context.Context) error {
	cl, err := polysession.ResolveAuthedCLOB(ctx, s.cfg, s.st)
	if err != nil {
		return err
	}
	trades, err := cl.Client.Trades(ctx, &clobtypes.TradesRequest{})
	if err != nil {
		return err
	}
	n := len(trades.Data)
	if n > 100 {
		n = 100
	}
	for i := n - 1; i >= 0; i-- {
		t := trades.Data[i]
		_, _ = s.ApplyClobTradeIfNew(ctx, struct {
			ID, AssetID, Side, Size, Price, Status string
			Market, Outcome                        string
		}{ID: t.ID, AssetID: t.AssetID, Side: t.Side, Size: t.Size, Price: t.Price, Status: t.Status, Market: t.Market, Outcome: ""})
	}
	s.touchRESTTradesSync()
	return s.ReconcileOpenRiskPositionsWithClobBalances(ctx)
}

// ParseUserWsTradePayload mirrors Node parseUserWsTradePayload (minimal).
func ParseUserWsTradePayload(raw []byte) (struct {
	ID, AssetID, Side, Size, Price, Status string
	Market, Outcome                        string
}, bool) {
	var o map[string]any
	if err := json.Unmarshal(raw, &o); err != nil {
		return struct {
			ID, AssetID, Side, Size, Price, Status string
			Market, Outcome                        string
		}{}, false
	}
	et := strings.ToLower(anyStr(o["event_type"]))
	ty := strings.ToUpper(anyStr(o["type"]))
	if et != "trade" && ty != "TRADE" {
		return struct {
			ID, AssetID, Side, Size, Price, Status string
			Market, Outcome                        string
		}{}, false
	}
	id := anyStr(o["id"])
	aid := anyStr(o["asset_id"])
	if id == "" || aid == "" {
		return struct {
			ID, AssetID, Side, Size, Price, Status string
			Market, Outcome                        string
		}{}, false
	}
	return struct {
		ID, AssetID, Side, Size, Price, Status string
		Market, Outcome                        string
	}{
		ID: id, AssetID: aid, Side: anyStr(o["side"]), Size: anyStr(o["size"]), Price: anyStr(o["price"]), Status: anyStr(o["status"]),
		Market: anyStr(o["market"]), Outcome: anyStr(o["outcome"]),
	}, true
}

func anyStr(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return ""
	}
}
