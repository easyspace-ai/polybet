package badgerdb

import "database/sql"

// MarketRow is the flat market shape used by sync and HTTP (stored in Badger).
type MarketRow struct {
	ID         string
	EventID    string
	Platform   string
	ExternalID string
	Sport      string
	League     string
	HomeTeam   string
	AwayTeam   string
	StartTime  string
	Status     string
	BetType    string
	Line       sql.NullFloat64
	MainLine   int
	PolySlug      string
	EventVolume   float64
}

// OutcomeRow is one outcome joined for API / cache payloads.
type OutcomeRow struct {
	MarketID        string
	ID              string
	Label           string
	ExternalID      sql.NullString
	CurrentOdds     float64
	LiquidityDepth  float64
	LiquidityLevels sql.NullString
	LastUpdated     string
	CanonicalKey    sql.NullString
}
