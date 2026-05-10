package store

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
)

func sqlOutcome(outcomeID string) interface{} {
	if outcomeID == "" {
		return nil
	}
	return outcomeID
}

// MergeOpenRiskBuy scales an open position by a new buy (same semantics as Node recordPolymarketBuyFill).
func (s *Store) MergeOpenRiskBuy(ctx context.Context, tokenID string, outcomeID, title, sideLabel string, entryCents, newShares, costUsd float64, stopLossPct float64, source string) error {
	row := s.db.QueryRowContext(ctx, `SELECT id, avg_entry_cents, size_shares, cost_usd, high_water_cents, outcome_id FROM risk_positions WHERE token_id = ? AND status = 'open'`, tokenID)
	var id string
	var avg, shares, cost, hw float64
	var oc sql.NullString
	if err := row.Scan(&id, &avg, &shares, &cost, &hw, &oc); err == sql.ErrNoRows {
		pid := uuid.NewString()
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO risk_positions(id, platform, outcome_id, token_id, title, side_label, avg_entry_cents, size_shares, cost_usd, high_water_cents, stop_loss_pct, source, status, created_at, updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?, 'open', datetime('now'), datetime('now'))`,
			pid, "polymarket", sqlOutcome(outcomeID), tokenID, title, sideLabel, entryCents, newShares, costUsd, entryCents, stopLossPct, source)
		return err
	} else if err != nil {
		return err
	}
	total := shares + newShares
	newAvg := (avg*shares + entryCents*newShares) / total
	newHW := hw
	if entryCents > hw {
		newHW = entryCents
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE risk_positions SET size_shares = ?, cost_usd = cost_usd + ?, avg_entry_cents = ?, high_water_cents = ?, title = ?, side_label = ?, outcome_id = COALESCE(outcome_id, ?), updated_at = datetime('now') WHERE id = ?`,
		total, costUsd, newAvg, newHW, title, sideLabel, sqlOutcome(outcomeID), id)
	return err
}
