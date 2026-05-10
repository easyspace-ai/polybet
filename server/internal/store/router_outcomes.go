package store

import (
	"context"
	"database/sql"
)

// RouterOutcome is one row for allocation routing (Polymarket siblings only).
type RouterOutcome struct {
	OutcomeID         string
	Label             string
	ExternalID        sql.NullString
	CurrentOdds       float64
	LiquidityDepth    float64
	LiquidityLevels   sql.NullString
	CanonicalBetID    sql.NullString
	MarketID          string
	MarketExternalID  string
	MarketPlatform    string
	MarketStatus      string
}

// ListRouterPolySiblings returns active Polymarket outcomes sharing the same canonical bet
// as the given primary outcome (or only the primary if canonical is null).
func (s *Store) ListRouterPolySiblings(ctx context.Context, primaryOutcomeID string) ([]RouterOutcome, error) {
	row := s.db.QueryRowContext(ctx, `SELECT canonical_bet_id FROM outcomes WHERE id = ?`, primaryOutcomeID)
	var canon sql.NullString
	if err := row.Scan(&canon); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	var rows *sql.Rows
	var err error
	if canon.Valid && canon.String != "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT o.id, o.label, o.external_id, o.current_odds, o.liquidity_depth, o.liquidity_levels, o.canonical_bet_id,
			       m.id, m.external_id, m.platform, m.status
			FROM outcomes o
			JOIN markets m ON o.market_id = m.id
			WHERE o.canonical_bet_id = ? AND m.platform = 'polymarket' AND m.status = 'active'`,
			canon.String)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT o.id, o.label, o.external_id, o.current_odds, o.liquidity_depth, o.liquidity_levels, o.canonical_bet_id,
			       m.id, m.external_id, m.platform, m.status
			FROM outcomes o
			JOIN markets m ON o.market_id = m.id
			WHERE o.id = ? AND m.platform = 'polymarket' AND m.status = 'active'`,
			primaryOutcomeID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RouterOutcome
	for rows.Next() {
		var r RouterOutcome
		if err := rows.Scan(&r.OutcomeID, &r.Label, &r.ExternalID, &r.CurrentOdds, &r.LiquidityDepth, &r.LiquidityLevels,
			&r.CanonicalBetID, &r.MarketID, &r.MarketExternalID, &r.MarketPlatform, &r.MarketStatus); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
