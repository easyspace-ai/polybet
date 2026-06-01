package autoorder

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/easyspace-ai/polybet/internal/storage"
)

// LoadConfig reads autoOrderConfig from bot_config.
func LoadConfig(ctx context.Context, st *storage.Backend) (Config, error) {
	if st == nil {
		return DefaultConfig(), nil
	}
	raw, ok, err := st.GetBotConfig(ctx, ConfigKey)
	if err != nil {
		return Config{}, err
	}
	if !ok {
		return DefaultConfig(), nil
	}
	return ParseConfig(raw)
}

// SaveConfig validates and persists autoOrderConfig.
func SaveConfig(ctx context.Context, st *storage.Backend, c Config) error {
	if err := ValidateConfig(&c); err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return st.UpsertBotConfig(ctx, ConfigKey, string(b))
}

// IsDryRun reads autoOrderDryRun bot_config (default true).
func IsDryRun(ctx context.Context, st *storage.Backend) bool {
	if st == nil {
		return true
	}
	v, ok, err := st.GetBotConfig(ctx, DryRunKey)
	if err != nil || !ok {
		return true
	}
	return v != "false" && v != "0"
}

// TickIntervalSec reads autoOrderTickSec (default 45, min 15).
func TickIntervalSec(ctx context.Context, st *storage.Backend) int {
	if st == nil {
		return DefaultTickSec
	}
	n := st.GetBotConfigInt(ctx, TickSecKey, DefaultTickSec)
	if n < 15 {
		return 15
	}
	if n > 300 {
		return 300
	}
	return n
}

// SetDryRun persists the dry-run flag.
func SetDryRun(ctx context.Context, st *storage.Backend, dry bool) error {
	v := "true"
	if !dry {
		v = "false"
	}
	return st.UpsertBotConfig(ctx, DryRunKey, v)
}

// ConfigResponse augments Config with runtime flags for the dashboard.
type ConfigResponse struct {
	Config
	DryRun       bool `json:"dryRun"`
	TickSec      int  `json:"tickSec"`
	ReadOnlyMode bool `json:"readOnlyMode"`
}

func LoadConfigResponse(ctx context.Context, st *storage.Backend, readOnly bool) (ConfigResponse, error) {
	c, err := LoadConfig(ctx, st)
	if err != nil {
		return ConfigResponse{}, err
	}
	return ConfigResponse{
		Config:       c,
		DryRun:       IsDryRun(ctx, st),
		TickSec:      TickIntervalSec(ctx, st),
		ReadOnlyMode: readOnly,
	}, nil
}

// SaveConfigRequest may include dry-run toggle alongside policy.
type SaveConfigRequest struct {
	Config
	DryRun *bool `json:"dryRun,omitempty"`
}

// ApplySaveRequest validates and persists config plus optional dry-run toggle.
func ApplySaveRequest(ctx context.Context, st *storage.Backend, req SaveConfigRequest) error {
	if err := SaveConfig(ctx, st, req.Config); err != nil {
		return err
	}
	if req.DryRun != nil {
		if err := SetDryRun(ctx, st, *req.DryRun); err != nil {
			return fmt.Errorf("dryRun: %w", err)
		}
	}
	return nil
}
