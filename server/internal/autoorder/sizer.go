package autoorder

// GroupBudgetForGroup returns the USD budget cap for a group (fundUsd preferred, legacy budgetPct fallback).
func GroupBudgetForGroup(dailyPoolUSD float64, g Group) float64 {
	if g.FundUsd > 0 {
		return g.FundUsd
	}
	return GroupBudgetUSD(dailyPoolUSD, g.BudgetPct)
}

// GroupBudgetUSD is this group's share of the daily pool (legacy).
func GroupBudgetUSD(dailyPoolUSD float64, budgetPct float64) float64 {
	if dailyPoolUSD <= 0 || budgetPct <= 0 {
		return 0
	}
	return dailyPoolUSD * budgetPct / 100.0
}

// GroupRemainingUSD subtracts already-spent amount for the NY day.
func GroupRemainingUSD(budgetUSD, spentUSD float64) float64 {
	rem := budgetUSD - spentUSD
	if rem < 0 {
		return 0
	}
	return rem
}

// StakeFromBands picks stakePct × remaining for the band matching popular price in cents.
func StakeFromBands(odds float64, bands []OddsBand, remainingUSD float64) (float64, bool) {
	if remainingUSD <= 0 || len(bands) == 0 {
		return 0, false
	}
	cents := odds * 100
	for _, b := range bands {
		if cents >= float64(b.MinCents)-1e-9 && cents <= float64(b.MaxCents)+1e-9 {
			stake := remainingUSD * b.StakePct / 100.0
			if stake <= 0 {
				return 0, false
			}
			return stake, true
		}
	}
	return 0, false
}
