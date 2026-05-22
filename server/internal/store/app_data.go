package store

import "context"

// ClearAllAppData wipes persisted markets, positions, trades, and risk tasks.
// Bot config and Polymarket accounts are preserved.
func (s *Store) ClearAllAppData(ctx context.Context) (int, error) {
	return s.kv().ClearAllAppData(ctx)
}
