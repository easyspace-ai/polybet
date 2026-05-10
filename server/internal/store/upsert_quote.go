package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/easyspace-ai/polybet/internal/domain"
	"github.com/easyspace-ai/polybet/internal/routercanon"
)

// UpsertPolyMarketQuote inserts or updates event, market, canonical bets, and outcomes for one Polymarket quote.
func (s *Store) UpsertPolyMarketQuote(ctx context.Context, q *domain.MarketQuote) error {
	if q.Platform != "polymarket" {
		return fmt.Errorf("only polymarket quotes supported")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	st := q.StartTime.UTC().Format(time.RFC3339Nano)
	var eventID string
	err = tx.QueryRowContext(ctx, `SELECT id FROM events WHERE poly_event_id = ? LIMIT 1`, q.PolyEventID).Scan(&eventID)
	if err == sql.ErrNoRows {
		eventID = "e_poly_" + q.PolyEventID
		_, err = tx.ExecContext(ctx, `
			INSERT INTO events(id, sport, league, home_team, away_team, start_time, status, poly_event_id)
			VALUES(?,?,?,?,?,?, 'active', ?)`,
			eventID, q.Sport, q.League, q.HomeTeam, q.AwayTeam, st, q.PolyEventID)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		_, err = tx.ExecContext(ctx, `
			UPDATE events SET sport=?, league=?, home_team=?, away_team=?, start_time=?, status='active' WHERE id=?`,
			q.Sport, q.League, q.HomeTeam, q.AwayTeam, st, eventID)
		if err != nil {
			return err
		}
	}

	marketID := "m_poly_" + q.ExternalID
	lineVal := interface{}(nil)
	if q.Line != nil {
		lineVal = *q.Line
	}
	main := 0
	if q.MainLine {
		main = 1
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO markets(id, event_id, platform, external_id, start_time, bet_type, line, main_line, status)
		VALUES(?,?,?,?,?,?,?,?, 'active')
		ON CONFLICT(platform, external_id) DO UPDATE SET
			event_id=excluded.event_id, start_time=excluded.start_time, bet_type=excluded.bet_type,
			line=excluded.line, main_line=excluded.main_line, status='active'`,
		marketID, eventID, "polymarket", q.ExternalID, st, q.BetType, lineVal, main)
	if err != nil {
		return err
	}

	for _, oc := range q.Outcomes {
		cr := routercanon.Canonicalize(oc.Label, q.BetType, q.HomeTeam, q.AwayTeam)
		if cr.Parts == nil {
			slog.Warn("outcome_canonicalize_skipped",
				"poly_event_id", q.PolyEventID, "market_external_id", q.ExternalID,
				"label", oc.Label, "bet_type", q.BetType, "home", q.HomeTeam, "away", q.AwayTeam,
				"reason", cr.Reason)
			continue
		}
		canonID := "cb_" + eventID + "_" + cr.Parts.Key
		_, err = tx.ExecContext(ctx, `
			INSERT INTO canonical_bets(id, event_id, key, bet_type, side, line)
			VALUES(?,?,?,?,?,?)
			ON CONFLICT(event_id, key) DO NOTHING`,
			canonID, eventID, cr.Parts.Key, cr.Parts.BetType, cr.Parts.Side, lineOrNil(cr.Parts.Line))
		if err != nil {
			return err
		}

		levelsJSON := ""
		if len(oc.LiquidityDepth.TopLevels) > 0 {
			b, _ := json.Marshal(oc.LiquidityDepth.TopLevels)
			levelsJSON = string(b)
		}
		outcomeID := "o_poly_" + oc.ExternalID
		_, err = tx.ExecContext(ctx, `
			INSERT INTO outcomes(id, market_id, label, external_id, current_odds, liquidity_depth, liquidity_levels, last_updated, canonical_bet_id)
			VALUES(?,?,?,?,?,?,?,datetime('now'), ?)
			ON CONFLICT(id) DO UPDATE SET
				label=excluded.label, current_odds=excluded.current_odds, liquidity_depth=excluded.liquidity_depth,
				liquidity_levels=excluded.liquidity_levels, last_updated=datetime('now'), canonical_bet_id=excluded.canonical_bet_id`,
			outcomeID, marketID, oc.Label, oc.ExternalID, oc.ImpliedOdds, oc.LiquidityDepth.AvailableSize, nullIfEmpty(levelsJSON), canonID)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func lineOrNil(l *float64) interface{} {
	if l == nil {
		return nil
	}
	return *l
}

func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
