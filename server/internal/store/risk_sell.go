package store

import (
	"context"
	"database/sql"
)

// ReduceOpenRiskSell applies a matched CLOB SELL to the open position (same rules as Node).
func (s *Store) ReduceOpenRiskSell(ctx context.Context, tokenID string, size, price float64, minOpen float64) error {
	row := s.db.QueryRowContext(ctx, `SELECT id, size_shares, cost_usd FROM risk_positions WHERE token_id = ? AND status = 'open'`, tokenID)
	var id string
	var shares, cost float64
	if err := row.Scan(&id, &shares, &cost); err == sql.ErrNoRows {
		return nil
	} else if err != nil {
		return err
	}
	next := shares - size
	if next < minOpen {
		_, err := s.db.ExecContext(ctx, `UPDATE risk_positions SET status='closed', size_shares=0, cost_usd=0, updated_at=datetime('now') WHERE id=?`, id)
		return err
	}
	ratio := size / shares
	newCost := max(0, cost*(1-ratio))
	_, err := s.db.ExecContext(ctx, `UPDATE risk_positions SET size_shares=?, cost_usd=?, updated_at=datetime('now') WHERE id=?`, next, newCost, id)
	return err
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
