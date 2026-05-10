package routersvc

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"

	"github.com/easyspace-ai/polybet/internal/bookcache"
	"github.com/easyspace-ai/polybet/internal/store"
)

type LiquidityLevel struct {
	Odds float64
	Size float64
}

type Allocation struct {
	Platform            string  `json:"platform"`
	OutcomeID           string  `json:"outcomeId"`
	ExternalMarketID    string  `json:"externalMarketId"`
	ExternalOutcomeID   string  `json:"externalOutcomeId"`
	Size                float64 `json:"size"`
	ExpectedOdds        float64 `json:"expectedOdds"`
	EstimatedSlippage   float64 `json:"estimatedSlippage"`
}

type AllocationPlan struct {
	Allocations   []Allocation `json:"allocations"`
	TotalSize     float64      `json:"totalSize"`
	WeightedOdds  float64      `json:"weightedOdds"`
	TotalSlippage float64      `json:"totalSlippage"`
}

type RouterError struct {
	Code    string  `json:"code"`
	Message string  `json:"message"`
	Detail  any     `json:"detail,omitempty"`
}

type RouterResult struct {
	OK    bool
	Plan  *AllocationPlan
	Error *RouterError
}

func liveLevels(cache *bookcache.Cache, extID sql.NullString, levelsJSON sql.NullString, currentOdds, liqDepth float64) []LiquidityLevel {
	if extID.Valid && extID.String != "" {
		lv := cache.GetLevels(extID.String)
		if len(lv) > 0 {
			out := make([]LiquidityLevel, 0, len(lv))
			for _, l := range lv {
				out = append(out, LiquidityLevel{Odds: l.Odds, Size: l.Size})
			}
			return out
		}
	}
	if levelsJSON.Valid && levelsJSON.String != "" {
		var parsed []LiquidityLevel
		if json.Unmarshal([]byte(levelsJSON.String), &parsed) == nil && len(parsed) > 0 {
			return parsed
		}
	}
	if liqDepth > 0 && currentOdds > 0 {
		return []LiquidityLevel{{Odds: currentOdds, Size: liqDepth}}
	}
	return nil
}

func BuildAllocationPlan(ctx context.Context, st *store.Store, cache *bookcache.Cache, primaryOutcomeID string, side string, size float64) RouterResult {
	_ = side
	maxTrade := st.GetBotConfigFloat(ctx, "maxTradeSize", 100)
	slipTol := st.GetBotConfigFloat(ctx, "slippageTolerance", 0.05)
	if size > maxTrade {
		return RouterResult{Error: &RouterError{Code: "size_exceeds_max", Message: "size exceeds maxTradeSize"}}
	}
	rows, err := st.ListRouterPolySiblings(ctx, primaryOutcomeID)
	if err != nil || len(rows) == 0 {
		return RouterResult{Error: &RouterError{Code: "outcome_not_found", Message: "outcome not found"}}
	}

	type cand struct {
		outcomeID        string
		extOutcome       string
		marketExternalID string
		levels           []LiquidityLevel
		bestOdds         float64
		totalAvail       float64
	}
	var candidates []cand
	for _, o := range rows {
		lv := liveLevels(cache, o.ExternalID, o.LiquidityLevels, o.CurrentOdds, o.LiquidityDepth)
		tot := 0.0
		for _, l := range lv {
			tot += l.Size
		}
		best := o.CurrentOdds
		if len(lv) > 0 {
			best = lv[0].Odds
		}
		extTok := ""
		if o.ExternalID.Valid {
			extTok = o.ExternalID.String
		}
		candidates = append(candidates, cand{
			outcomeID:        o.OutcomeID,
			extOutcome:       extTok,
			marketExternalID: o.MarketExternalID,
			levels:           lv,
			bestOdds:         best,
			totalAvail:       tot,
		})
	}

	type lvlSrc struct {
		odds         float64
		size         float64
		candidateIdx int
	}
	var all []lvlSrc
	for i, c := range candidates {
		for _, lv := range c.levels {
			all = append(all, lvlSrc{odds: lv.Odds, size: lv.Size, candidateIdx: i})
		}
	}
	if len(all) == 0 {
		return RouterResult{Error: &RouterError{Code: "no_liquidity", Message: "No liquidity available for this outcome"}}
	}
	// ascending odds (best first)
	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			if all[j].odds < all[i].odds {
				all[i], all[j] = all[j], all[i]
			}
		}
	}
	globalBest := all[0].odds
	per := make([]struct{ filled, wsum float64 }, len(candidates))
	rem := size
	for _, l := range all {
		if rem <= 0 {
			break
		}
		take := math.Min(rem, l.size)
		per[l.candidateIdx].filled += take
		per[l.candidateIdx].wsum += l.odds * take
		rem -= take
	}
	total := 0.0
	for _, p := range per {
		total += p.filled
	}
	if total <= 0 {
		return RouterResult{Error: &RouterError{Code: "no_liquidity", Message: "No liquidity available for this outcome"}}
	}
	weighted := 0.0
	for _, p := range per {
		weighted += p.wsum
	}
	weighted /= total
	totalSlip := 0.0
	if globalBest > 0 {
		totalSlip = math.Max(0, (weighted-globalBest)/globalBest)
	}
	if totalSlip > slipTol {
		return RouterResult{Error: &RouterError{
			Code: "slippage_exceeded", Message: "slippage exceeded",
			Detail: map[string]any{"slippage": totalSlip, "tolerance": slipTol},
		}}
	}
	var allocs []Allocation
	for i, c := range candidates {
		if per[i].filled <= 0 {
			continue
		}
		allocs = append(allocs, Allocation{
			Platform:          "polymarket",
			OutcomeID:         c.outcomeID,
			ExternalMarketID:  c.marketExternalID,
			ExternalOutcomeID: c.extOutcome,
			Size:              per[i].filled,
			ExpectedOdds:      per[i].wsum / per[i].filled,
			EstimatedSlippage: totalSlip,
		})
	}
	if len(allocs) == 0 {
		return RouterResult{Error: &RouterError{Code: "no_liquidity", Message: "No liquidity available for this outcome"}}
	}
	return RouterResult{OK: true, Plan: &AllocationPlan{
		Allocations:   allocs,
		TotalSize:     total,
		WeightedOdds:  weighted,
		TotalSlippage: totalSlip,
	}}
}
