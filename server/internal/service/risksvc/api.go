package risksvc

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/easyspace-ai/polysdk/pkg/clob/clobtypes"

	"github.com/easyspace-ai/polybet/internal/dataclient"
	"github.com/easyspace-ai/polybet/internal/gammaclient"
	"github.com/easyspace-ai/polybet/internal/polyexec"
	"github.com/easyspace-ai/polybet/internal/polywiring"
	"github.com/easyspace-ai/polybet/internal/service/polysession"
	"github.com/easyspace-ai/polybet/internal/store"
)

const reconcileMinInterval = 60 * time.Second

// reconcileZeroEps: treat on-chain conditional balance at or below this as flat (Polymarket UI shows no position).
const reconcileZeroEps = 1e-8

type Meta struct {
	UserWsConnected         bool    `json:"userWsConnected"`
	UserWsConnecting        bool    `json:"userWsConnecting"`
	UserWsLastMessageAt     *string `json:"userWsLastMessageAt"`
	RestTradesSyncLastAt    *string `json:"restTradesSyncLastAt"`
	UserWsLastIssue         *string `json:"userWsLastIssue"`
	OutboundProxyConfigured bool    `json:"outboundProxyConfigured"`
	MinOpenRiskShares       float64 `json:"minOpenRiskShares"`
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// polymarketLinks builds human-facing Polymarket URLs from local DB sync + live Gamma /markets.
func polymarketLinks(dm store.RiskDisplayMeta, gm gammaclient.TokenMarketDisplay, title string) (eventURL, searchURL string) {
	slug := strings.Trim(firstNonEmpty(dm.PolySlug, gm.EventSlug, gm.Slug), "/")
	if slug != "" {
		slug = strings.TrimPrefix(slug, "event/")
		return "https://polymarket.com/event/" + slug, ""
	}
	cond := strings.TrimSpace(gm.ConditionID)
	if strings.HasPrefix(strings.ToLower(cond), "0x") {
		return "https://polymarket.com/market/" + cond, ""
	}
	if id := strings.Trim(strings.TrimSpace(dm.PolyEventID), "/"); id != "" {
		return "https://polymarket.com/event/" + id, ""
	}
	t := strings.TrimSpace(title)
	if t != "" {
		return "", "https://polymarket.com/search?q=" + url.QueryEscape(t)
	}
	return "", "https://polymarket.com/"
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
	tokens := make([]string, 0, len(rows))
	for _, p := range rows {
		if tid := strings.TrimSpace(p.TokenID); tid != "" {
			tokens = append(tokens, tid)
		}
	}
	disp, derr := s.st.RiskDisplayMetaForPositions(ctx, rows)
	if derr != nil {
		s.log.Warn("risk_display_meta", "err", derr.Error())
		disp = map[string]store.RiskDisplayMeta{}
	}
	gammaByTok := s.gammaMetaBatch(ctx, tokens)
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
		dm := disp[p.TokenID]
		gm := gammaByTok[p.TokenID]
		displayTitle := strings.TrimSpace(p.Title)
		if dm.HomeTeam != "" && dm.AwayTeam != "" {
			displayTitle = dm.HomeTeam + " vs " + dm.AwayTeam
		} else if strings.TrimSpace(gm.Question) != "" {
			displayTitle = gm.Question
		}
		sport := firstNonEmpty(dm.Sport, gm.Category)
		if sport != "" {
			sport = strings.ToLower(sport)
		}
		eventURL, searchURL := polymarketLinks(dm, gm, displayTitle)
		image := strings.TrimSpace(gm.Image)
		icon := strings.TrimSpace(gm.Icon)
		if icon == "" {
			icon = image
		}
		if image == "" {
			image = icon
		}
		m := map[string]any{
			"id": p.ID, "title": p.Title, "sideLabel": p.SideLabel,
			"displayTitle": displayTitle, "sport": sport,
			"officialUrl": eventURL, "officialSearchUrl": searchURL,
			"imageUrl": image,
			"iconUrl":  icon,
			"tokenId": p.TokenID,
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
	return s.ReconcileOpenRiskPositionsWithClobBalances(ctx)
}

// ReconcileOpenRiskPositionsWithClobBalances compares each open/closing row to Polymarket portfolio data
// (Data API /positions — same family as the website) when possible, then falls back to CLOB balance-allowance.
func (s *Service) ReconcileOpenRiskPositionsWithClobBalances(ctx context.Context) error {
	cl, err := polysession.ResolveAuthedCLOB(ctx, s.cfg, s.st)
	if err != nil {
		return err
	}
	return s.reconcileRiskPositionsWithAuthedCLOB(ctx, cl)
}

func polyWantsOfficialPortfolio(funder string) bool {
	a := strings.TrimSpace(strings.ToLower(funder))
	return strings.HasPrefix(a, "0x") && len(a) == 42 && a != "0x0000000000000000000000000000000000000000"
}

func useOfficialDataForSource(source string) bool {
	switch strings.TrimSpace(source) {
	case "", "polymarket_clob", "bot":
		return true
	default:
		return false
	}
}

func (s *Service) reconcileRiskPositionsWithAuthedCLOB(ctx context.Context, cl *polywiring.AuthedCLOB) error {
	if cl == nil || cl.Client == nil {
		return errors.New("nil authed clob")
	}
	client := cl.Client
	fund := strings.TrimSpace(cl.FunderAddress)

	var official map[string]float64
	var dataErr error
	if polyWantsOfficialPortfolio(fund) {
		var err error
		official, err = dataclient.FetchPositivePositionSizes(ctx, s.cfg.HTTPPlatformProxy, fund)
		dataErr = err
		if err != nil && s.log != nil {
			s.log.Warn("risk_reconcile_data_positions_err", "funder", fund, "err", err.Error())
		}
	}

	rows, err := s.st.ListRiskPositionsOpenClosing(ctx)
	if err != nil {
		return err
	}
	for _, p := range rows {
		tok := strings.TrimSpace(p.TokenID)
		if tok == "" {
			if s.log != nil {
				s.log.Debug("risk_reconcile_skip_no_token", "position_id", p.ID)
			}
			continue
		}

		useData := dataErr == nil && official != nil && useOfficialDataForSource(p.Source) && polyWantsOfficialPortfolio(fund)
		if useData {
			sz, has := official[tok]
			if !has || sz <= reconcileZeroEps {
				if s.log != nil {
					s.log.Info("risk_reconcile_close_data_api", "position_id", p.ID, "token_id", tok, "has", has, "api_size", sz, "source", p.Source)
				}
				if closeErr := s.st.CloseRiskPosition(ctx, p.ID); closeErr != nil {
					s.log.Error("risk_reconcile_close_err", "position_id", p.ID, "token_id", tok, "err", closeErr.Error())
				}
				continue
			}
			if sz+1e-9 < p.SizeShares {
				ratio := sz / p.SizeShares
				newCost := max(0, p.CostUSD*ratio)
				if s.log != nil {
					s.log.Info("risk_reconcile_scale_data_api", "position_id", p.ID, "token_id", tok, "db_shares", p.SizeShares, "api_shares", sz)
				}
				if updateErr := s.st.UpdateRiskPositionSharesCost(ctx, p.ID, sz, newCost); updateErr != nil {
					s.log.Error("risk_reconcile_scale_err", "position_id", p.ID, "token_id", tok, "err", updateErr.Error())
				}
			}
			continue
		}

		bal, err := client.BalanceAllowance(ctx, &clobtypes.BalanceAllowanceRequest{
			AssetType: clobtypes.AssetTypeConditional,
			TokenID:   tok,
		})
		if err != nil {
			if s.log != nil {
				s.log.Warn("risk_reconcile_balance_err", "position_id", p.ID, "token_id", tok, "err", err.Error())
			}
			// Fallback: if official data is available, use it even for sources that normally prefer on-chain.
			if dataErr == nil && official != nil {
				sz, has := official[tok]
				if !has || sz <= reconcileZeroEps {
					if s.log != nil {
						s.log.Info("risk_reconcile_close_data_fallback", "position_id", p.ID, "token_id", tok, "has", has, "api_size", sz, "source", p.Source)
					}
					if closeErr := s.st.CloseRiskPosition(ctx, p.ID); closeErr != nil {
						s.log.Error("risk_reconcile_close_err", "position_id", p.ID, "token_id", tok, "err", closeErr.Error())
					}
				} else if sz+1e-9 < p.SizeShares {
					ratio := sz / p.SizeShares
					newCost := max(0, p.CostUSD*ratio)
					if s.log != nil {
						s.log.Info("risk_reconcile_scale_data_fallback", "position_id", p.ID, "token_id", tok, "db_shares", p.SizeShares, "api_shares", sz)
					}
					if updateErr := s.st.UpdateRiskPositionSharesCost(ctx, p.ID, sz, newCost); updateErr != nil {
						s.log.Error("risk_reconcile_scale_err", "position_id", p.ID, "token_id", tok, "err", updateErr.Error())
					}
				}
			}
			continue
		}
		onChain := polyexec.ConditionalBalanceShares(bal.Balance)
		if math.IsNaN(onChain) || math.IsInf(onChain, 0) {
			if s.log != nil {
				s.log.Warn("risk_reconcile_balance_bad_float", "position_id", p.ID, "token_id", tok, "balance_raw", bal.Balance)
			}
			continue
		}
		if onChain <= reconcileZeroEps {
			if s.log != nil {
				s.log.Info("risk_reconcile_close_zero", "position_id", p.ID, "token_id", tok, "balance_raw", bal.Balance, "on_chain", onChain)
			}
			if closeErr := s.st.CloseRiskPosition(ctx, p.ID); closeErr != nil {
				s.log.Error("risk_reconcile_close_err", "position_id", p.ID, "token_id", tok, "err", closeErr.Error())
			}
			continue
		}
		if onChain+1e-9 < p.SizeShares {
			ratio := onChain / p.SizeShares
			newCost := max(0, p.CostUSD*ratio)
			if updateErr := s.st.UpdateRiskPositionSharesCost(ctx, p.ID, onChain, newCost); updateErr != nil {
				s.log.Error("risk_reconcile_scale_err", "position_id", p.ID, "token_id", tok, "err", updateErr.Error())
			}
		}
	}
	lastReconcile = time.Now()
	return nil
}

func (s *Service) SyncRiskFromRESTTrades(ctx context.Context) error {
	cl, err := polysession.ResolveAuthedCLOB(ctx, s.cfg, s.st)
	if err != nil {
		return err
	}
	var tradeErr error
	trades, err := cl.Client.Trades(ctx, &clobtypes.TradesRequest{})
	if err != nil {
		tradeErr = err
	} else {
		n := min(len(trades.Data), 100)
		for i := n - 1; i >= 0; i-- {
			t := trades.Data[i]
			_, _ = s.ApplyClobTradeIfNew(ctx, struct {
				ID, AssetID, Side, Size, Price, Status string
				Market, Outcome                        string
			}{ID: t.ID, AssetID: t.AssetID, Side: t.Side, Size: t.Size, Price: t.Price, Status: t.Status, Market: t.Market, Outcome: ""})
		}
	}
	s.touchRESTTradesSync()
	recErr := s.reconcileRiskPositionsWithAuthedCLOB(ctx, cl)
	return errors.Join(tradeErr, recErr)
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
