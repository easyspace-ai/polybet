package store

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/easyspace-ai/polybet/internal/domain"
)

func (s *Store) CreatePendingTrade(ctx context.Context, marketID, outcomeID, platform, side string, reqSize, reqOdds float64, accountID string) (string, error) {
	id := domain.NewID()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO trades(id, market_id, outcome_id, side, requested_size, requested_odds, platform, status, created_at, account_id)
		VALUES(?,?,?,?,?,?,?,'pending',?,?)`,
		id, marketID, outcomeID, side, reqSize, reqOdds, platform, now, accountID)
	return id, err
}

func (s *Store) MarkTradeFilled(ctx context.Context, id, txHash string, execSize, fillOdds float64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE trades SET status = 'filled', executed_size = ?, fill_odds = ?, tx_hash = ? WHERE id = ?`,
		execSize, fillOdds, txHash, id)
	return err
}

func (s *Store) MarkTradeFailed(ctx context.Context, id, reason string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE trades SET status = 'failed', failure_reason = ? WHERE id = ?`, reason, id)
	return err
}

func (s *Store) ListTrades(ctx context.Context, page, limit int, accountID string) (total int, trades []map[string]any, err error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM trades WHERE account_id = ?`, accountID).Scan(&total)
	if err != nil {
		return 0, nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.created_at, t.side, t.requested_size, t.executed_size, t.requested_odds, t.fill_odds, t.platform, t.status, t.tx_hash, t.failure_reason,
		       o.label, e.home_team, e.away_team, e.poly_slug, e.sport
		FROM trades t
		JOIN outcomes o ON t.outcome_id = o.id
		JOIN markets m ON t.market_id = m.id
		JOIN events e ON m.event_id = e.id
		WHERE t.account_id = ?
		ORDER BY t.created_at DESC LIMIT ? OFFSET ?`, accountID, limit, offset)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	trades = make([]map[string]any, 0)
	for rows.Next() {
		var id, created, side, platform, status, outcomeLabel, home, away string
		var txh, fail, polySlug sql.NullString
		var sport sql.NullString
		var reqS, reqO float64
		var execS, fillO sql.NullFloat64
		if err := rows.Scan(&id, &created, &side, &reqS, &execS, &reqO, &fillO, &platform, &status, &txh, &fail, &outcomeLabel, &home, &away, &polySlug, &sport); err != nil {
			return 0, nil, err
		}
		marketName := home + " vs " + away
		var officialUrl string
		if polySlug.Valid && polySlug.String != "" {
			slug := strings.TrimPrefix(strings.TrimSpace(polySlug.String), "event/")
			officialUrl = "https://polymarket.com/event/" + slug
		}
		trades = append(trades, map[string]any{
			"id": id, "createdAt": created, "side": side, "requestedSize": reqS, "executedSize": nullFloat(execS),
			"requestedOdds": reqO, "fillOdds": nullFloat(fillO), "platform": platform, "status": status,
			"txHash": nullStrOrNil(txh), "failureReason": nullStrPtr(fail),
			"outcomeLabel": outcomeLabel, "marketName": marketName,
			"officialUrl": officialUrl, "sport": nullStrPtr(sport),
		})
	}
	return total, trades, rows.Err()
}

func nullFloat(n sql.NullFloat64) any {
	if !n.Valid {
		return nil
	}
	return n.Float64
}

func nullStrPtr(n sql.NullString) any {
	if !n.Valid {
		return nil
	}
	return n.String
}

func nullStrOrNil(n sql.NullString) any {
	if !n.Valid {
		return nil
	}
	return n.String
}
