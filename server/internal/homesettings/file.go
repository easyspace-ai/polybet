package homesettings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/easyspace-ai/polybet/internal/store"
)

// FileName is the basename under ~/.polybet/ holding a JSON object of
// bot_config key → string value (same keys as GET /api/config).
const FileName = "bot-settings.json"

// FilePath returns ~/.polybet/bot-settings.json.
func FilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home dir: %w", err)
	}
	if home == "" {
		return "", errors.New("empty user home dir")
	}
	return filepath.Join(home, ".polybet", FileName), nil
}

// ApplyFromFile reads the home settings file (if present) and upserts each
// entry into bot_config. Values in the file win over existing DB rows for
// those keys. Missing file is not an error.
func ApplyFromFile(ctx context.Context, st *store.Store) error {
	if st == nil {
		return errors.New("nil store")
	}
	p, err := FilePath()
	if err != nil {
		return err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return fmt.Errorf("parse %s: %w", p, err)
	}
	for key, rm := range raw {
		if key == "" {
			continue
		}
		val, err := stringifyJSONValue(rm)
		if err != nil {
			return fmt.Errorf("key %q: %w", key, err)
		}
		if err := st.UpsertBotConfig(ctx, key, val); err != nil {
			return fmt.Errorf("upsert %q: %w", key, err)
		}
	}
	return nil
}

// SnapshotToFile writes the full bot_config table to ~/.polybet/bot-settings.json
// with mode 0600. Creates ~/.polybet when missing.
func SnapshotToFile(ctx context.Context, st *store.Store) error {
	if st == nil {
		return errors.New("nil store")
	}
	p, err := FilePath()
	if err != nil {
		return err
	}
	rows, err := st.ListBotConfig(ctx)
	if err != nil {
		return err
	}
	m := make(map[string]string, len(rows))
	for _, r := range rows {
		m[r.Key] = r.Value
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	_ = os.Chmod(dir, 0o700)
	return nil
}

func stringifyJSONValue(rm json.RawMessage) (string, error) {
	if len(rm) == 0 {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(rm, &s); err == nil {
		return s, nil
	}
	var n float64
	if err := json.Unmarshal(rm, &n); err == nil {
		return strconv.FormatFloat(n, 'f', -1, 64), nil
	}
	var b bool
	if err := json.Unmarshal(rm, &b); err == nil {
		return strconv.FormatBool(b), nil
	}
	return string(rm), nil
}
