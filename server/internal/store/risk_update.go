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

func (s *Store) CloseRiskPosition(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE risk_positions SET status = 'closed', size_shares = 0, cost_usd = 0, updated_at = datetime('now') WHERE id = ?`, id)
	return err
}

func (s *Store) ListRiskPositionsOpenClosing(ctx context.Context) ([]RiskPosition, error) {
	return s.listRiskPositionsWhere(ctx, `status IN ('open','closing')`, nil)
}

func (s *Store) ListRiskTasksRecent(ctx context.Context, limit int) ([]RiskTask, error) {
	if limit <= 0 {
		limit = 40
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, position_id, status, attempts, last_error, next_run_at, created_at, updated_at
		FROM risk_tasks ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RiskTask, 0)
	for rows.Next() {
		var t RiskTask
		var pid, le sql.NullString
		var nr, ca, ua string
		if err := rows.Scan(&t.ID, &t.Type, &pid, &t.Status, &t.Attempts, &le, &nr, &ca, &ua); err != nil {
			return nil, err
		}
		t.PositionID = pid
		t.LastError = le
		t.NextRunAt = parseSQLiteTime(nr)
		t.CreatedAt = parseSQLiteTime(ca)
		t.UpdatedAt = parseSQLiteTime(ua)
		out = append(out, t)
	}
	return out, rows.Err()
}
