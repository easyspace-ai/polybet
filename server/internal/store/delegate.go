package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/easyspace-ai/polybet/internal/accountsfile"
	"github.com/easyspace-ai/polybet/internal/storage/badgerdb"
)

// Re-export types backed by Badger documents.
type (
	RiskPosition          = badgerdb.RiskPosition
	RiskTask              = badgerdb.RiskTask
	TradeQuality          = badgerdb.TradeQuality
	TradeQualityAggregate = badgerdb.TradeQualityAggregate
	EventRealizedPnL      = badgerdb.EventRealizedPnL
	LastTradeSummary      = badgerdb.LastTradeSummary
)

// DefaultStopLossPct matches badgerdb.DefaultStopLossPct.
const DefaultStopLossPct = badgerdb.DefaultStopLossPct

// ErrRiskPatchNoFields is returned when PATCH omits both fields.
var ErrRiskPatchNoFields = badgerdb.ErrRiskPatchNoFields

// RiskPositionConfig is retained for API compatibility (merged into RiskPosition in storage).
type RiskPositionConfig struct {
	PositionID     string
	HighWaterCents float64
	StopLossPct    float64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NormalizeRiskCLOBTokenID re-exports the canonical CLOB token normalizer.
func NormalizeRiskCLOBTokenID(tokenID string) string {
	return badgerdb.NormalizeCLOBTokenID(tokenID)
}

// IsKnownStartTime re-exports the market start-time predicate.
func IsKnownStartTime(t time.Time) bool {
	return badgerdb.IsKnownStartTime(t)
}

func (s *Store) InsertRiskAppliedTrade(ctx context.Context, id string, accountID string) (bool, error) {
	return s.kv().InsertRiskAppliedTrade(ctx, id, accountID)
}

func (s *Store) ListOpenRiskPositionsByToken(ctx context.Context, tokenID string, accountID string) ([]RiskPosition, error) {
	return s.kv().ListOpenRiskPositionsByToken(ctx, tokenID, accountID)
}

func (s *Store) ListOpenRiskPositionTokenIDs(ctx context.Context) ([]string, error) {
	return s.kv().ListOpenRiskPositionTokenIDs(ctx)
}

func (s *Store) ListOpenRiskPositionTokenIDsForAccount(ctx context.Context, accountID string) ([]string, error) {
	return s.kv().ListOpenRiskPositionTokenIDsForAccount(ctx, accountID)
}

func (s *Store) ListOpenRiskPositionsMinShares(ctx context.Context, minShares float64, accountID string) ([]RiskPosition, error) {
	return s.kv().ListOpenRiskPositionsMinShares(ctx, minShares, accountID)
}

func (s *Store) CountOpenRiskPositionsMinShares(ctx context.Context, minShares float64, accountID string) (int64, error) {
	return s.kv().CountOpenRiskPositionsMinShares(ctx, minShares, accountID)
}

func (s *Store) ListOpenOrClosingRiskPositions(ctx context.Context, accountID string) ([]RiskPosition, error) {
	return s.kv().ListOpenOrClosingRiskPositions(ctx, accountID)
}

func (s *Store) GetRiskPosition(ctx context.Context, id string) (*RiskPosition, error) {
	return s.kv().GetRiskPosition(ctx, id)
}

func (s *Store) UpdateRiskPositionPolySlugs(ctx context.Context, id, eventSlug, marketSlug string) error {
	return s.kv().UpdateRiskPositionPolySlugs(ctx, id, eventSlug, marketSlug)
}

func (s *Store) UpdateRiskPositionHighWater(ctx context.Context, id string, hw float64) error {
	return s.kv().UpdateRiskPositionHighWater(ctx, id, hw)
}

func (s *Store) UpdateRiskPositionStop(ctx context.Context, id string, stopLossPct *float64, highWaterCents *float64) error {
	return s.kv().UpdateRiskPositionStop(ctx, id, stopLossPct, highWaterCents)
}

func (s *Store) UpsertRiskPositionConfig(ctx context.Context, cfg *RiskPositionConfig) error {
	if cfg == nil {
		return errors.New("nil config")
	}
	return s.kv().UpsertRiskPositionConfig(ctx, cfg.PositionID, cfg.HighWaterCents, cfg.StopLossPct)
}

func (s *Store) GetRiskPositionConfig(ctx context.Context, positionID string) (*RiskPositionConfig, error) {
	hw, stop, ca, ua, ok, err := s.kv().GetRiskPositionConfig(ctx, positionID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return &RiskPositionConfig{
		PositionID: positionID, HighWaterCents: hw, StopLossPct: stop, CreatedAt: ca, UpdatedAt: ua,
	}, nil
}

func (s *Store) UpdateRiskPositionStatusShares(ctx context.Context, id, status string, shares, cost float64) error {
	return s.kv().UpdateRiskPositionStatusShares(ctx, id, status, shares, cost)
}

func (s *Store) CreateRiskPosition(ctx context.Context, p *RiskPosition) error {
	return s.kv().CreateRiskPosition(ctx, p)
}

func (s *Store) GetOpenRiskPositionByToken(ctx context.Context, tokenID, accountID string) (*RiskPosition, error) {
	return s.kv().GetOpenRiskPositionByToken(ctx, tokenID, accountID)
}

func (s *Store) UpdateRiskPositionAvgEntry(ctx context.Context, id string, avgEntryCents float64) error {
	return s.kv().UpdateRiskPositionAvgEntry(ctx, id, avgEntryCents)
}

func (s *Store) UpdateRiskPositionTitle(ctx context.Context, id, title, sideLabel string) error {
	return s.kv().UpdateRiskPositionTitle(ctx, id, title, sideLabel)
}

func (s *Store) NormalizeDustRisk(ctx context.Context, dust float64) error {
	return s.kv().NormalizeDustRisk(ctx, dust)
}

func (s *Store) FindPendingCloseTask(ctx context.Context, positionID string) (bool, error) {
	return s.kv().FindPendingCloseTask(ctx, positionID)
}

func (s *Store) InsertRiskTask(ctx context.Context, t *RiskTask) error {
	return s.kv().InsertRiskTask(ctx, t)
}

func (s *Store) ListDueRiskTasks(ctx context.Context, limit int) ([]RiskTask, error) {
	return s.kv().ListDueRiskTasks(ctx, limit)
}

func (s *Store) SetRiskPositionStatus(ctx context.Context, id, status string) error {
	return s.kv().SetRiskPositionStatus(ctx, id, status)
}

func (s *Store) UpdateRiskPositionSharesCost(ctx context.Context, id string, shares, cost float64) error {
	return s.kv().UpdateRiskPositionSharesCost(ctx, id, shares, cost)
}

func (s *Store) CloseRiskPosition(ctx context.Context, id string) error {
	return s.kv().CloseRiskPosition(ctx, id)
}

func (s *Store) CloseRiskPositionPnL(ctx context.Context, id string, realizedPnLUSD float64) error {
	return s.kv().CloseRiskPositionPnL(ctx, id, realizedPnLUSD)
}

func (s *Store) ListRiskPositionsOpenClosing(ctx context.Context, accountID string) ([]RiskPosition, error) {
	return s.kv().ListRiskPositionsOpenClosing(ctx, accountID)
}

func (s *Store) ListRiskTasksRecent(ctx context.Context, limit int) ([]RiskTask, error) {
	return s.kv().ListRiskTasksRecent(ctx, limit)
}

func (s *Store) DeleteRiskTasksTerminal(ctx context.Context) (int64, error) {
	return s.kv().DeleteRiskTasksTerminal(ctx)
}

func (s *Store) ListRiskTasksByReason(ctx context.Context, taskType, reason string, limit int) ([]RiskTask, error) {
	return s.kv().ListRiskTasksByReason(ctx, taskType, reason, limit)
}

func (s *Store) SetRiskTaskRunning(ctx context.Context, id string) error {
	return s.kv().SetRiskTaskRunning(ctx, id)
}

func (s *Store) UpdateRiskTaskLastAttemptDetail(ctx context.Context, id, detailJSON string) error {
	return s.kv().UpdateRiskTaskLastAttemptDetail(ctx, id, detailJSON)
}

func (s *Store) SetRiskTaskFailed(ctx context.Context, id string, attempts int, lastErr string, nextRun time.Time) error {
	return s.kv().SetRiskTaskFailed(ctx, id, attempts, lastErr, nextRun)
}

func (s *Store) SetRiskTaskSucceeded(ctx context.Context, id string) error {
	return s.kv().SetRiskTaskSucceeded(ctx, id)
}

func (s *Store) SetRiskTaskCancelled(ctx context.Context, id, reason string) error {
	return s.kv().SetRiskTaskCancelled(ctx, id, reason)
}

func (s *Store) CancelOtherCloseTasks(ctx context.Context, positionID, exceptTaskID string) error {
	return s.kv().CancelOtherCloseTasks(ctx, positionID, exceptTaskID)
}

func (s *Store) FindOutcomeIDByToken(ctx context.Context, tokenID string) (string, bool, error) {
	return s.kv().FindOutcomeIDByToken(ctx, tokenID)
}

func (s *Store) AccountRealizedPnLSince(ctx context.Context, accountID string, since time.Time) (float64, error) {
	return s.kv().AccountRealizedPnLSince(ctx, accountID, since)
}

func (s *Store) AccountOpenExposureUSD(ctx context.Context, accountID string) (float64, error) {
	return s.kv().AccountOpenExposureUSD(ctx, accountID)
}

func (s *Store) MarketOpenExposureUSD(ctx context.Context, accountID, polyEventSlug string) (float64, error) {
	return s.kv().MarketOpenExposureUSD(ctx, accountID, polyEventSlug)
}

func (s *Store) PolyEventSlugForToken(ctx context.Context, tokenID string) string {
	return s.kv().PolyEventSlugForToken(ctx, tokenID)
}

func (s *Store) MarketStartTimeForToken(ctx context.Context, tokenID string) (time.Time, bool) {
	return s.kv().MarketStartTimeForToken(ctx, tokenID)
}

func (s *Store) UpsertRiskHiddenPosition(ctx context.Context, accountID, tokenID, sideLabel string) error {
	if strings.TrimSpace(accountID) == "" {
		return fmt.Errorf("account_id required")
	}
	if strings.TrimSpace(tokenID) == "" {
		return fmt.Errorf("token_id required")
	}
	return s.kv().UpsertRiskHiddenPosition(ctx, accountID, tokenID, sideLabel)
}

func (s *Store) DeleteRiskHiddenPosition(ctx context.Context, accountID, tokenID, sideLabel string) error {
	if strings.TrimSpace(accountID) == "" {
		return fmt.Errorf("account_id required")
	}
	return s.kv().DeleteRiskHiddenPosition(ctx, accountID, tokenID, sideLabel)
}

func (s *Store) ListRiskHiddenPositions(ctx context.Context, accountID string) ([]RiskHiddenPosition, error) {
	return s.kv().ListRiskHiddenPositions(ctx, accountID)
}

func (s *Store) ListRiskHiddenCompositeKeys(ctx context.Context, accountID string) (map[string]struct{}, error) {
	rows, err := s.kv().ListRiskHiddenPositions(ctx, accountID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]struct{})
	for _, r := range rows {
		out[RiskPositionMonitorKey(r.TokenID, r.SideLabel)] = struct{}{}
	}
	return out, nil
}

func (s *Store) IsRiskPositionHidden(ctx context.Context, accountID, tokenID, sideLabel string) (bool, error) {
	return s.kv().IsRiskPositionHidden(ctx, accountID, tokenID, sideLabel)
}

func (s *Store) CreatePendingTrade(ctx context.Context, marketID, outcomeID, platform, side string, reqSize, reqOdds float64, accountID string) (string, error) {
	return s.kv().CreatePendingTrade(ctx, marketID, outcomeID, platform, side, reqSize, reqOdds, accountID)
}

func (s *Store) MarkTradeFilled(ctx context.Context, id, txHash string, execSize, fillOdds float64) error {
	return s.kv().MarkTradeFilled(ctx, id, txHash, execSize, fillOdds)
}

func (s *Store) MarkTradeFailed(ctx context.Context, id, reason string) error {
	return s.kv().MarkTradeFailed(ctx, id, reason)
}

func (s *Store) ListTrades(ctx context.Context, page, limit int, accountID string) (total int, trades []map[string]any, err error) {
	return s.kv().ListTrades(ctx, page, limit, accountID)
}

func (s *Store) ListPolymarketOutcomeTokenIDs(ctx context.Context) ([]string, error) {
	return s.kv().ListPolymarketOutcomeTokenIDs(ctx)
}

func (s *Store) GetLastTradeSummary(ctx context.Context) (*LastTradeSummary, error) {
	return s.kv().GetLastTradeSummary(ctx)
}

func (s *Store) InsertTradeQuality(ctx context.Context, q *TradeQuality) error {
	return s.kv().InsertTradeQuality(ctx, q)
}

func (s *Store) AggregateTradeQuality(ctx context.Context, accountID string, since time.Time) (TradeQualityAggregate, error) {
	return s.kv().AggregateTradeQuality(ctx, accountID, since)
}

func (s *Store) RealizedPnLByEvent(ctx context.Context, accountID string, since time.Time, limit int) ([]EventRealizedPnL, error) {
	return s.kv().RealizedPnLByEvent(ctx, accountID, since, limit)
}

func (s *Store) ListRecentTradeQuality(ctx context.Context, accountID string, limit int) ([]TradeQuality, error) {
	return s.kv().ListRecentTradeQuality(ctx, accountID, limit)
}

func (s *Store) GetActivePolymarketAccount(ctx context.Context) (*PolymarketAccount, error) {
	aid, err := accountsfile.Default().ReadActiveAccountID(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(aid) == "" {
		return nil, nil
	}
	return accountsfile.Default().ReadAccount(ctx, aid)
}

func (s *Store) GetSingletonPolymarketAccount(ctx context.Context) (*PolymarketAccount, error) {
	list, err := accountsfile.Default().ReadAccounts(ctx)
	if err != nil {
		return nil, err
	}
	if len(list) != 1 {
		return nil, nil
	}
	a := list[0]
	return &a, nil
}

func (s *Store) ListPolymarketAccounts(ctx context.Context) ([]PolymarketAccount, error) {
	return accountsfile.Default().ReadAccounts(ctx)
}

func (s *Store) InsertPolymarketAccount(ctx context.Context, a *PolymarketAccount) error {
	return accountsfile.Default().InsertPolymarketAccount(ctx, a)
}

func (s *Store) DeactivateAllAccounts(ctx context.Context) error {
	return accountsfile.Default().DeactivateAllAccounts(ctx)
}

func (s *Store) ActivateAccount(ctx context.Context, id string) error {
	return accountsfile.Default().ActivateAccount(ctx, id)
}

func (s *Store) DeletePolymarketAccount(ctx context.Context, id string) (int64, error) {
	return accountsfile.Default().DeletePolymarketAccount(ctx, id)
}

func (s *Store) CountPolymarketAccounts(ctx context.Context) (int, error) {
	return accountsfile.Default().CountPolymarketAccounts(ctx)
}

func (s *Store) ListRouterPolySiblings(ctx context.Context, primaryOutcomeID string) ([]RouterOutcome, error) {
	rows, err := s.kv().ListRouterPolySiblings(ctx, primaryOutcomeID)
	if err != nil {
		return nil, err
	}
	out := make([]RouterOutcome, 0, len(rows))
	for _, r := range rows {
		out = append(out, RouterOutcome{
			OutcomeID:        r.OutcomeID,
			Label:            r.Label,
			ExternalID:       sqlNonEmpty(r.ExternalID),
			CurrentOdds:      r.CurrentOdds,
			LiquidityDepth:   r.LiquidityDepth,
			LiquidityLevels:  sqlNonEmpty(r.LiquidityLevels),
			CanonicalBetID:   sqlNonEmpty(r.CanonicalBetID),
			MarketID:         r.MarketID,
			MarketExternalID: r.MarketExternalID,
			MarketPlatform:   r.MarketPlatform,
			MarketStatus:     r.MarketStatus,
		})
	}
	return out, nil
}

func (s *Store) RiskDisplayMetaForPositions(ctx context.Context, positions []RiskPosition) (map[string]RiskDisplayMeta, error) {
	uniq := make([]string, 0, len(positions))
	seen := make(map[string]struct{}, len(positions))
	tokToOutcome := make(map[string]string)
	for _, p := range positions {
		tid := strings.TrimSpace(p.TokenID)
		if tid == "" {
			continue
		}
		if _, ok := seen[tid]; !ok {
			seen[tid] = struct{}{}
			uniq = append(uniq, tid)
		}
		if p.OutcomeID.Valid && strings.TrimSpace(p.OutcomeID.String) != "" {
			tokToOutcome[tid] = strings.TrimSpace(p.OutcomeID.String)
		}
	}
	if len(uniq) == 0 {
		return map[string]RiskDisplayMeta{}, nil
	}
	parts, err := s.kv().RiskDisplayMetaBatch(ctx, uniq, tokToOutcome)
	if err != nil {
		return nil, err
	}
	out := make(map[string]RiskDisplayMeta, len(parts))
	for k, v := range parts {
		out[k] = RiskDisplayMeta{
			TokenID: k, HomeTeam: v.HomeTeam, AwayTeam: v.AwayTeam, Sport: v.Sport,
			PolyEventID: v.PolyEventID, PolySlug: v.PolySlug,
		}
	}
	return out, nil
}

func sqlNonEmpty(s string) sql.NullString {
	s = strings.TrimSpace(s)
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
