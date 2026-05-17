package store

import "database/sql"

// RouterOutcome is one row for allocation routing (Polymarket siblings only).
type RouterOutcome struct {
	OutcomeID        string
	Label            string
	ExternalID       sql.NullString
	CurrentOdds      float64
	LiquidityDepth   float64
	LiquidityLevels  sql.NullString
	CanonicalBetID   sql.NullString
	MarketID         string
	MarketExternalID string
	MarketPlatform   string
	MarketStatus     string
}
