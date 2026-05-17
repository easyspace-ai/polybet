package storage

import (
	"context"
	"time"

	"github.com/easyspace-ai/polybet/internal/domain"
	"github.com/easyspace-ai/polybet/internal/store"
)

// Tx is a read/write transaction scope for KV backends (BadgerDB).
type Tx interface {
	Get(key []byte) ([]byte, error)
	Set(key, value []byte) error
	Delete(key []byte) error
	Exists(key []byte) (bool, error)
}

// KVStorage is the low-level key-value abstraction (ADR §4.1).
type KVStorage interface {
	Close() error
	Update(fn func(Tx) error) error
	View(fn func(Tx) error) error
	Get(key []byte) ([]byte, error)
	Set(key, value []byte) error
	Delete(key []byte) error
	Exists(key []byte) (bool, error)
	Scan(prefix []byte, fn func(key, value []byte) error) error
	ScanRange(start, end []byte, fn func(key, value []byte) error) error
}

// RiskStore is the risk-domain surface (ADR §4.2). Method names follow the
// existing store package for easier incremental adoption.
type RiskStore interface {
	GetRiskPosition(ctx context.Context, id string) (*store.RiskPosition, error)
	ListOpenOrClosingRiskPositions(ctx context.Context, accountID string) ([]store.RiskPosition, error)
	ListRiskPositionsOpenClosing(ctx context.Context, accountID string) ([]store.RiskPosition, error)
	ListOpenRiskPositionsByToken(ctx context.Context, tokenID, accountID string) ([]store.RiskPosition, error)
	ListOpenRiskPositionsMinShares(ctx context.Context, minShares float64, accountID string) ([]store.RiskPosition, error)
	CountOpenRiskPositionsMinShares(ctx context.Context, minShares float64, accountID string) (int64, error)
	ListOpenRiskPositionTokenIDs(ctx context.Context) ([]string, error)
	ListOpenRiskPositionTokenIDsForAccount(ctx context.Context, accountID string) ([]string, error)
	CreateRiskPosition(ctx context.Context, p *store.RiskPosition) error
	SetRiskPositionStatus(ctx context.Context, id, status string) error
	UpdateRiskPositionSharesCost(ctx context.Context, id string, shares, cost float64) error
	CloseRiskPosition(ctx context.Context, id string) error
	CloseRiskPositionPnL(ctx context.Context, id string, realizedPnLUSD float64) error
	UpdateRiskPositionHighWater(ctx context.Context, id string, hw float64) error
	UpdateRiskPositionStop(ctx context.Context, id string, stopLossPct *float64, highWaterCents *float64) error
	UpdateRiskPositionPolySlugs(ctx context.Context, id, eventSlug, marketSlug string) error
	NormalizeDustRisk(ctx context.Context, dust float64) error
	AccountOpenExposureUSD(ctx context.Context, accountID string) (float64, error)
	AccountRealizedPnLSince(ctx context.Context, accountID string, since time.Time) (float64, error)
	MarketOpenExposureUSD(ctx context.Context, accountID, polyEventSlug string) (float64, error)
	InsertRiskTask(ctx context.Context, t *store.RiskTask) error
	ListDueRiskTasks(ctx context.Context, limit int) ([]store.RiskTask, error)
	ListRiskTasksRecent(ctx context.Context, limit int) ([]store.RiskTask, error)
	ListRiskTasksByReason(ctx context.Context, taskType, reason string, limit int) ([]store.RiskTask, error)
	FindPendingCloseTask(ctx context.Context, positionID string) (bool, error)
	SetRiskTaskRunning(ctx context.Context, id string) error
	SetRiskTaskFailed(ctx context.Context, id string, attempts int, lastErr string, nextRun time.Time) error
	SetRiskTaskSucceeded(ctx context.Context, id string) error
	SetRiskTaskCancelled(ctx context.Context, id, reason string) error
	CancelOtherCloseTasks(ctx context.Context, positionID, exceptTaskID string) error
	UpdateRiskTaskLastAttemptDetail(ctx context.Context, id, detailJSON string) error
	DeleteRiskTasksTerminal(ctx context.Context) (int64, error)
	InsertRiskAppliedTrade(ctx context.Context, id, accountID string) (bool, error)
	UpsertRiskHiddenPosition(ctx context.Context, accountID, tokenID, sideLabel string) error
	ListRiskHiddenPositions(ctx context.Context, accountID string) ([]store.RiskHiddenPosition, error)
	DeleteRiskHiddenPosition(ctx context.Context, accountID, tokenID, sideLabel string) error
}

// MarketStore is the market-domain surface (ADR §4.2).
type MarketStore interface {
	UpsertPolyMarketQuote(ctx context.Context, q *domain.MarketQuote) error
	ListActiveMarketsFlat(ctx context.Context) ([]store.MarketRow, []store.OutcomeRow, error)
	CountActiveMarkets(ctx context.Context) (int, error)
	CountActiveOutcomes(ctx context.Context) (int, error)
}

// AccountStore is the Polymarket account surface (ADR §4.2).
type AccountStore interface {
	GetActivePolymarketAccount(ctx context.Context) (*store.PolymarketAccount, error)
	GetSingletonPolymarketAccount(ctx context.Context) (*store.PolymarketAccount, error)
	ListPolymarketAccounts(ctx context.Context) ([]store.PolymarketAccount, error)
	InsertPolymarketAccount(ctx context.Context, a *store.PolymarketAccount) error
	ActivateAccount(ctx context.Context, id string) error
	DeactivateAllAccounts(ctx context.Context) error
	DeletePolymarketAccount(ctx context.Context, id string) (int64, error)
	CountPolymarketAccounts(ctx context.Context) (int, error)
}

// ConfigKVStore is bot runtime configuration (ADR config/bot).
type ConfigKVStore interface {
	GetBotConfig(ctx context.Context, key string) (string, bool, error)
	GetBotConfigFloat(ctx context.Context, key string, def float64) float64
	GetBotConfigInt(ctx context.Context, key string, def int) int
	UpsertBotConfig(ctx context.Context, key, value string) error
	InsertBotConfigDefault(ctx context.Context, key, value string) error
	ListBotConfig(ctx context.Context) ([]struct{ Key, Value string }, error)
	SeedDefaultConfig(ctx context.Context) error
}

// TradeQualityStore is execution-quality telemetry (ADR §4.2).
type TradeQualityStore interface {
	InsertTradeQuality(ctx context.Context, q *store.TradeQuality) error
	ListRecentTradeQuality(ctx context.Context, accountID string, limit int) ([]store.TradeQuality, error)
	AggregateTradeQuality(ctx context.Context, accountID string, since time.Time) (store.TradeQualityAggregate, error)
	RealizedPnLByEvent(ctx context.Context, accountID string, since time.Time, limit int) ([]store.EventRealizedPnL, error)
}
