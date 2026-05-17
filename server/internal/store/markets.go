package store

import (
	"context"

	"github.com/easyspace-ai/polybet/internal/storage/badgerdb"
)

// MarketRow and OutcomeRow are the wire shapes used across services.
type MarketRow = badgerdb.MarketRow
type OutcomeRow = badgerdb.OutcomeRow

func (s *Store) ListActiveMarketsFlat(ctx context.Context) ([]MarketRow, []OutcomeRow, error) {
	return s.kv().ListActiveMarketsFlat(ctx)
}

func (s *Store) GetOutcomeWithMarket(ctx context.Context, outcomeID string) (outcomeIDRet, marketID, label, extID, home, away string, err error) {
	return s.kv().GetOutcomeWithMarket(ctx, outcomeID)
}
