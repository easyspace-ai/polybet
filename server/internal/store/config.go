package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
)

func (s *Store) GetBotConfig(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM bot_config WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

func (s *Store) GetBotConfigFloat(ctx context.Context, key string, def float64) float64 {
	v, ok, err := s.GetBotConfig(ctx, key)
	if err != nil || !ok {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

func (s *Store) GetBotConfigInt(ctx context.Context, key string, def int) int {
	v, ok, err := s.GetBotConfig(ctx, key)
	if err != nil || !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func (s *Store) UpsertBotConfig(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO bot_config(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value)
	return err
}

// InsertBotConfigDefault inserts a row only when the key is absent.
// Used for first-boot defaults so restarts never clobber user values (e.g. onboardingComplete).
func (s *Store) InsertBotConfigDefault(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO bot_config(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO NOTHING
	`, key, value)
	return err
}

func (s *Store) ListBotConfig(ctx context.Context) ([]struct{ Key, Value string }, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM bot_config ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]struct{ Key, Value string }, 0)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out = append(out, struct{ Key, Value string }{k, v})
	}
	return out, rows.Err()
}

// SeedDefaultConfig mirrors bot marketSync seedDefaultConfig essentials.
// Only inserts missing keys so values changed at runtime (including onboardingComplete) survive restarts.
func (s *Store) SeedDefaultConfig(ctx context.Context) error {
	rows := []struct{ k, v string }{
		{"pollingInterval", "30"},
		{"maxTradeSize", "100"},
		{"slippageTolerance", "0.05"},
		{"orderBookLevels", "10"},
		{"httpPlatformProxyUrl", ""},
		{"telegramBotToken", ""},
		{"telegramAuthorizedChatId", ""},
		{"eventClassificationTags", `["nba","nhl"]`},
		{"priceStopLossRanges", `[{"id":"r1","name":"20-30¢","minCents":20,"maxCents":30,"fundPct":17,"stopLossPct":20},{"id":"r2","name":"30-40¢","minCents":30,"maxCents":40,"fundPct":17,"stopLossPct":20},{"id":"r3","name":"40-50¢","minCents":40,"maxCents":50,"fundPct":17,"stopLossPct":20},{"id":"r4","name":"50-60¢","minCents":50,"maxCents":60,"fundPct":17,"stopLossPct":20},{"id":"r5","name":"60-70¢","minCents":60,"maxCents":70,"fundPct":16,"stopLossPct":20},{"id":"r6","name":"70-80¢","minCents":70,"maxCents":80,"fundPct":16,"stopLossPct":20}]`},
		{"polymarketFokBuyExtraTicks", "5"},
		{"polymarketFokSellExtraTicks", "5"},
		{"minOpenRiskShares", "1"},
		{"onboardingComplete", "false"},
	}
	for _, r := range rows {
		if err := s.InsertBotConfigDefault(ctx, r.k, r.v); err != nil {
			return fmt.Errorf("seed %s: %w", r.k, err)
		}
	}
	return nil
}
