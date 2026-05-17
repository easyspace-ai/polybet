// Package wsconfig loads WebSocket resilience timing from bot-settings.json.
package wsconfig

import (
	"context"
	"time"

	"github.com/easyspace-ai/polybet/internal/marketstream"
	"github.com/easyspace-ai/polybet/internal/storage"
)

// Markets sync interval is bot_config key pollingInterval (minutes, default 60 =
// 1 hour). It is not part of Settings; see config.DefaultMarketsSyncIntervalMin.

// Settings holds all WS-related bot config (durations in seconds unless noted).
type Settings struct {
	ClobPingIntervalSec          int
	ClobPongTimeoutSec           int
	ClobBackoffBaseSec           int
	ClobBackoffMaxSec            int
	ClobBackoffJitterPct         int
	ClobReconnectStableSec       int
	ClobMaxReconnectAttempts     int
	ClobSleepThresholdSec        int
	HealthCheckIntervalSec       int
	BookStaleThresholdSec        int
	PositionsReconcileOpenSec    int
	PositionsReconcileIdleSec    int
	RestTradesIntervalSec        int
	StoplossReconcileSec         int
	DashPingIntervalSec          int
	DashPongTimeoutSec           int
	DashBackoffBaseSec           int
	DashBackoffMaxSec            int
	DashBackoffJitterPct         int
	DashSleepThresholdSec        int
	RiskPollIntervalSec          int
	AutoReconnectOnDisconnect    bool
	AutoRequestUpstreamReconnect bool
}

const (
	defaultClobPingIntervalSec       = 20
	defaultClobPongTimeoutSec        = 60
	defaultClobBackoffBaseSec        = 1
	defaultClobBackoffMaxSec         = 60
	defaultClobBackoffJitterPct      = 30
	defaultClobReconnectStableSec    = 120
	defaultClobMaxReconnectAttempts  = 0
	defaultClobSleepThresholdSec     = 5
	defaultHealthCheckIntervalSec    = 30
	defaultBookStaleThresholdSec     = 45
	defaultPositionsReconcileOpenSec = 20
	defaultPositionsReconcileIdleSec = 60
	defaultRestTradesIntervalSec     = 45
	defaultStoplossReconcileSec      = 2
	defaultDashPingIntervalSec       = 20
	defaultDashPongTimeoutSec        = 10
	defaultDashBackoffBaseSec        = 1
	defaultDashBackoffMaxSec         = 60
	defaultDashBackoffJitterPct      = 30
	defaultDashSleepThresholdSec     = 5
	defaultRiskPollIntervalSec       = 30
)

// Load reads WS settings from the store with validated defaults.
func Load(ctx context.Context, st *storage.Backend) Settings {
	s := Settings{
		ClobPingIntervalSec:          st.GetBotConfigInt(ctx, "wsClobPingIntervalSec", defaultClobPingIntervalSec),
		ClobPongTimeoutSec:           st.GetBotConfigInt(ctx, "wsClobPongTimeoutSec", defaultClobPongTimeoutSec),
		ClobBackoffBaseSec:           st.GetBotConfigInt(ctx, "wsClobBackoffBaseSec", defaultClobBackoffBaseSec),
		ClobBackoffMaxSec:            st.GetBotConfigInt(ctx, "wsClobBackoffMaxSec", defaultClobBackoffMaxSec),
		ClobBackoffJitterPct:         st.GetBotConfigInt(ctx, "wsClobBackoffJitterPct", defaultClobBackoffJitterPct),
		ClobReconnectStableSec:       st.GetBotConfigInt(ctx, "wsClobReconnectStableSec", defaultClobReconnectStableSec),
		ClobMaxReconnectAttempts:     st.GetBotConfigInt(ctx, "wsClobMaxReconnectAttempts", defaultClobMaxReconnectAttempts),
		ClobSleepThresholdSec:        st.GetBotConfigInt(ctx, "wsClobSleepThresholdSec", defaultClobSleepThresholdSec),
		HealthCheckIntervalSec:       st.GetBotConfigInt(ctx, "wsHealthCheckIntervalSec", defaultHealthCheckIntervalSec),
		BookStaleThresholdSec:        st.GetBotConfigInt(ctx, "wsBookStaleThresholdSec", defaultBookStaleThresholdSec),
		PositionsReconcileOpenSec:    st.GetBotConfigInt(ctx, "wsPositionsReconcileOpenSec", defaultPositionsReconcileOpenSec),
		PositionsReconcileIdleSec:    st.GetBotConfigInt(ctx, "wsPositionsReconcileIdleSec", defaultPositionsReconcileIdleSec),
		RestTradesIntervalSec:        st.GetBotConfigInt(ctx, "wsRestTradesIntervalSec", defaultRestTradesIntervalSec),
		StoplossReconcileSec:         st.GetBotConfigInt(ctx, "wsStoplossReconcileSec", defaultStoplossReconcileSec),
		DashPingIntervalSec:          st.GetBotConfigInt(ctx, "wsDashPingIntervalSec", defaultDashPingIntervalSec),
		DashPongTimeoutSec:           st.GetBotConfigInt(ctx, "wsDashPongTimeoutSec", defaultDashPongTimeoutSec),
		DashBackoffBaseSec:           st.GetBotConfigInt(ctx, "wsDashBackoffBaseSec", defaultDashBackoffBaseSec),
		DashBackoffMaxSec:            st.GetBotConfigInt(ctx, "wsDashBackoffMaxSec", defaultDashBackoffMaxSec),
		DashBackoffJitterPct:         st.GetBotConfigInt(ctx, "wsDashBackoffJitterPct", defaultDashBackoffJitterPct),
		DashSleepThresholdSec:        st.GetBotConfigInt(ctx, "wsDashSleepThresholdSec", defaultDashSleepThresholdSec),
		RiskPollIntervalSec:          st.GetBotConfigInt(ctx, "wsRiskPollIntervalSec", defaultRiskPollIntervalSec),
		AutoReconnectOnDisconnect:    botConfigBool(st, ctx, "wsAutoReconnectOnDisconnect", true),
		AutoRequestUpstreamReconnect: botConfigBool(st, ctx, "wsAutoRequestUpstreamReconnect", true),
	}
	return s.Validate()
}

func botConfigBool(st *storage.Backend, ctx context.Context, key string, def bool) bool {
	v, ok, _ := st.GetBotConfig(ctx, key)
	if !ok {
		return def
	}
	switch v {
	case "0", "false", "False", "no":
		return false
	case "1", "true", "True", "yes":
		return true
	default:
		return def
	}
}

// Validate clamps values to safe ranges.
func (s Settings) Validate() Settings {
	clamp := func(v, lo, hi int) int {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}
	s.ClobPingIntervalSec = clamp(s.ClobPingIntervalSec, 5, 120)
	s.ClobPongTimeoutSec = clamp(s.ClobPongTimeoutSec, 5, 300)
	s.ClobBackoffBaseSec = clamp(s.ClobBackoffBaseSec, 1, 60)
	s.ClobBackoffMaxSec = clamp(s.ClobBackoffMaxSec, s.ClobBackoffBaseSec, 600)
	s.ClobBackoffJitterPct = clamp(s.ClobBackoffJitterPct, 0, 50)
	s.ClobReconnectStableSec = clamp(s.ClobReconnectStableSec, 30, 3600)
	s.ClobSleepThresholdSec = clamp(s.ClobSleepThresholdSec, 2, 120)
	s.HealthCheckIntervalSec = clamp(s.HealthCheckIntervalSec, 5, 300)
	s.BookStaleThresholdSec = clamp(s.BookStaleThresholdSec, 10, 600)
	s.PositionsReconcileOpenSec = clamp(s.PositionsReconcileOpenSec, 5, 300)
	s.PositionsReconcileIdleSec = clamp(s.PositionsReconcileIdleSec, 10, 600)
	s.RestTradesIntervalSec = clamp(s.RestTradesIntervalSec, 10, 600)
	s.StoplossReconcileSec = clamp(s.StoplossReconcileSec, 1, 60)
	s.DashPingIntervalSec = clamp(s.DashPingIntervalSec, 5, 120)
	s.DashPongTimeoutSec = clamp(s.DashPongTimeoutSec, 3, 120)
	s.DashBackoffBaseSec = clamp(s.DashBackoffBaseSec, 1, 60)
	s.DashBackoffMaxSec = clamp(s.DashBackoffMaxSec, s.DashBackoffBaseSec, 600)
	s.DashBackoffJitterPct = clamp(s.DashBackoffJitterPct, 0, 50)
	s.DashSleepThresholdSec = clamp(s.DashSleepThresholdSec, 2, 120)
	s.RiskPollIntervalSec = clamp(s.RiskPollIntervalSec, 5, 300)
	if s.ClobMaxReconnectAttempts < 0 {
		s.ClobMaxReconnectAttempts = 0
	}
	return s
}

// ToMarketstreamConfig maps settings into a marketstream.Config (CLOB upstream).
func (s Settings) ToMarketstreamConfig(base *marketstream.Config) *marketstream.Config {
	if base == nil {
		base = marketstream.DefaultConfig()
	}
	base.ReconnectEnabled = true
	base.ReconnectDelay = time.Duration(s.ClobBackoffBaseSec) * time.Second
	base.MaxReconnectDelay = time.Duration(s.ClobBackoffMaxSec) * time.Second
	base.MaxReconnectAttempts = s.ClobMaxReconnectAttempts
	base.PingInterval = time.Duration(s.ClobPingIntervalSec) * time.Second
	base.PongTimeout = time.Duration(s.ClobPongTimeoutSec) * time.Second
	base.BackoffBase = time.Duration(s.ClobBackoffBaseSec) * time.Second
	base.BackoffMax = time.Duration(s.ClobBackoffMaxSec) * time.Second
	base.BackoffJitterPct = s.ClobBackoffJitterPct
	base.ReconnectStable = time.Duration(s.ClobReconnectStableSec) * time.Second
	base.SleepThreshold = time.Duration(s.ClobSleepThresholdSec) * time.Second
	return base
}

// DefaultSeedRows returns key/value pairs for SeedDefaultConfig.
func DefaultSeedRows() []struct{ K, V string } {
	return []struct{ K, V string }{
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
	}
}
