package risksvc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/easyspace-ai/polysdk/pkg/data"

	"github.com/easyspace-ai/polybet/internal/gammaclient"
	"github.com/easyspace-ai/polybet/internal/store"
	"github.com/ethereum/go-ethereum/common"
	"github.com/google/uuid"
)

type Meta struct {
	UserWsConnected         bool    `json:"userWsConnected"`
	UserWsConnecting        bool    `json:"userWsConnecting"`
	UserWsLastMessageAt     *string `json:"userWsLastMessageAt"`
	RestTradesSyncLastAt    *string `json:"restTradesSyncLastAt"`
	UserWsLastIssue         *string `json:"userWsLastIssue"`
	OutboundProxyConfigured bool    `json:"outboundProxyConfigured"`
	MinOpenRiskShares       float64 `json:"minOpenRiskShares"`
	OrderbookWsConnected    bool    `json:"orderbookWsConnected"`
	OrderbookWsConnecting   bool    `json:"orderbookWsConnecting"`
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

func (s *Service) ListRiskPositionsEnriched(ctx context.Context, meta Meta, accountID string) ([]map[string]any, Meta, error) {
	meta = s.fillMeta(meta)
	_ = s.st.NormalizeDustRisk(ctx, 1e-9)
	min := s.minShares(ctx)
	rows, err := s.st.ListOpenOrClosingRiskPositions(ctx, accountID)
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

func (s *Service) SyncPositionsFromDataAPI(ctx context.Context, accountID string) error {
	acct, err := s.st.GetActivePolymarketAccount(ctx)
	if err != nil || acct == nil || strings.TrimSpace(acct.FunderAddress) == "" {
		if err != nil {
			return fmt.Errorf("no active account: %w", err)
		}
		return fmt.Errorf("no active account")
	}
	addr := common.HexToAddress(strings.TrimSpace(acct.FunderAddress))
	limit := 500
	positions, err := s.dataClient.Positions(ctx, &data.PositionsRequest{
		User:  addr,
		Limit: &limit,
	})
	if err != nil {
		return fmt.Errorf("positions api: %w", err)
	}

	// Build map of official positions by token_id
	officialByToken := make(map[string]data.Position, len(positions))
	for _, pos := range positions {
		hexStr := strings.ToLower(pos.Asset.Text(16))
		if len(hexStr) < 64 {
			hexStr = strings.Repeat("0", 64-len(hexStr)) + hexStr
		}
		tokenID := "0x" + hexStr
		if tokenID != "0x" {
			officialByToken[tokenID] = pos
		}
	}

	// Upsert or update existing positions
	for tokenID, pos := range officialByToken {
		size, _ := pos.Size.Float64()
		avgPrice, _ := pos.AvgPrice.Float64()
		initialVal, _ := pos.InitialValue.Float64()
		if size <= 0 {
			continue
		}
		entryCents := avgPrice * 100
		costUsd := initialVal
		if costUsd <= 0 {
			costUsd = size * avgPrice
		}

		existing, err := s.st.GetOpenRiskPositionByToken(ctx, tokenID, acct.ID)
		if err != nil {
			s.log.Warn("sync_positions_lookup", "token", tokenID, "err", err)
			continue
		}
		if existing == nil {
			// New position: high_water = avg_entry
			stop := resolveStopLossPct(ctx, s.st, entryCents)
			err = s.st.CreateRiskPosition(ctx, &store.RiskPosition{
				ID:             uuid.NewString(),
				Platform:       "polymarket",
				AccountID:      acct.ID,
				TokenID:        tokenID,
				Title:          strings.TrimSpace(pos.Title),
				SideLabel:      strings.TrimSpace(pos.Outcome),
				AvgEntryCents:  entryCents,
				SizeShares:     size,
				CostUSD:        costUsd,
				HighWaterCents: entryCents,
				StopLossPct:    stop,
				Source:         "polymarket_api",
				Status:         "open",
			})
			if err != nil {
				s.log.Warn("sync_positions_create", "token", tokenID, "err", err)
			}
		} else {
			// Existing position: keep high_water, update shares/avg/cost
			err = s.st.UpdateRiskPositionSharesCost(ctx, existing.ID, size, costUsd)
			if err != nil {
				s.log.Warn("sync_positions_update_shares", "token", tokenID, "err", err)
			}
			if existing.AvgEntryCents != entryCents {
				_ = s.st.UpdateRiskPositionAvgEntry(ctx, existing.ID, entryCents)
			}
			if existing.Title != pos.Title {
				_ = s.st.UpdateRiskPositionTitle(ctx, existing.ID, strings.TrimSpace(pos.Title), strings.TrimSpace(pos.Outcome))
			}
		}
	}

	// Close positions that are open in DB but not in official response
	min := s.minShares(ctx)
	openRows, err := s.st.ListOpenRiskPositionsMinShares(ctx, min, acct.ID)
	if err != nil {
		return err
	}
	for _, p := range openRows {
		if _, ok := officialByToken[strings.ToLower(p.TokenID)]; !ok {
			_ = s.st.CloseRiskPosition(ctx, p.ID)
			s.log.Info("sync_positions_close_missing", "token", p.TokenID)
		}
	}

	return nil
}

func (s *Service) SyncRiskFromRESTTrades(ctx context.Context) error {
	return s.SyncPositionsFromDataAPI(ctx, "")
}

func (s *Service) ListOfficialTrades(ctx context.Context, limit int) ([]map[string]any, error) {
	acct, err := s.st.GetActivePolymarketAccount(ctx)
	if err != nil || acct == nil || strings.TrimSpace(acct.FunderAddress) == "" {
		return nil, fmt.Errorf("no active account")
	}
	addr := common.HexToAddress(strings.TrimSpace(acct.FunderAddress))
	if limit <= 0 {
		limit = 50
	}
	trades, err := s.dataClient.Trades(ctx, &data.TradesRequest{
		User:  &addr,
		Limit: &limit,
	})
	if err != nil {
		return nil, fmt.Errorf("trades api: %w", err)
	}
	out := make([]map[string]any, 0, len(trades))
	for _, t := range trades {
		size, _ := t.Size.Float64()
		price, _ := t.Price.Float64()
		out = append(out, map[string]any{
			"id":         t.TransactionHash.Hex(),
			"side":       strings.ToLower(string(t.Side)),
			"title":      t.Title,
			"outcome":    t.Outcome,
			"size":       size,
			"price":      price,
			"priceCents": price * 100,
			"timestamp":  time.Unix(t.Timestamp, 0).UTC().Format(time.RFC3339),
			"icon":       t.Icon,
		})
	}
	return out, nil
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
