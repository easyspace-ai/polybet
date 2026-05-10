package store

import (
	"context"
	"database/sql"
	"time"
)

type PolymarketAccount struct {
	ID            string // set on insert; omitted when scanning if empty (legacy)
	Name          string
	APIKey        string
	Secret        string
	Passphrase    string
	PrivateKey    string
	FunderAddress string
	IsActive      bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (s *Store) GetActivePolymarketAccount(ctx context.Context) (*PolymarketAccount, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, api_key, secret, passphrase, private_key, funder_address, is_active, created_at, updated_at
		FROM polymarket_accounts WHERE is_active = 1 LIMIT 1`)
	var a PolymarketAccount
	var created, updated string
	var active int
	if err := row.Scan(&a.ID, &a.Name, &a.APIKey, &a.Secret, &a.Passphrase, &a.PrivateKey, &a.FunderAddress, &active, &created, &updated); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	a.IsActive = active != 0
	a.CreatedAt, _ = time.Parse(time.RFC3339, created)
	a.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return &a, nil
}

// GetSingletonPolymarketAccount returns the only row when the table has exactly one account (ignores is_active).
// Used so a single imported account works even if no row was ever marked active.
func (s *Store) GetSingletonPolymarketAccount(ctx context.Context) (*PolymarketAccount, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM polymarket_accounts`).Scan(&n); err != nil {
		return nil, err
	}
	if n != 1 {
		return nil, nil
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, api_key, secret, passphrase, private_key, funder_address, is_active, created_at, updated_at
		FROM polymarket_accounts ORDER BY created_at DESC LIMIT 1`)
	var a PolymarketAccount
	var created, updated string
	var active int
	if err := row.Scan(&a.ID, &a.Name, &a.APIKey, &a.Secret, &a.Passphrase, &a.PrivateKey, &a.FunderAddress, &active, &created, &updated); err != nil {
		return nil, err
	}
	a.IsActive = active != 0
	a.CreatedAt, _ = time.Parse(time.RFC3339, created)
	a.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return &a, nil
}

func (s *Store) ListPolymarketAccounts(ctx context.Context) ([]PolymarketAccount, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, api_key, secret, passphrase, private_key, funder_address, is_active, created_at, updated_at
		FROM polymarket_accounts ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]PolymarketAccount, 0)
	for rows.Next() {
		var a PolymarketAccount
		var created, updated string
		var active int
		if err := rows.Scan(&a.ID, &a.Name, &a.APIKey, &a.Secret, &a.Passphrase, &a.PrivateKey, &a.FunderAddress, &active, &created, &updated); err != nil {
			return nil, err
		}
		a.IsActive = active != 0
		a.CreatedAt, _ = time.Parse(time.RFC3339, created)
		a.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) DeactivateAllAccounts(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE polymarket_accounts SET is_active = 0`)
	return err
}

func (s *Store) ActivateAccount(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE polymarket_accounts SET is_active = 0`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE polymarket_accounts SET is_active = 1, updated_at = datetime('now') WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeletePolymarketAccount(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM polymarket_accounts WHERE id = ?`, id)
	return err
}

func (s *Store) CountPolymarketAccounts(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM polymarket_accounts`).Scan(&n)
	return n, err
}

func (s *Store) InsertPolymarketAccount(ctx context.Context, a *PolymarketAccount) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	active := 0
	if a.IsActive {
		active = 1
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO polymarket_accounts(id, name, api_key, secret, passphrase, private_key, funder_address, is_active, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.Name, a.APIKey, a.Secret, a.Passphrase, a.PrivateKey, a.FunderAddress, active, now, now)
	return err
}
