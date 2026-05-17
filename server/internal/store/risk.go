package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type RiskPosition struct {
	ID             string
	Platform       string
	AccountID      string
	OutcomeID      sql.NullString
	TokenID        string
	Title          string
	SideLabel      string
	PolyEventSlug  string
	PolyMarketSlug string
	AvgEntryCents  float64
	SizeShares     float64
	CostUSD        float64
	HighWaterCents float64 // from LEFT JOIN risk_position_configs (COALESCE)
	StopLossPct    float64 // from LEFT JOIN risk_position_configs (COALESCE)
	Source         string
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type RiskPositionConfig struct {
	PositionID     string
	HighWaterCents float64
	StopLossPct    float64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type RiskTask struct {
	ID                string
	Type              string
	PositionID        sql.NullString
	Status            string
	Attempts          int
	LastError         sql.NullString
	Reason            sql.NullString
	LastAttemptDetail sql.NullString // JSON: last FOK submit snapshot or pre-submit abort context
	NextRunAt         time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (s *Store) InsertRiskAppliedTrade(ctx context.Context, id string, accountID string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO risk_applied_clob_trades(id, account_id) VALUES(?,?)`, id, accountID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) ListOpenRiskPositionsByToken(ctx context.Context, tokenID string, accountID string) ([]RiskPosition, error) {
	return s.listRiskPositionsWhere(ctx, `token_id = ? AND account_id = ? AND status = 'open'`, tokenID, accountID)
}

// ListOpenRiskPositionTokenIDs returns distinct token_ids for open risk positions
// across all accounts (legacy; prefer ListOpenRiskPositionTokenIDsForAccount).
func (s *Store) ListOpenRiskPositionTokenIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT token_id FROM risk_positions WHERE status = 'open' AND token_id IS NOT NULL AND token_id != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ListOpenRiskPositionTokenIDsForAccount returns distinct token_ids for open risk
// positions for a single Polymarket account (active mobile stop-loss scope).
func (s *Store) ListOpenRiskPositionTokenIDsForAccount(ctx context.Context, accountID string) ([]string, error) {
	if strings.TrimSpace(accountID) == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT token_id FROM risk_positions WHERE status = 'open' AND account_id = ? AND token_id IS NOT NULL AND token_id != ''`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) ListOpenRiskPositionsMinShares(ctx context.Context, minShares float64, accountID string) ([]RiskPosition, error) {
	return s.listRiskPositionsWhere(ctx, `status = 'open' AND account_id = ? AND size_shares >= ?`, accountID, minShares)
}

// CountOpenRiskPositionsMinShares counts open positions at or above minShares for the account.
func (s *Store) CountOpenRiskPositionsMinShares(ctx context.Context, minShares float64, accountID string) (int64, error) {
	if strings.TrimSpace(accountID) == "" {
		return 0, nil
	}
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM risk_positions WHERE status = 'open' AND account_id = ? AND size_shares >= ?`,
		accountID, minShares,
	).Scan(&n)
	return n, err
}

func retryOnUniqueOpenRisk(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "idx_risk_positions_open_key") ||
		strings.Contains(msg, "UNIQUE constraint failed")
}

func (s *Store) ListOpenOrClosingRiskPositions(ctx context.Context, accountID string) ([]RiskPosition, error) {
	return s.listRiskPositionsWhere(ctx, `status IN ('open','closing') AND account_id = ?`, accountID)
}

func (s *Store) listRiskPositionsWhere(ctx context.Context, clause string, args ...any) ([]RiskPosition, error) {
	q := `SELECT rp.id, rp.platform, rp.outcome_id, rp.token_id, rp.title, rp.side_label,
		COALESCE(rp.poly_event_slug, ''), COALESCE(rp.poly_market_slug, ''),
		rp.avg_entry_cents, rp.size_shares, rp.cost_usd,
		COALESCE(rpc.high_water_cents, rp.avg_entry_cents) as high_water_cents,
		COALESCE(rpc.stop_loss_pct, 10) as stop_loss_pct,
		rp.source, rp.status, rp.created_at, rp.updated_at
	FROM risk_positions rp
	LEFT JOIN risk_position_configs rpc ON rp.id = rpc.position_id
	WHERE ` + clause
	rows, err := s.db.QueryContext(ctx, q, args...)
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
		if err := rows.Scan(&p.ID, &p.Platform, &oc, &p.TokenID, &p.Title, &p.SideLabel, &p.PolyEventSlug, &p.PolyMarketSlug, &p.AvgEntryCents, &p.SizeShares, &p.CostUSD, &p.HighWaterCents, &p.StopLossPct, &p.Source, &p.Status, &ca, &ua); err != nil {
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
	row := s.db.QueryRowContext(ctx, `SELECT rp.id, rp.platform, rp.outcome_id, rp.token_id, rp.title, rp.side_label,
		COALESCE(rp.poly_event_slug, ''), COALESCE(rp.poly_market_slug, ''),
		rp.avg_entry_cents, rp.size_shares, rp.cost_usd,
		COALESCE(rpc.high_water_cents, rp.avg_entry_cents) as high_water_cents,
		COALESCE(rpc.stop_loss_pct, 10) as stop_loss_pct,
		rp.source, rp.status, rp.created_at, rp.updated_at
	FROM risk_positions rp
	LEFT JOIN risk_position_configs rpc ON rp.id = rpc.position_id
	WHERE rp.id = ?`, id)
	var p RiskPosition
	var oc sql.NullString
	var ca, ua string
	if err := row.Scan(&p.ID, &p.Platform, &oc, &p.TokenID, &p.Title, &p.SideLabel, &p.PolyEventSlug, &p.PolyMarketSlug, &p.AvgEntryCents, &p.SizeShares, &p.CostUSD, &p.HighWaterCents, &p.StopLossPct, &p.Source, &p.Status, &ca, &ua); err != nil {
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

func (s *Store) UpdateRiskPositionPolySlugs(ctx context.Context, id, eventSlug, marketSlug string) error {
	eventSlug = strings.Trim(strings.TrimPrefix(strings.TrimSpace(eventSlug), "event/"), "/")
	marketSlug = strings.Trim(strings.TrimPrefix(strings.TrimSpace(marketSlug), "event/"), "/")
	_, err := s.db.ExecContext(ctx, `
		UPDATE risk_positions SET
			poly_event_slug = COALESCE(NULLIF(?, ''), poly_event_slug),
			poly_market_slug = COALESCE(NULLIF(?, ''), poly_market_slug),
			updated_at = datetime('now')
		WHERE id = ?`, eventSlug, marketSlug, id)
	return err
}

func (s *Store) UpdateRiskPositionHighWater(ctx context.Context, id string, hw float64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO risk_position_configs(position_id, high_water_cents, stop_loss_pct, created_at, updated_at)
		VALUES(?, ?, COALESCE((SELECT stop_loss_pct FROM risk_position_configs WHERE position_id = ?), 10), datetime('now'), datetime('now'))
		ON CONFLICT(position_id) DO UPDATE SET high_water_cents = excluded.high_water_cents, updated_at = datetime('now')`,
		id, hw, id)
	return err
}

// ErrRiskPatchNoFields is returned when PATCH body omits both fields.
var ErrRiskPatchNoFields = errors.New("risk_patch_no_fields")

func (s *Store) UpdateRiskPositionStop(ctx context.Context, id string, stopLossPct *float64, highWaterCents *float64) error {
	if stopLossPct == nil && highWaterCents == nil {
		return ErrRiskPatchNoFields
	}
	if stopLossPct != nil && highWaterCents != nil {
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO risk_position_configs(position_id, high_water_cents, stop_loss_pct, created_at, updated_at)
			VALUES(?, ?, ?, datetime('now'), datetime('now'))
			ON CONFLICT(position_id) DO UPDATE SET high_water_cents = excluded.high_water_cents, stop_loss_pct = excluded.stop_loss_pct, updated_at = datetime('now')`,
			id, *highWaterCents, *stopLossPct)
		return err
	}
	if stopLossPct != nil {
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO risk_position_configs(position_id, high_water_cents, stop_loss_pct, created_at, updated_at)
			VALUES(?, COALESCE((SELECT high_water_cents FROM risk_position_configs WHERE position_id = ?), ?), ?, datetime('now'), datetime('now'))
			ON CONFLICT(position_id) DO UPDATE SET stop_loss_pct = excluded.stop_loss_pct, updated_at = datetime('now')`,
			id, id, 0, *stopLossPct)
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO risk_position_configs(position_id, high_water_cents, stop_loss_pct, created_at, updated_at)
		VALUES(?, ?, COALESCE((SELECT stop_loss_pct FROM risk_position_configs WHERE position_id = ?), 10), datetime('now'), datetime('now'))
		ON CONFLICT(position_id) DO UPDATE SET high_water_cents = excluded.high_water_cents, updated_at = datetime('now')`,
		id, *highWaterCents, id)
	return err
}

func (s *Store) UpsertRiskPositionConfig(ctx context.Context, cfg *RiskPositionConfig) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO risk_position_configs(position_id, high_water_cents, stop_loss_pct, created_at, updated_at)
		VALUES(?,?,?,?,?)
		ON CONFLICT(position_id) DO UPDATE SET high_water_cents = excluded.high_water_cents, stop_loss_pct = excluded.stop_loss_pct, updated_at = excluded.updated_at`,
		cfg.PositionID, cfg.HighWaterCents, cfg.StopLossPct, now, now)
	return err
}

func (s *Store) GetRiskPositionConfig(ctx context.Context, positionID string) (*RiskPositionConfig, error) {
	row := s.db.QueryRowContext(ctx, `SELECT position_id, high_water_cents, stop_loss_pct, created_at, updated_at FROM risk_position_configs WHERE position_id = ?`, positionID)
	var c RiskPositionConfig
	var ca, ua string
	if err := row.Scan(&c.PositionID, &c.HighWaterCents, &c.StopLossPct, &ca, &ua); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	c.CreatedAt = parseSQLiteTime(ca)
	c.UpdatedAt = parseSQLiteTime(ua)
	return &c, nil
}

func (s *Store) UpdateRiskPositionStatusShares(ctx context.Context, id, status string, shares, cost float64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE risk_positions SET status = ?, size_shares = ?, cost_usd = ?, updated_at = datetime('now') WHERE id = ?`,
		status, shares, cost, id)
	return err
}

func (s *Store) CreateRiskPosition(ctx context.Context, p *RiskPosition) error {
	return s.createRiskPositionOnce(ctx, p, true)
}

func (s *Store) createRiskPositionOnce(ctx context.Context, p *RiskPosition, retryOnUnique bool) error {
	p.TokenID = NormalizeRiskCLOBTokenID(p.TokenID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var existingID string
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM risk_positions
		WHERE COALESCE(account_id,'') = COALESCE(?, '') AND token_id = ? AND side_label = ? AND status IN ('open','closing')
		LIMIT 1`,
		p.AccountID, p.TokenID, p.SideLabel).Scan(&existingID)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `
			UPDATE risk_positions SET
				platform = ?, outcome_id = ?, title = ?, avg_entry_cents = ?, size_shares = ?, cost_usd = ?,
				poly_event_slug = COALESCE(NULLIF(?, ''), poly_event_slug),
				poly_market_slug = COALESCE(NULLIF(?, ''), poly_market_slug),
				source = ?, status = ?, updated_at = ?
			WHERE id = ?`,
			p.Platform, sqlNullable(p.OutcomeID), p.Title, p.AvgEntryCents, p.SizeShares, p.CostUSD,
			strings.TrimSpace(p.PolyEventSlug), strings.TrimSpace(p.PolyMarketSlug),
			p.Source, p.Status, now, existingID)
		if err != nil {
			return err
		}
		var curHW float64
		_ = tx.QueryRowContext(ctx, `
			SELECT COALESCE(rpc.high_water_cents, rp.avg_entry_cents)
			FROM risk_positions rp
			LEFT JOIN risk_position_configs rpc ON rp.id = rpc.position_id
			WHERE rp.id = ?`, existingID).Scan(&curHW)
		newHW := p.HighWaterCents
		if curHW > newHW {
			newHW = curHW
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO risk_position_configs(position_id, high_water_cents, stop_loss_pct, created_at, updated_at)
			VALUES(?,?,?,?,?)
			ON CONFLICT(position_id) DO UPDATE SET
				high_water_cents = MAX(risk_position_configs.high_water_cents, excluded.high_water_cents),
				stop_loss_pct = excluded.stop_loss_pct,
				updated_at = excluded.updated_at`,
			existingID, newHW, p.StopLossPct, now, now)
		if err != nil {
			return err
		}
		p.ID = existingID
		return tx.Commit()
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO risk_positions(id, platform, account_id, outcome_id, token_id, title, side_label, poly_event_slug, poly_market_slug, avg_entry_cents, size_shares, cost_usd, source, status, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.Platform, p.AccountID, sqlNullable(p.OutcomeID), p.TokenID, p.Title, p.SideLabel,
		nullIfEmptyString(strings.TrimSpace(p.PolyEventSlug)), nullIfEmptyString(strings.TrimSpace(p.PolyMarketSlug)),
		p.AvgEntryCents, p.SizeShares, p.CostUSD, p.Source, p.Status, now, now)
	if err != nil {
		if retryOnUnique && retryOnUniqueOpenRisk(err) {
			_ = tx.Rollback()
			return s.createRiskPositionOnce(ctx, p, false)
		}
		return err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO risk_position_configs(position_id, high_water_cents, stop_loss_pct, created_at, updated_at)
		VALUES(?,?,?,?,?)`,
		p.ID, p.HighWaterCents, p.StopLossPct, now, now)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Store) GetOpenRiskPositionByToken(ctx context.Context, tokenID, accountID string) (*RiskPosition, error) {
	tokenID = NormalizeRiskCLOBTokenID(tokenID)
	row := s.db.QueryRowContext(ctx, `SELECT rp.id, rp.platform, rp.outcome_id, rp.token_id, rp.title, rp.side_label, rp.avg_entry_cents, rp.size_shares, rp.cost_usd,
		COALESCE(rpc.high_water_cents, rp.avg_entry_cents) as high_water_cents,
		COALESCE(rpc.stop_loss_pct, 10) as stop_loss_pct,
		rp.source, rp.status, rp.created_at, rp.updated_at
	FROM risk_positions rp
	LEFT JOIN risk_position_configs rpc ON rp.id = rpc.position_id
	WHERE rp.token_id = ? AND COALESCE(rp.account_id,'') = COALESCE(?, '') AND rp.status IN ('open','closing')`, tokenID, accountID)
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

func (s *Store) UpdateRiskPositionAvgEntry(ctx context.Context, id string, avgEntryCents float64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE risk_positions SET avg_entry_cents = ?, updated_at = datetime('now') WHERE id = ?`, avgEntryCents, id)
	return err
}

func (s *Store) UpdateRiskPositionTitle(ctx context.Context, id, title, sideLabel string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE risk_positions SET title = ?, side_label = ?, updated_at = datetime('now') WHERE id = ?`, title, sideLabel, id)
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
		INSERT INTO risk_tasks(id, type, position_id, status, attempts, last_error, reason, next_run_at, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.Type, sqlNullable(t.PositionID), t.Status, t.Attempts, sqlNullable(t.LastError), sqlNullable(t.Reason), nr, now, now)
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
		SELECT id, type, position_id, status, attempts, last_error, reason, next_run_at, created_at, updated_at, last_attempt_detail
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
		var reason sql.NullString
		var lad sql.NullString
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

func (s *Store) SetRiskTaskRunning(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE risk_tasks SET status = 'running', updated_at = datetime('now') WHERE id = ?`, id)
	return err
}

// UpdateRiskTaskLastAttemptDetail stores JSON (FOK limit price, shares, book snapshot, abort context) for UI and replay.
func (s *Store) UpdateRiskTaskLastAttemptDetail(ctx context.Context, id, detailJSON string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE risk_tasks SET last_attempt_detail = ?, updated_at = datetime('now') WHERE id = ?`,
		detailJSON, id)
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

func (s *Store) SetRiskTaskCancelled(ctx context.Context, id, reason string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE risk_tasks SET status = 'cancelled', last_error = ?, updated_at = datetime('now') WHERE id = ?`,
		reason, id)
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
