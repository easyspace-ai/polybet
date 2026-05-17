package store

import (
	"context"

	"github.com/easyspace-ai/polybet/internal/domain"
)

// UpsertPolyMarketQuote inserts or updates event, market, canonical bets, and outcomes for one Polymarket quote.
func (s *Store) UpsertPolyMarketQuote(ctx context.Context, q *domain.MarketQuote) error {
	return s.kv().UpsertPolyMarketQuote(ctx, q)
}
