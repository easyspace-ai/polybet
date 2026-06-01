package autoorder

import (
	"strings"
	"time"

	"github.com/easyspace-ai/polybet/internal/bookcache"
	"github.com/easyspace-ai/polybet/internal/store"
)

// EvalResult is the outcome of trigger + policy checks for one candidate.
type EvalResult struct {
	OK           bool
	SkipReason   string
	Outcome      *store.OutcomeRow
	ImpliedOdds  float64
	StartTime    time.Time
	InTriggerWin bool
}

func liveOdds(o store.OutcomeRow, platform string, cache *bookcache.Cache) float64 {
	odds := o.CurrentOdds
	if platform == "polymarket" && o.ExternalID.Valid && o.ExternalID.String != "" && cache != nil {
		if v, ok := cache.TakerOdds(o.ExternalID.String); ok {
			odds = v
		}
	}
	return odds
}

// PickPopularOutcome selects the outcome with higher implied odds between two sides.
func PickPopularOutcome(outcomes []store.OutcomeRow, platform string, cache *bookcache.Cache) (*store.OutcomeRow, float64) {
	if len(outcomes) < 2 {
		return nil, 0
	}
	var best *store.OutcomeRow
	var bestOdds float64
	for i := range outcomes {
		o := &outcomes[i]
		odds := liveOdds(*o, platform, cache)
		if best == nil || odds > bestOdds {
			best = o
			bestOdds = odds
		}
	}
	return best, bestOdds
}

func parseStartTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	layouts := []string{time.RFC3339Nano, time.RFC3339}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func inTriggerWindow(now, start time.Time, minutesBefore int) bool {
	if start.IsZero() {
		return false
	}
	windowStart := start.Add(-time.Duration(minutesBefore) * time.Minute)
	return !now.Before(windowStart) && now.Before(start)
}

// Evaluate applies trigger window, volume, popular-side policy, and price gate.
func Evaluate(now time.Time, cfg Config, cand MatchCandidate, cache *bookcache.Cache) EvalResult {
	g := cand.Group
	m := cand.Market
	start, ok := parseStartTime(m.StartTime)
	if !ok {
		return EvalResult{SkipReason: "missing_start_time"}
	}
	if !inTriggerWindow(now, start, g.Triggers.MinutesBeforeStart) {
		return EvalResult{SkipReason: "outside_trigger_window", StartTime: start}
	}
	if m.EventVolume < g.Triggers.MinEventVolumeUsd {
		return EvalResult{SkipReason: "event_volume_below_min", StartTime: start}
	}
	pop, odds := PickPopularOutcome(cand.Outcomes, m.Platform, cache)
	if pop == nil {
		return EvalResult{SkipReason: "popular_outcome_not_found", StartTime: start}
	}
	minOdds := cfg.OutcomePolicy.MinImpliedOdds
	if minOdds <= 0 {
		minOdds = 0.50
	}
	if odds <= minOdds {
		return EvalResult{SkipReason: "popular_odds_below_min", StartTime: start}
	}
	if !PriceInGate(odds, g.PriceGate) {
		return EvalResult{SkipReason: "outside_price_gate", StartTime: start}
	}
	return EvalResult{
		OK:          true,
		Outcome:     pop,
		ImpliedOdds: odds,
		StartTime:   start,
		InTriggerWin: true,
	}
}
