package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type configFile struct {
	mu   sync.RWMutex
	path string
	data map[string]string
}

func (cf *configFile) load() error {
	cf.mu.Lock()
	defer cf.mu.Unlock()

	b, err := os.ReadFile(cf.path)
	if err != nil {
		if os.IsNotExist(err) {
			cf.data = make(map[string]string)
			return nil
		}
		return err
	}
	return json.Unmarshal(b, &cf.data)
}

func (cf *configFile) save() error {
	cf.mu.Lock()
	defer cf.mu.Unlock()

	dir := filepath.Dir(cf.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	b, err := json.MarshalIndent(cf.data, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')

	tmp := cf.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, cf.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (cf *configFile) get(key string) (string, bool) {
	cf.mu.RLock()
	defer cf.mu.RUnlock()
	v, ok := cf.data[key]
	return v, ok
}

func (cf *configFile) set(key, value string) error {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	cf.data[key] = value
	return cf.saveLocked()
}

func (cf *configFile) saveLocked() error {
	if cf.data == nil {
		return nil
	}

	dir := filepath.Dir(cf.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	b, err := json.MarshalIndent(cf.data, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')

	tmp := cf.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, cf.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (cf *configFile) list() []struct{ Key, Value string } {
	cf.mu.RLock()
	defer cf.mu.RUnlock()

	out := make([]struct{ Key, Value string }, 0, len(cf.data))
	for k, v := range cf.data {
		out = append(out, struct{ Key, Value string }{k, v})
	}
	return out
}

func configFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home dir: %w", err)
	}
	if home == "" {
		return "", errors.New("empty user home dir")
	}
	return filepath.Join(home, ".polybet", "bot-settings.json"), nil
}

var globalConfigFile *configFile

func init() {
	path, err := configFilePath()
	if err != nil {
		panic("config file path: " + err.Error())
	}
	globalConfigFile = &configFile{path: path}
	if err := globalConfigFile.load(); err != nil {
		panic("load config file: " + err.Error())
	}
}

func (s *Store) GetBotConfig(ctx context.Context, key string) (string, bool, error) {
	v, ok := globalConfigFile.get(key)
	return v, ok, nil
}

func (s *Store) GetBotConfigFloat(ctx context.Context, key string, def float64) float64 {
	v, ok, err := s.GetBotConfig(ctx, key)
	if err != nil || !ok {
		return def
	}
	var f float64
	if _, err := fmt.Sscanf(v, "%f", &f); err != nil {
		return def
	}
	return f
}

func (s *Store) GetBotConfigInt(ctx context.Context, key string, def int) int {
	v, ok, err := s.GetBotConfig(ctx, key)
	if err != nil || !ok {
		return def
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
		return def
	}
	return n
}

func (s *Store) UpsertBotConfig(ctx context.Context, key, value string) error {
	return globalConfigFile.set(key, value)
}

func (s *Store) InsertBotConfigDefault(ctx context.Context, key, value string) error {
	if _, ok := globalConfigFile.get(key); ok {
		return nil
	}
	return globalConfigFile.set(key, value)
}

func (s *Store) ListBotConfig(ctx context.Context) ([]struct{ Key, Value string }, error) {
	return globalConfigFile.list(), nil
}

func (s *Store) SeedDefaultConfig(ctx context.Context) error {
	rows := []struct{ k, v string }{
		{"pollingInterval", "60"},
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
	for _, r := range append(rows, wsConfigSeedRows()...) {
		if err := s.InsertBotConfigDefault(ctx, r.k, r.v); err != nil {
			return fmt.Errorf("seed %s: %w", r.k, err)
		}
	}
	return nil
}

func wsConfigSeedRows() []struct{ k, v string } {
	out := make([]struct{ k, v string }, 0, 24)
	// Inline defaults to avoid import cycle with wsconfig package.
	seed := []struct{ k, v string }{
		{"wsClobPingIntervalSec", "20"},
		{"wsClobPongTimeoutSec", "60"},
		{"wsClobBackoffBaseSec", "1"},
		{"wsClobBackoffMaxSec", "60"},
		{"wsClobBackoffJitterPct", "30"},
		{"wsClobReconnectStableSec", "120"},
		{"wsClobMaxReconnectAttempts", "0"},
		{"wsClobSleepThresholdSec", "5"},
		{"wsHealthCheckIntervalSec", "30"},
		{"wsBookStaleThresholdSec", "45"},
		{"wsPositionsReconcileOpenSec", "20"},
		{"wsPositionsReconcileIdleSec", "60"},
		{"wsRestTradesIntervalSec", "45"},
		{"wsStoplossReconcileSec", "2"},
		{"wsDashPingIntervalSec", "20"},
		{"wsDashPongTimeoutSec", "10"},
		{"wsDashBackoffBaseSec", "1"},
		{"wsDashBackoffMaxSec", "60"},
		{"wsDashBackoffJitterPct", "30"},
		{"wsDashSleepThresholdSec", "5"},
		{"wsRiskPollIntervalSec", "30"},
		{"wsAutoReconnectOnDisconnect", "true"},
		{"wsAutoRequestUpstreamReconnect", "true"},
		{"desktopSidecarWatchdogSec", "30"},
		{"desktopSidecarWatchdogFailThreshold", "2"},
		{"desktopSidecarWatchdogHttpTimeoutSec", "5"},
		{"desktopSidecarMaxRetries", "0"},
		{"desktopSidecarKillGraceSec", "5"},
	}
	return append(out, seed...)
}