package domain

import "time"

// LiquidityDepth mirrors bot/types LiquidityDepth.
type LiquidityDepth struct {
	AvailableSize float64
	TopLevels     []struct {
		Odds float64
		Size float64
	}
}

type OutcomeOdds struct {
	Label           string
	ImpliedOdds     float64
	LiquidityDepth  LiquidityDepth
	ExternalID      string // CLOB token id for Polymarket
}

type MarketQuote struct {
	Platform     string
	ExternalID   string
	Sport        string
	League       string
	HomeTeam     string
	AwayTeam     string
	Name         string
	StartTime    time.Time
	BetType      string
	Line         *float64
	MainLine     bool
	Outcomes     []OutcomeOdds
	PolyEventID  string
}
