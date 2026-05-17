package store

import (
	"context"
	"database/sql"
)

func (s *Store) SetRiskPositionStatus(ctx context.Context, id, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE risk_positions SET status = ?, updated_at = datetime('now') WHERE id = ?`, status, id)
	return err
}

func (s *Store) UpdateRiskPositionSharesCost(ctx context.Context, id string, shares, cost float64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE risk_positions SET size_shares=?, cost_usd=?, updated_at=datetime('now') WHERE id=?`, shares, cost, id)
	return err
}

// CloseRiskPosition transitions a position to status='closed' without
// recording a realized PnL value. Used by paths where we don't have a
// reliable fill price (e.g. dust normalization, ghost-balance cleanup).
//
// New code that has a realized PnL number should call CloseRiskPositionPnL
// so the kill-switch evaluator can sum closed-today PnL.
func (s *Store) CloseRiskPosition(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE risk_positions SET status = 'closed', size_shares = 0, cost_usd = 0,
			closed_at = COALESCE(closed_at, datetime('now')),
			updated_at = datetime('now')
		WHERE id = ?`, id)
	return err
}

// CloseRiskPositionPnL transitions a position to status='closed' and stores
// the realized PnL (USD; negative = loss). closed_at is set to now if not
// already populated so the kill switch can window queries by closure time.
//
// realizedPnLUSD = sale_proceeds - cost_basis. Callers should compute it
// from the actual fill (FOK exact fill price × shares) or a conservative
// proxy (FAK limit floor × shares) and document the choice in `notes`
// via trade_quality.
func (s *Store) CloseRiskPositionPnL(ctx context.Context, id string, realizedPnLUSD float64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE risk_positions SET status = 'closed', size_shares = 0, cost_usd = 0,
			realized_pnl_usd = ?,
			closed_at = COALESCE(closed_at, datetime('now')),
			updated_at = datetime('now')
		WHERE id = ?`, realizedPnLUSD, id)
	return err
}

func (s *Store) ListRiskPositionsOpenClosing(ctx context.Context, accountID string) ([]RiskPosition, error) {
	return s.listRiskPositionsWhere(ctx, `status IN ('open','closing') AND account_id = ?`, accountID)
}

func (s *Store) ListRiskTasksRecent(ctx context.Context, limit int) ([]RiskTask, error) {
	if limit <= 0 {
		limit = 40
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, position_id, status, attempts, last_error, reason, next_run_at, created_at, updated_at, last_attempt_detail
		FROM risk_tasks ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RiskTask, 0)
	for rows.Next() {
		var t RiskTask
		var pid, le, reason, lad sql.NullString
		var nr, ca, ua string
		if err := rows.Scan(&t.ID, &t.Type, &pid, &t.Status, &t.Attempts, &le, &reason, &nr, &ca, &ua, &lad); err != nil {
			return nil, err
		}
		t.PositionID = pid
		t.LastError = le
		t.Reason = reason
		t.LastAttemptDetail = lad
		t.NextRunAt = parseSQLiteTime(nr)
		t.CreatedAt = parseSQLiteTime(ca)
		t.UpdatedAt = parseSQLiteTime(ua)
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteRiskTasksTerminal removes finished task rows from the visible log:
// status in (succeeded, failed, cancelled). Rows still in progress (pending,
// running) are never deleted.
func (s *Store) DeleteRiskTasksTerminal(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM risk_tasks WHERE status IN ('succeeded','failed','cancelled')`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) ListRiskTasksByReason(ctx context.Context, taskType, reason string, limit int) ([]RiskTask, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, position_id, status, attempts, last_error, reason, next_run_at, created_at, updated_at, last_attempt_detail
		FROM risk_tasks WHERE type = ? AND reason = ? AND status = 'succeeded'
		ORDER BY created_at DESC LIMIT ?`, taskType, reason, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RiskTask, 0)
	for rows.Next() {
		var t RiskTask
		var pid, le, r, lad sql.NullString
		var nr, ca, ua string
		if err := rows.Scan(&t.ID, &t.Type, &pid, &t.Status, &t.Attempts, &le, &r, &nr, &ca, &ua, &lad); err != nil {
			return nil, err
		}
		t.PositionID = pid
		t.LastError = le
		t.Reason = r
		t.LastAttemptDetail = lad
		t.NextRunAt = parseSQLiteTime(nr)
		t.CreatedAt = parseSQLiteTime(ca)
		t.UpdatedAt = parseSQLiteTime(ua)
		out = append(out, t)
	}
	return out, rows.Err()
}
