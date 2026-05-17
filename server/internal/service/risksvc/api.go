package risksvc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/easyspace-ai/polysdk/pkg/data"

	"github.com/easyspace-ai/polybet/internal/gammaclient"
	"github.com/easyspace-ai/polybet/internal/logx"
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
	// Global stop-loss close execution (bot_config); per-position override is not supported.
	RiskCloseExecutionMode string  `json:"riskCloseExecutionMode"`
	RiskCloseFakWorstPrice  float64 `json:"riskCloseFakWorstPrice"`
	RiskHedgeBuySizing      string  `json:"riskHedgeBuySizing"`
}

func isBenignListContextErr(readOnly bool, err error) bool {
	if !readOnly || err == nil {
		return false
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// avgPriceUSDFromDataPosition returns average entry in 0–1 "probability price" units
// (same units as Data API avgPrice). When the API reports avgPrice as 0 for a fresh
// position (indexing lag), initialValue/size matches Polymarket's documented cost basis.
func avgPriceUSDFromDataPosition(pos data.Position) float64 {
	size, _ := pos.Size.Float64()
	avg, _ := pos.AvgPrice.Float64()
	if size <= 0 {
		return avg
	}
	if avg > 0 {
		return avg
	}
	init, _ := pos.InitialValue.Float64()
	if init > 0 {
		return init / size
	}
	return avg
}

func withPolymarketOutcomeQuery(eventURL, outcomeLabel string) string {
	outcomeTrim := strings.TrimSpace(outcomeLabel)
	if eventURL == "" || outcomeTrim == "" {
		return eventURL
	}
	if !strings.Contains(eventURL, "/event/") {
		return eventURL
	}
	// TODO(polymarket): ?outcome= is a best-effort deep link; confirm against live polymarket.com when UI/routes change.
	u, err := url.Parse(eventURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return eventURL
	}
	q := u.Query()
	if q.Get("outcome") != "" {
		return eventURL
	}
	q.Set("outcome", outcomeTrim)
	u.RawQuery = q.Encode()
	return u.String()
}

func normalizePolySlug(slug string) string {
	slug = strings.Trim(strings.TrimPrefix(strings.TrimSpace(slug), "event/"), "/")
	return slug
}

func polymarketEventURL(slug string) string {
	slug = normalizePolySlug(slug)
	if slug == "" {
		return ""
	}
	return "https://polymarket.com/event/" + slug
}

// polymarketLinks builds direct Polymarket event/market URLs (never search) from DB sync, Data API slugs, and Gamma.
func polymarketLinks(dm store.RiskDisplayMeta, gm gammaclient.TokenMarketDisplay, title, outcomeLabel, posEventSlug, posMarketSlug string) (eventURL, searchURL string) {
	_ = title
	eventSlug := normalizePolySlug(firstNonEmpty(
		dm.PolySlug,
		posEventSlug,
		gm.EventSlug,
		posMarketSlug,
	))
	if eventSlug == "" {
		eventSlug = normalizePolySlug(gm.Slug)
	}
	eventURL = polymarketEventURL(eventSlug)
	if eventURL == "" {
		cond := strings.TrimSpace(gm.ConditionID)
		if strings.HasPrefix(strings.ToLower(cond), "0x") {
			eventURL = "https://polymarket.com/market/" + cond
		}
	}
	if eventURL == "" {
		if id := strings.Trim(strings.TrimSpace(dm.PolyEventID), "/"); id != "" {
			eventURL = polymarketEventURL(id)
		}
	}
	eventURL = withPolymarketOutcomeQuery(eventURL, outcomeLabel)
	if eventURL != "" {
		return eventURL, ""
	}
	return "", ""
}

func (s *Service) ListRiskPositionsEnriched(ctx context.Context, meta Meta, accountID string) ([]map[string]any, Meta, error) {
	return s.listRiskPositionsEnriched(ctx, meta, accountID, false)
}

// ListRiskPositionsEnrichedReadOnly serves HTTP GET paths: no DB writes, no REST
// orderbook fallback, no stop-task enqueue. Ratcheting runs in background workers.
func (s *Service) ListRiskPositionsEnrichedReadOnly(ctx context.Context, meta Meta, accountID string) ([]map[string]any, Meta, error) {
	return s.listRiskPositionsEnriched(ctx, meta, accountID, true)
}

func (s *Service) listRiskPositionsEnriched(ctx context.Context, meta Meta, accountID string, readOnly bool) ([]map[string]any, Meta, error) {
	meta = s.fillMeta(ctx, meta)
	if !readOnly {
		_ = s.st.NormalizeDustRisk(ctx, 1e-9)
	}
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
	if derr != nil && !isBenignListContextErr(readOnly, derr) {
		s.log.WithFields(logx.Pairs("err", derr.Error())).Warn("风控：展示元数据加载失败")
	}
	if derr != nil {
		disp = map[string]store.RiskDisplayMeta{}
	}
	var gammaByTok map[string]gammaclient.TokenMarketDisplay
	if readOnly {
		gammaByTok = s.gammaMetaCachedOnly(tokens)
	} else {
		gammaByTok = s.gammaMetaBatch(ctx, tokens)
	}
	hiddenKeys, herr := s.st.ListRiskHiddenCompositeKeys(ctx, accountID)
	if herr != nil && !isBenignListContextErr(readOnly, herr) {
		s.log.WithFields(logx.Pairs("err", herr.Error())).Warn("风控：隐藏持仓键加载失败")
	}
	if herr != nil {
		hiddenKeys = map[string]struct{}{}
	}
	out := make([]map[string]any, 0, len(rows))
	seen := make(map[string]string) // key -> id

	for _, p := range rows {
		if p.SizeShares < min && p.Status == "open" {
			continue
		}
		tid := store.NormalizeRiskCLOBTokenID(p.TokenID)
		if _, hid := hiddenKeys[store.RiskPositionMonitorKey(p.TokenID, p.SideLabel)]; hid {
			continue
		}
		key := fmt.Sprintf("%s_%s", tid, p.SideLabel)
		if oldID, ok := seen[key]; ok {
			s.log.WithFields(logx.Pairs("key", key, "old_id", oldID, "new_id", p.ID)).Debug("风控：检测到重复持仓键，已跳过")
			continue
		}
		seen[key] = p.ID

		var bid, ask float64
		var ok bool
		if readOnly {
			bid, ask, ok = s.BestBidAskCentsFromCache(tid)
		} else {
			bid, ask, ok = s.BestBidAskCents(ctx, tid)
		}
		var hw, trail float64
		var curPtr *float64
		if readOnly || !ok {
			hw = FloorCents1(p.HighWaterCents)
			trail = s.trailingStopCents(ctx, hw, p.StopLossPct)
			if ok {
				curVal := bid
				if curVal <= 0 && ask > 0 {
					curVal = ask
				}
				if curVal > 0 {
					curPtr = &curVal
				}
			}
		} else {
			var err error
			hw, trail, curPtr, err = s.UpdateHighWaterAndMaybeQueueStop(ctx, p, bid, ask)
			if err != nil {
				fields := logx.Pairs("err", err, "position_id", p.ID)
				s.log.WithFields(fields).Warn("风控：更新高点/止损队列失败")
				logx.StopLoss().WithFields(fields).Warn("风控：更新高点/止损队列失败")
			}
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
		eventURL, searchURL := polymarketLinks(dm, gm, displayTitle, p.SideLabel, p.PolyEventSlug, p.PolyMarketSlug)
		polySlug := normalizePolySlug(firstNonEmpty(dm.PolySlug, p.PolyEventSlug, gm.EventSlug, p.PolyMarketSlug, gm.Slug))
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
			"polySlug":     polySlug,
			"officialUrl": eventURL, "officialSearchUrl": searchURL,
			"imageUrl":      image,
			"iconUrl":       icon,
			"tokenId":       tid,
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

	// Build map of official positions by token_id (canonical CLOB id for DB + unique index).
	officialByToken := make(map[string]data.Position, len(positions))
	for _, pos := range positions {
		hexStr := strings.ToLower(pos.Asset.Text(16))
		if len(hexStr) < 64 {
			hexStr = strings.Repeat("0", 64-len(hexStr)) + hexStr
		}
		tokenID := store.NormalizeRiskCLOBTokenID("0x" + hexStr)
		if tokenID != "" {
			officialByToken[tokenID] = pos
		}
	}

	// Upsert or update existing positions
	for tokenID, pos := range officialByToken {
		if err := ctx.Err(); err != nil {
			return err
		}
		size, _ := pos.Size.Float64()
		if size <= 0 {
			continue
		}
		avgPrice := avgPriceUSDFromDataPosition(pos)
		initialVal, _ := pos.InitialValue.Float64()
		entryCents := avgPrice * 100
		costUsd := initialVal
		if costUsd <= 0 {
			costUsd = size * avgPrice
		}

		existing, err := s.st.GetOpenRiskPositionByToken(ctx, tokenID, acct.ID)
		if err != nil {
			s.log.WithFields(logx.Pairs("token", tokenID, "err", err)).Warn("风控同步：按 token 查询持仓失败")
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
				PolyEventSlug:  strings.TrimSpace(pos.EventSlug),
				PolyMarketSlug: strings.TrimSpace(pos.Slug),
				AvgEntryCents:  entryCents,
				SizeShares:     size,
				CostUSD:        costUsd,
				HighWaterCents: FloorCents1(entryCents),
				StopLossPct:    stop,
				Source:         "polymarket_api",
				Status:         "open",
			})
			if err != nil {
				fields := logx.Pairs("token", tokenID, "err", err)
				s.log.WithFields(fields).Warn("风控同步：创建持仓失败")
				logx.Open().WithFields(fields).Warn("风控同步：创建持仓失败")
			} else {
				openFields := logx.Pairs("token", tokenID, "size_shares", size, "avg_entry_cents", entryCents, "source", "polymarket_api")
				logx.Open().WithFields(openFields).Info("风控同步：新建持仓")
				logx.Position().WithFields(openFields).Info("风控同步：新建持仓")
			}
		} else {
			// Existing position: keep high_water, update shares/avg/cost
			err = s.st.UpdateRiskPositionSharesCost(ctx, existing.ID, size, costUsd)
			if err != nil {
				fields := logx.Pairs("token", tokenID, "err", err)
				s.log.WithFields(fields).Warn("风控同步：更新份额失败")
				logx.Position().WithFields(fields).Warn("风控同步：更新份额失败")
			} else {
				posFields := logx.Pairs("token", tokenID, "position_id", existing.ID, "size_shares", size, "avg_entry_cents", entryCents)
				logx.Position().WithFields(posFields).Info("风控同步：更新持仓份额")
			}
			if existing.AvgEntryCents != entryCents {
				_ = s.st.UpdateRiskPositionAvgEntry(ctx, existing.ID, entryCents)
			}
			if existing.Title != pos.Title {
				_ = s.st.UpdateRiskPositionTitle(ctx, existing.ID, strings.TrimSpace(pos.Title), strings.TrimSpace(pos.Outcome))
			}
			_ = s.st.UpdateRiskPositionPolySlugs(ctx, existing.ID, pos.EventSlug, pos.Slug)
		}
	}

	// Close positions that are open in DB but not in official response
	min := s.minShares(ctx)
	openRows, err := s.st.ListOpenRiskPositionsMinShares(ctx, min, acct.ID)
	if err != nil {
		return err
	}
	for _, p := range openRows {
		if err := ctx.Err(); err != nil {
			return err
		}
		tok := store.NormalizeRiskCLOBTokenID(p.TokenID)
		if _, ok := officialByToken[tok]; !ok {
			_ = s.st.CloseRiskPosition(ctx, p.ID)
			fields := logx.Pairs("token", p.TokenID, "position_id", p.ID)
			s.log.WithFields(fields).Info("风控同步：官方已无该持仓，已关闭本地记录")
			logx.Position().WithFields(fields).Info("风控同步：官方已无该持仓，已关闭本地记录")
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
		eventURL, _ := polymarketLinks(
			store.RiskDisplayMeta{},
			gammaclient.TokenMarketDisplay{},
			t.Title,
			t.Outcome,
			t.EventSlug,
			t.Slug,
		)
		polySlug := normalizePolySlug(firstNonEmpty(t.EventSlug, t.Slug))
		out = append(out, map[string]any{
			"id":           t.TransactionHash.Hex(),
			"side":         strings.ToLower(string(t.Side)),
			"title":        t.Title,
			"outcome":      t.Outcome,
			"size":         size,
			"price":        price,
			"priceCents":   price * 100,
			"timestamp":    time.Unix(t.Timestamp, 0).UTC().Format(time.RFC3339),
			"icon":         t.Icon,
			"polySlug":     polySlug,
			"officialUrl":  eventURL,
		})
	}
	return out, nil
}

// OfficialURLForRiskPosition resolves a direct Polymarket link for a stored risk row.
func (s *Service) OfficialURLForRiskPosition(ctx context.Context, p *store.RiskPosition) string {
	if p == nil || strings.TrimSpace(p.TokenID) == "" {
		return ""
	}
	disp, _ := s.st.RiskDisplayMetaForPositions(ctx, []store.RiskPosition{*p})
	dm := disp[p.TokenID]
	gm := s.gammaMetaBatch(ctx, []string{p.TokenID})[p.TokenID]
	url, _ := polymarketLinks(dm, gm, p.Title, p.SideLabel, p.PolyEventSlug, p.PolyMarketSlug)
	return url
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
