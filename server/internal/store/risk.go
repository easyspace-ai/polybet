package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type RiskPosition struct {
	ID             string
	Platform       string
	OutcomeID      sql.NullString
	TokenID        string
	Title          string
	SideLabel      string
	AvgEntryCents  float64
	SizeShares     float64
	CostUSD        float64
	HighWaterCents float64
	StopLossPct    float64
	Source         string
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type RiskTask struct {
	ID         string
	Type       string
	PositionID sql.NullString
	Status     string
	Attempts   int
	LastError  sql.NullString
	NextRunAt  time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (s *Store) InsertRiskAppliedTrade(ctx context.Context, id string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO risk_applied_clob_trades(id) VALUES(?)`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) ListOpenRiskPositionsByToken(ctx context.Context, tokenID string) ([]RiskPosition, error) {
	return s.listRiskPositionsWhere(ctx, `token_id = ? AND status = 'open'`, tokenID)
}

func (s *Store) ListOpenRiskPositionsMinShares(ctx context.Context, minShares float64) ([]RiskPosition, error) {
	return s.listRiskPositionsWhere(ctx, `status = 'open' AND size_shares >= ?`, minShares)
}

func (s *Store) ListOpenOrClosingRiskPositions(ctx context.Context) ([]RiskPosition, error) {
	return s.listRiskPositionsWhere(ctx, `status IN ('open','closing')`, nil)
}

func (s *Store) listRiskPositionsWhere(ctx context.Context, clause string, arg any) ([]RiskPosition, error) {
	q := `SELECT id, platform, outcome_id, token_id, title, side_label, avg_entry_cents, size_shares, cost_usd,
		high_water_cents, stop_loss_pct, source, status, created_at, updated_at FROM risk_positions WHERE ` + clause
	var rows *sql.Rows
	var err error
	if arg != nil {
		rows, err = s.db.QueryContext(ctx, q, arg)
	} else {
		rows, err = s.db.QueryContext(ctx, q)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRiskPositions(rows)
}

func scanRiskPositions(rows *sql.Rows) ([]RiskPosition, error) {
	out := make([]RiskPosition, 0)
	for rows.Next() {
		var p RiskPosition
		var oc sql.NullString
		var ca, ua string
		if err := rows.Scan(&p.ID, &p.Platform, &oc, &p.TokenID, &p.Title, &p.SideLabel, &p.AvgEntryCents, &p.SizeShares, &p.CostUSD, &p.HighWaterCents, &p.StopLossPct, &p.Source, &p.Status, &ca, &ua); err != nil {
			return nil, err
		}
		p.OutcomeID = oc
		p.CreatedAt = parseSQLiteTime(ca)
		p.UpdatedAt = parseSQLiteTime(ua)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetRiskPosition(ctx context.Context, id string) (*RiskPosition, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, platform, outcome_id, token_id, title, side_label, avg_entry_cents, size_shares, cost_usd,
		high_water_cents, stop_loss_pct, source, status, created_at, updated_at FROM risk_positions WHERE id = ?`, id)
	var p RiskPosition
	var oc sql.NullString
	var ca, ua string
	if err := row.Scan(&p.ID, &p.Platform, &oc, &p.TokenID, &p.Title, &p.SideLabel, &p.AvgEntryCents, &p.SizeShares, &p.CostUSD, &p.HighWaterCents, &p.StopLossPct, &p.Source, &p.Status, &ca, &ua); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	p.OutcomeID = oc
	p.CreatedAt = parseSQLiteTime(ca)
	p.UpdatedAt = parseSQLiteTime(ua)
	return &p, nil
}

func (s *Store) UpdateRiskPositionHighWater(ctx context.Context, id string, hw float64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE risk_positions SET high_water_cents = ?, updated_at = datetime('now') WHERE id = ?`, hw, id)
	return err
}

// ErrRiskPatchNoFields is returned when PATCH body omits both fields.
var ErrRiskPatchNoFields = errors.New("risk_patch_no_fields")

func (s *Store) UpdateRiskPositionStop(ctx context.Context, id string, stopLossPct *float64, highWaterCents *float64) error {
	if stopLossPct == nil && highWaterCents == nil {
		return ErrRiskPatchNoFields
	}
	if stopLossPct != nil && highWaterCents != nil {
		_, err := s.db.ExecContext(ctx, `UPDATE risk_positions SET stop_loss_pct = ?, high_water_cents = ?, updated_at = datetime('now') WHERE id = ?`,
			*stopLossPct, *highWaterCents, id)
		return err
	}
	if stopLossPct != nil {
		_, err := s.db.ExecContext(ctx, `UPDATE risk_positions SET stop_loss_pct = ?, updated_at = datetime('now') WHERE id = ?`, *stopLossPct, id)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE risk_positions SET high_water_cents = ?, updated_at = datetime('now') WHERE id = ?`, *highWaterCents, id)
	return err
}

func (s *Store) UpdateRiskPositionStatusShares(ctx context.Context, id, status string, shares, cost float64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE risk_positions SET status = ?, size_shares = ?, cost_usd = ?, updated_at = datetime('now') WHERE id = ?`,
		status, shares, cost, id)
	return err
}

func (s *Store) CreateRiskPosition(ctx context.Context, p *RiskPosition) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO risk_positions(id, platform, outcome_id, token_id, title, side_label, avg_entry_cents, size_shares, cost_usd, high_water_cents, stop_loss_pct, source, status, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.Platform, sqlNullable(p.OutcomeID), p.TokenID, p.Title, p.SideLabel, p.AvgEntryCents, p.SizeShares, p.CostUSD, p.HighWaterCents, p.StopLossPct, p.Source, p.Status, now, now)
	return err
}


func (s *Store) NormalizeDustRisk(ctx context.Context, dust float64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE risk_positions SET status = 'closed', size_shares = 0, cost_usd = 0, updated_at = datetime('now')
		WHERE status IN ('open','closing') AND size_shares <= ?`, dust)
	return err
}

func (s *Store) FindPendingCloseTask(ctx context.Context, positionID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(1) FROM risk_tasks WHERE position_id = ? AND type = 'close_position' AND status IN ('pending','running')`,
		positionID).Scan(&n)
	return n > 0, err
}

func (s *Store) InsertRiskTask(ctx context.Context, t *RiskTask) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	nr := t.NextRunAt.UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO risk_tasks(id, type, position_id, status, attempts, last_error, next_run_at, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		t.ID, t.Type, sqlNullable(t.PositionID), t.Status, t.Attempts, sqlNullable(t.LastError), nr, now, now)
	return err
}

func sqlNullable(ns sql.NullString) any {
	if !ns.Valid {
		return nil
	}
	return ns.String
}

func (s *Store) ListDueRiskTasks(ctx context.Context, limit int) ([]RiskTask, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, position_id, status, attempts, last_error, next_run_at, created_at, updated_at
		FROM risk_tasks WHERE status IN ('pending','failed') AND next_run_at <= ? ORDER BY next_run_at ASC LIMIT ?`,
		now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RiskTask
	for rows.Next() {
		var t RiskTask
		var pid sql.NullString
		var le sql.NullString
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

func (s *Store) SetRiskTaskRunning(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE risk_tasks SET status = 'running', updated_at = datetime('now') WHERE id = ?`, id)
	return err
}

func (s *Store) SetRiskTaskFailed(ctx context.Context, id string, attempts int, lastErr string, nextRun time.Time) error {
	nr := nextRun.UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
		UPDATE risk_tasks SET status = 'failed', attempts = ?, last_error = ?, next_run_at = ?, updated_at = datetime('now') WHERE id = ?`,
		attempts, lastErr, nr, id)
	return err
}

func (s *Store) SetRiskTaskSucceeded(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE risk_tasks SET status = 'succeeded', last_error = NULL, updated_at = datetime('now') WHERE id = ?`, id)
	return err
}

func (s *Store) CancelOtherCloseTasks(ctx context.Context, positionID, exceptTaskID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE risk_tasks SET status = 'cancelled', last_error = 'superseded', updated_at = datetime('now')
		WHERE position_id = ? AND type = 'close_position' AND status IN ('pending','failed') AND id != ?`,
		positionID, exceptTaskID)
	return err
}

func (s *Store) FindOutcomeIDByToken(ctx context.Context, tokenID string) (string, bool, error) {
	var id sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id FROM outcomes WHERE external_id = ? LIMIT 1`, tokenID).Scan(&id)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return id.String, id.Valid, nil
}
