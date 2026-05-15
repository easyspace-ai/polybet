package store

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"strings"
)

// NormalizeRiskCLOBTokenID matches app/httpserver poly_ws token normalization:
// decimal uint256 strings become 0x + 64 hex; hex ids are lowercased and left-padded to 66 chars.
func NormalizeRiskCLOBTokenID(tokenID string) string {
	id := strings.TrimSpace(tokenID)
	if id == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(id), "0x") {
		id = strings.ToLower(id)
	} else {
		if n, ok := new(big.Int).SetString(id, 10); ok {
			id = "0x" + fmt.Sprintf("%064x", n)
		} else {
			id = "0x" + strings.ToLower(strings.TrimPrefix(id, "0x"))
		}
	}
	if len(id) < 66 && strings.HasPrefix(id, "0x") {
		id = "0x" + strings.Repeat("0", 66-len(id)) + id[2:]
	}
	return id
}

func riskHiddenCompositeKey(tokenID, sideLabel string) string {
	return NormalizeRiskCLOBTokenID(tokenID) + "\x1f" + sideLabel
}

// RiskPositionMonitorKey is the composite key for hidden-from-monitoring rows (token + side).
func RiskPositionMonitorKey(tokenID, sideLabel string) string {
	return riskHiddenCompositeKey(tokenID, sideLabel)
}

// UpsertRiskHiddenPosition records that a (token, side) row should not appear in risk monitoring for the account.
func (s *Store) UpsertRiskHiddenPosition(ctx context.Context, accountID, tokenID, sideLabel string) error {
	if strings.TrimSpace(accountID) == "" {
		return fmt.Errorf("account_id required")
	}
	if strings.TrimSpace(tokenID) == "" {
		return fmt.Errorf("token_id required")
	}
	tid := NormalizeRiskCLOBTokenID(tokenID)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO risk_hidden_positions(account_id, token_id, side_label, created_at)
		VALUES(?,?,?,datetime('now'))
		ON CONFLICT(account_id, token_id, side_label) DO UPDATE SET created_at = excluded.created_at`,
		accountID, tid, sideLabel)
	return err
}

// DeleteRiskHiddenPosition removes a hidden marker so the row can appear again.
func (s *Store) DeleteRiskHiddenPosition(ctx context.Context, accountID, tokenID, sideLabel string) error {
	if strings.TrimSpace(accountID) == "" {
		return fmt.Errorf("account_id required")
	}
	tid := NormalizeRiskCLOBTokenID(tokenID)
	_, err := s.db.ExecContext(ctx, `DELETE FROM risk_hidden_positions WHERE account_id = ? AND token_id = ? AND side_label = ?`,
		accountID, tid, sideLabel)
	return err
}

// RiskHiddenPosition is one persisted hide row (debug / unhide tooling).
type RiskHiddenPosition struct {
	AccountID  string
	TokenID    string
	SideLabel  string
	CreatedAt  string
}

// ListRiskHiddenPositions returns all hidden keys for an account (newest first).
func (s *Store) ListRiskHiddenPositions(ctx context.Context, accountID string) ([]RiskHiddenPosition, error) {
	if strings.TrimSpace(accountID) == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT account_id, token_id, side_label, created_at
		FROM risk_hidden_positions WHERE account_id = ? ORDER BY created_at DESC`,
		accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RiskHiddenPosition
	for rows.Next() {
		var r RiskHiddenPosition
		if err := rows.Scan(&r.AccountID, &r.TokenID, &r.SideLabel, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListRiskHiddenCompositeKeys returns composite keys tokenID\x1fsideLabel for matching risk rows.
func (s *Store) ListRiskHiddenCompositeKeys(ctx context.Context, accountID string) (map[string]struct{}, error) {
	out := make(map[string]struct{})
	if strings.TrimSpace(accountID) == "" {
		return out, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT token_id, side_label FROM risk_hidden_positions WHERE account_id = ?`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var tid, side string
		if err := rows.Scan(&tid, &side); err != nil {
			return nil, err
		}
		out[riskHiddenCompositeKey(tid, side)] = struct{}{}
	}
	return out, rows.Err()
}

// IsRiskPositionHidden reports whether this account's (token, side) is hidden from monitoring.
func (s *Store) IsRiskPositionHidden(ctx context.Context, accountID, tokenID, sideLabel string) (bool, error) {
	if strings.TrimSpace(accountID) == "" {
		return false, nil
	}
	tid := NormalizeRiskCLOBTokenID(tokenID)
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM risk_hidden_positions WHERE account_id = ? AND token_id = ? AND side_label = ?`,
		accountID, tid, sideLabel).Scan(&n)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
