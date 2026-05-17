package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// IsKnownStartTime reports whether t is a meaningful market start time
// (i.e. a real kickoff time written by the sync engine), as opposed to
// the sentinel zero/very-old time stored when the upstream Gamma feed
// could not be decoded.
//
// The threshold (year >= 2000) is deliberately permissive: any sane
// market schedule is in this millennium, and the only inputs we want
// to flag as "unknown" are time.Time{} (year 0001) and Unix epoch
// (year 1970) which arise from parse fallbacks in older code paths.
func IsKnownStartTime(t time.Time) bool {
	return !t.IsZero() && t.Year() >= 2000
}

// MarketStartTimeForToken returns the market start_time for the given CLOB
// token id. Returns ok=false when:
//   - the token does not map to any active outcome
//   - the joined market has no parseable start_time
//   - the start_time is the unknown sentinel (zero / very old)
//
// This is the trade-gate's hook to refuse new opens once the underlying
// game has kicked off — see EnsureTradeAllowed wiring.
func (s *Store) MarketStartTimeForToken(ctx context.Context, tokenID string) (time.Time, bool) {
	tid := NormalizeRiskCLOBTokenID(tokenID)
	if tid == "" {
		return time.Time{}, false
	}
	// Outcomes store the CLOB token id in external_id; some legacy rows may
	// hold the decimal-form token id. Try the canonical hex lookup first
	// then fall back to the trimmed input.
	rawTried := []string{tid, strings.TrimSpace(tokenID)}
	tried := make(map[string]struct{}, len(rawTried))
	for _, candidate := range rawTried {
		if candidate == "" {
			continue
		}
		if _, ok := tried[candidate]; ok {
			continue
		}
		tried[candidate] = struct{}{}
		var startStr sql.NullString
		err := s.db.QueryRowContext(ctx, `
			SELECT m.start_time
			FROM outcomes o
			JOIN markets m ON o.market_id = m.id
			WHERE o.external_id = ?
			LIMIT 1`, candidate).Scan(&startStr)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return time.Time{}, false
		}
		if !startStr.Valid || strings.TrimSpace(startStr.String) == "" {
			return time.Time{}, false
		}
		t := parseSQLiteTime(strings.TrimSpace(startStr.String))
		if !IsKnownStartTime(t) {
			return time.Time{}, false
		}
		return t, true
	}
	return time.Time{}, false
}
