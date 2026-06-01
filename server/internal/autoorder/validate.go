package autoorder

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// ValidateConfig checks the full auto-order policy before persistence.
func ValidateConfig(c *Config) error {
	if c == nil {
		return fmt.Errorf("config is nil")
	}
	if c.OutcomePolicy.Side != "" && c.OutcomePolicy.Side != "popular" {
		return fmt.Errorf("outcomePolicy.side must be popular")
	}
	if c.OutcomePolicy.MinImpliedOdds <= 0 || c.OutcomePolicy.MinImpliedOdds > 1 {
		return fmt.Errorf("outcomePolicy.minImpliedOdds must be in (0,1]")
	}
	switch c.DailyPool.Mode {
	case "percent_balance", "fixed_usd":
	default:
		return fmt.Errorf("dailyPool.mode must be percent_balance or fixed_usd")
	}
	if c.DailyPool.Value <= 0 {
		// Allow zero/invalid daily pool when all enabled groups use fundUsd.
		hasLegacy := false
		for _, g := range c.Groups {
			if g.Enabled && g.FundUsd <= 0 && g.BudgetPct > 0 {
				hasLegacy = true
				break
			}
		}
		if hasLegacy {
			return fmt.Errorf("dailyPool.value must be positive")
		}
	}
	if c.DailyPool.Mode == "percent_balance" && c.DailyPool.Value > 100 {
		return fmt.Errorf("dailyPool.value for percent_balance must be <= 100")
	}

	seenTeam := map[int]string{}
	for i := range c.Groups {
		g := &c.Groups[i]
		if strings.TrimSpace(g.ID) == "" {
			g.ID = uuid.NewString()
		}
		if strings.TrimSpace(g.Name) == "" {
			return fmt.Errorf("group %s: name is required", g.ID)
		}
		if strings.TrimSpace(g.League) == "" {
			return fmt.Errorf("group %s: league is required", g.ID)
		}
		g.League = strings.ToLower(strings.TrimSpace(g.League))
		if g.Enabled {
			if g.FundUsd <= 0 && (g.BudgetPct <= 0 || g.BudgetPct > 100) {
				return fmt.Errorf("group %s: fundUsd must be positive (or legacy budgetPct in (0,100])", g.Name)
			}
		}
		if err := validatePriceGate(g.PriceGate, g.Name); err != nil {
			return err
		}
		if len(g.OddsBands) == 0 {
			return fmt.Errorf("group %s: oddsBands required", g.Name)
		}
		for _, b := range g.OddsBands {
			if err := validateOddsBand(b, g.Name, g.PriceGate); err != nil {
				return err
			}
		}
		if g.Triggers.MinutesBeforeStart < 0 {
			return fmt.Errorf("group %s: minutesBeforeStart must be >= 0", g.Name)
		}
		if g.Triggers.MinEventVolumeUsd < 0 {
			return fmt.Errorf("group %s: minEventVolumeUsd must be >= 0", g.Name)
		}
		if len(g.Teams) == 0 {
			return fmt.Errorf("group %s: at least one team required", g.Name)
		}
		for _, t := range g.Teams {
			if t.ID <= 0 {
				return fmt.Errorf("group %s: team id must be positive", g.Name)
			}
			if strings.TrimSpace(t.Name) == "" {
				return fmt.Errorf("group %s: team name required", g.Name)
			}
			if prev, ok := seenTeam[t.ID]; ok && prev != g.ID {
				return fmt.Errorf("team %d (%s) already assigned to another group", t.ID, t.Name)
			}
			seenTeam[t.ID] = g.ID
		}
	}
	// Sync legacy global enabled flag from groups.
	c.Enabled = c.AnyGroupEnabled()
	return nil
}

func validatePriceGate(pg PriceGate, groupName string) error {
	if pg.MinCents < 1 || pg.MaxCents > 99 || pg.MinCents > pg.MaxCents {
		return fmt.Errorf("group %s: priceGate min/max cents invalid", groupName)
	}
	return nil
}

func validateOddsBand(b OddsBand, groupName string, gate PriceGate) error {
	if b.MinCents < gate.MinCents || b.MaxCents > gate.MaxCents {
		return fmt.Errorf("group %s: odds band [%d,%d] must be within priceGate [%d,%d]", groupName, b.MinCents, b.MaxCents, gate.MinCents, gate.MaxCents)
	}
	if b.MinCents > b.MaxCents {
		return fmt.Errorf("group %s: odds band min > max", groupName)
	}
	if b.StakePct <= 0 || b.StakePct > 100 {
		return fmt.Errorf("group %s: stakePct must be in (0,100]", groupName)
	}
	return nil
}

// PriceInGate returns true when odds (0-1) fall within the inclusive cent gate.
func PriceInGate(odds float64, gate PriceGate) bool {
	cents := odds * 100
	return cents >= float64(gate.MinCents)-1e-9 && cents <= float64(gate.MaxCents)+1e-9
}
