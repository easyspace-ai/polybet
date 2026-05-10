package store

import (
	"context"
	"database/sql"
)

// OutcomeRow is a joined outcome for API responses.
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
}

func (s *Store) ListActiveMarketsFlat(ctx context.Context) ([]MarketRow, []OutcomeRow, error) {
	mrows, err := s.db.QueryContext(ctx, `
		SELECT m.id, m.event_id, m.platform, m.external_id, e.sport, e.league, e.home_team, e.away_team,
		       m.start_time, m.status, m.bet_type, m.line, m.main_line
		FROM markets m JOIN events e ON m.event_id = e.id
		WHERE m.status = 'active' AND e.status = 'active' ORDER BY m.start_time ASC`)
	if err != nil {
		return nil, nil, err
	}
	defer mrows.Close()
	markets := make([]MarketRow, 0)
	marketIDs := make(map[string]struct{})
	for mrows.Next() {
		var m MarketRow
		if err := mrows.Scan(&m.ID, &m.EventID, &m.Platform, &m.ExternalID, &m.Sport, &m.League, &m.HomeTeam, &m.AwayTeam,
			&m.StartTime, &m.Status, &m.BetType, &m.Line, &m.MainLine); err != nil {
			return nil, nil, err
		}
		markets = append(markets, m)
		marketIDs[m.ID] = struct{}{}
	}
	if err := mrows.Err(); err != nil {
		return nil, nil, err
	}
	if len(marketIDs) == 0 {
		if markets == nil {
			markets = make([]MarketRow, 0)
		}
		return markets, make([]OutcomeRow, 0), nil
	}
	ids := make([]string, 0, len(marketIDs))
	for id := range marketIDs {
		ids = append(ids, id)
	}
	// simple IN query - sqlite limit - batch if needed
	orows, err := s.db.QueryContext(ctx, `
		SELECT o.id, o.market_id, o.label, o.external_id, o.current_odds, o.liquidity_depth, o.liquidity_levels, o.last_updated, c.key AS canonical_key
		FROM outcomes o
		LEFT JOIN canonical_bets c ON o.canonical_bet_id = c.id
		WHERE o.market_id IN (`+placeholders(len(ids))+`)`, argsStrings(ids)...)
	if err != nil {
		return nil, nil, err
	}
	defer orows.Close()
	outcomes := make([]OutcomeRow, 0)
	for orows.Next() {
		var o OutcomeRow
		if err := orows.Scan(&o.ID, &o.MarketID, &o.Label, &o.ExternalID, &o.CurrentOdds, &o.LiquidityDepth,
			&o.LiquidityLevels, &o.LastUpdated, &o.CanonicalKey); err != nil {
			return nil, nil, err
		}
		outcomes = append(outcomes, o)
	}
	return markets, outcomes, orows.Err()
}

func placeholders(n int) string {
	if n == 0 {
		return ""
	}
	b := make([]byte, 0, n*2)
	for i := 0; i < n; i++ {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '?')
	}
	return string(b)
}

func argsStrings(ids []string) []any {
	a := make([]any, len(ids))
	for i := range ids {
		a[i] = ids[i]
	}
	return a
}

func (s *Store) GetOutcomeWithMarket(ctx context.Context, outcomeID string) (outcomeIDRet, marketID, label, extID, home, away string, err error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT o.id, o.market_id, o.label, COALESCE(o.external_id,''), e.home_team, e.away_team
		FROM outcomes o JOIN markets m ON o.market_id = m.id JOIN events e ON m.event_id = e.id
		WHERE o.id = ?`, outcomeID)
	err = row.Scan(&outcomeIDRet, &marketID, &label, &extID, &home, &away)
	return
}
