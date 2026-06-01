package autoorder

import "encoding/json"

const (
	ConfigKey       = "autoOrderConfig"
	LedgerKey       = "autoOrderLedger"
	RunsKey         = "autoOrderRuns"
	DryRunKey       = "autoOrderDryRun"
	TickSecKey      = "autoOrderTickSec"
	DefaultTickSec  = 45
	MaxRecentRuns   = 50
)

// Config is the persisted auto-order policy (bot_config.autoOrderConfig JSON).
type Config struct {
	Enabled       bool          `json:"enabled"`
	DailyPool     DailyPool     `json:"dailyPool"`
	OutcomePolicy OutcomePolicy `json:"outcomePolicy"`
	Groups        []Group       `json:"groups"`
}

type DailyPool struct {
	Mode  string  `json:"mode"` // percent_balance | fixed_usd
	Value float64 `json:"value"`
}

type OutcomePolicy struct {
	Side             string  `json:"side"` // popular
	MinImpliedOdds   float64 `json:"minImpliedOdds"`
}

type Group struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Enabled    bool       `json:"enabled"`
	League     string     `json:"league"`
	FundUsd    float64    `json:"fundUsd"`
	BudgetPct  float64    `json:"budgetPct"`
	Teams      []Team     `json:"teams"`
	PriceGate  PriceGate  `json:"priceGate"`
	OddsBands  []OddsBand `json:"oddsBands"`
	Triggers   Triggers   `json:"triggers"`
}

type Team struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Abbreviation string `json:"abbreviation"`
}

type PriceGate struct {
	MinCents int `json:"minCents"`
	MaxCents int `json:"maxCents"`
}

type OddsBand struct {
	MinCents int     `json:"minCents"`
	MaxCents int     `json:"maxCents"`
	StakePct float64 `json:"stakePct"`
}

type Triggers struct {
	MinutesBeforeStart int     `json:"minutesBeforeStart"`
	MinEventVolumeUsd  float64 `json:"minEventVolumeUsd"`
}

// RunRecord is one auto-order attempt (success, skip, dry-run, or failure).
type RunRecord struct {
	At           string  `json:"at"`
	GroupID      string  `json:"groupId"`
	GroupName    string  `json:"groupName"`
	EventID      string  `json:"eventId"`
	Match        string  `json:"match"`
	OutcomeID    string  `json:"outcomeId,omitempty"`
	OutcomeLabel string  `json:"outcomeLabel,omitempty"`
	SizeUSD      float64 `json:"sizeUsd,omitempty"`
	Odds         float64 `json:"odds,omitempty"`
	Status       string  `json:"status"` // skipped | dry_run | filled | failed
	Reason       string  `json:"reason,omitempty"`
	TradeID      string  `json:"tradeId,omitempty"`
}

// Ledger tracks NY-calendar-day spend and idempotency keys.
type Ledger struct {
	Date       string             `json:"date"`
	GroupSpent map[string]float64 `json:"groupSpent"`
	Executed   []string           `json:"executed"`
}

func DefaultConfig() Config {
	return Config{
		Enabled: false,
		DailyPool: DailyPool{
			Mode:  "percent_balance",
			Value: 10,
		},
		OutcomePolicy: OutcomePolicy{
			Side:           "popular",
			MinImpliedOdds: 0.50,
		},
		Groups: nil,
	}
}

// AnyGroupEnabled reports whether at least one group is enabled.
func (c Config) AnyGroupEnabled() bool {
	for _, g := range c.Groups {
		if g.Enabled {
			return true
		}
	}
	return false
}

func DefaultConfigJSON() string {
	b, _ := json.Marshal(DefaultConfig())
	return string(b)
}

func ParseConfig(raw string) (Config, error) {
	raw = trimJSON(raw)
	if raw == "" {
		return DefaultConfig(), nil
	}
	var c Config
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return Config{}, err
	}
	return c, nil
}

func trimJSON(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\n' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 {
		last := s[len(s)-1]
		if last == ' ' || last == '\n' || last == '\t' {
			s = s[:len(s)-1]
			continue
		}
		break
	}
	return s
}
