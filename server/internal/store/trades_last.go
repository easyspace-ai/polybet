package store

import (
	"context"
	"database/sql"
)

// LastTradeSummary is used by Telegram /status (Node parity).
type LastTradeSummary struct {
	Status        string
	Platform      string
	RequestedSize float64
	RequestedOdds float64
	FillOdds      sql.NullFloat64
}

func (s *Store) GetLastTradeSummary(ctx context.Context) (*LastTradeSummary, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT status, platform, requested_size, requested_odds, fill_odds
		FROM trades ORDER BY created_at DESC LIMIT 1`)
	var t LastTradeSummary
	if err := row.Scan(&t.Status, &t.Platform, &t.RequestedSize, &t.RequestedOdds, &t.FillOdds); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}
