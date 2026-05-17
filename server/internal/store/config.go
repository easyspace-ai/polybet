package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

func (cf *configFile) snapshotStringMap() map[string]string {
	cf.mu.RLock()
	defer cf.mu.RUnlock()
	out := make(map[string]string, len(cf.data))
	for k, v := range cf.data {
		out[k] = v
	}
	return out
}

// BotConfigStringMap returns an in-memory copy of bot-settings keys (for Badger import).
func BotConfigStringMap() map[string]string {
	return globalConfigFile.snapshotStringMap()
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
	if s != nil && s.kv() != nil {
		m, err := s.kv().ReadBotConfigMap(ctx)
		if err != nil {
			return "", false, err
		}
		if m != nil {
			if v, ok := m[key]; ok {
				return v, true, nil
			}
		}
	}
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
	if err := globalConfigFile.set(key, value); err != nil {
		return err
	}
	if s == nil || s.kv() == nil {
		return nil
	}
	m, err := s.kv().ReadBotConfigMap(ctx)
	if err != nil {
		return err
	}
	if m == nil {
		m = map[string]string{}
	}
	m[key] = value
	return s.kv().WriteBotConfigMap(ctx, m)
}

func (s *Store) InsertBotConfigDefault(ctx context.Context, key, value string) error {
	if _, ok := globalConfigFile.get(key); ok {
		return nil
	}
	if s != nil && s.kv() != nil {
		m, err := s.kv().ReadBotConfigMap(ctx)
		if err != nil {
			return err
		}
		if m != nil {
			if _, has := m[key]; has {
				return nil
			}
		}
	}
	return s.UpsertBotConfig(ctx, key, value)
}

func (s *Store) ListBotConfig(ctx context.Context) ([]struct{ Key, Value string }, error) {
	merged := map[string]string{}
	for _, e := range globalConfigFile.list() {
		merged[e.Key] = e.Value
	}
	if s != nil && s.kv() != nil {
		m, err := s.kv().ReadBotConfigMap(ctx)
		if err != nil {
			return nil, err
		}
		for k, v := range m {
			merged[k] = v
		}
	}
	out := make([]struct{ Key, Value string }, 0, len(merged))
	for k, v := range merged {
		out = append(out, struct{ Key, Value string }{Key: k, Value: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
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
		{"riskStopLossMarketEndedCooldownSec", "300"},
		{"minOpenRiskShares", "1"},
		{"onboardingComplete", "false"},
		{"riskCloseExecutionMode", "fok_sell"},
		{"riskCloseFakWorstPrice", "0.01"},
		// SELL-side slippage cap in basis points. > 0 refuses FOK/FAK SELL
		// attempts whose projected limit floor would drop more than this
		// many bps below the eval bid (= panic-dump territory). The hedge
		// tier is exempt because it's the lock-losses-at-any-price
		// fallback. 0 disables; suggested production value: 1000 (10%).
		{"riskCloseMaxSlippageBps", "0"},
		{"riskHedgeBuySizing", "notional"},
		{"riskHedgeAutoHidePosition", "true"},
		// Hedge collateral protection. The hedge_fok_buy path needs USDC
		// to fund the BUY; without these checks a small or empty wallet
		// produces a CLOB rejection that leaves the original position
		// uncovered. reservePct is a fraction kept UNUSED for fees and
		// rounding (5% headroom by default). minUSDC is the smallest
		// hedge the close path is willing to submit; below it, the
		// hedge tier aborts so the operator notices instead of
		// silently misfiring on a dust BUY.
		{"riskHedgeCollateralReservePct", "0.05"},
		{"riskHedgeMinUSDC", "1.0"},
		// Trade gate / kill-switch defaults. All <= 0 / falsy keys are no-ops
		// so existing deployments are unaffected until operators opt in.
		{"riskTradingHalted", "false"},
		{"riskMaxDailyLossUSD", "0"},
		// Window over which the kill switch counts realized PnL (closed
		// positions). Default 86400 = 24h. Set to 0 to count only unrealized
		// PnL (legacy behaviour). Closed positions whose realized_pnl_usd is
		// NULL — dust closures, ghost-balance reconciles — are excluded.
		{"riskKillSwitchWindowSec", "86400"},
		{"riskMaxOpenPositions", "0"},
		// Notional cost-basis exposure caps (USD). 0 disables. Cost-basis
		// is conservative for an open-side cap because it ignores price
		// drift (mark-to-market would inflate winners and shrink losers).
		{"riskMaxAccountExposureUSD", "0"},
		{"riskMaxMarketExposureUSD", "0"},
		// Default taker fee fraction applied at sync time when a Gamma row
		// does not carry feeRate / feeRateBps / takerBaseFee. Override per
		// market still wins; this only changes the FALLBACK that previously
		// hardcoded 0.03 across all sports markets. Set to 0 if the league
		// you trade is fee-free, or to your real average if you have data.
		{"syncDefaultTakerFeeRate", "0.03"},
		// 0 disables per-token book-staleness gating; setting to e.g. 5000
		// (= 5 s) refuses opens when WS+REST cache is older than that window.
		{"riskBookMaxAgeMs", "0"},
		// 0 disables; setting to e.g. 60 (= 60 s) refuses opens when no WS
		// market message has been observed across all subscriptions for that
		// long, even if the per-token cache happens to look fresh.
		{"riskMaxReconcileGapSec", "0"},
		// > 0 refuses new opens when the market start_time is more than
		// this many seconds in the past (post-kickoff guard). Tokens with
		// unknown start_time are NOT blocked. 0 disables.
		{"riskBlockOpenAfterStartSec", "0"},
		// Absolute cent-drop ceiling combined with the price-band percent
		// table. Effective trigger = max(percent_trail, hw - priceStopLossAbsCents).
		// Useful for high-price favourites where 10% percent trail is far too
		// loose (10% of 95¢ = 9.5¢ drop). 0 disables, preserving legacy
		// percent-only behaviour.
		{"priceStopLossAbsCents", "0"},
		// High-water ratchet model:
		//   riskHwUseMicroPrice (true/false): use depth-weighted micro-price
		//     instead of max(bid,ask) so a single thin ask flicker cannot
		//     inflate HW. Default false preserves legacy behaviour.
		//   riskHwMinDepthUsd (USD float, 0 disables): suppress HW ratchet
		//     when neither side of the top-of-book has at least this much
		//     USD depth. Filters microstructure noise on illiquid books.
		{"riskHwUseMicroPrice", "false"},
		{"riskHwMinDepthUsd", "0"},
		// Pre-submit /book freshness check. When > 0 and elapsed since the
		// initial fetch exceeds this many ms, polyexec refetches /book and
		// recomputes the limit price before signing. Catches build-then-
		// submit races where bestBid moves down between fetch and signing.
		// 0 disables; suggested production value: 1500.
		{"orderSubmitMaxAgeMs", "0"},
		// Close ladder tiers (per-attempt close strategy) used when
		// riskCloseExecutionMode = "ladder". The default progression is
		// gentle FOK → aggressive FAK → deeper FAK → hedge to lock losses.
		// Operators can override via a JSON array; see risk_close_ladder.go.
		{"riskCloseLadderTiers", `[{"type":"fok_sell","extraTicks":2},{"type":"fak_sell","extraTicks":5},{"type":"fak_sell","extraTicks":15},{"type":"hedge_fok_buy"}]`},
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
