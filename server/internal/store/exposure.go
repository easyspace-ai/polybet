package store

import (
	"context"
	"strings"
	"time"
)

// AccountRealizedPnLSince returns the sum of realized_pnl_usd across all
// risk_positions for the given account that closed on or after `since`.
// Used by the kill-switch evaluator so today's already-realized losses
// (closed positions) count alongside the unrealized mark-to-market on
// open positions.
//
// Rows whose realized_pnl_usd is NULL (legacy / dust / ghost-balance
// closures with no reliable fill price) are excluded from the sum so
// missing data does not poison the aggregate. Operators relying on
// dust-heavy closures may want to track those out-of-band.
func (s *Store) AccountRealizedPnLSince(ctx context.Context, accountID string, since time.Time) (float64, error) {
	if strings.TrimSpace(accountID) == "" {
		return 0, nil
	}
	if since.IsZero() {
		// All-time aggregate is rarely useful and can overflow on long-
		// running deployments; require a window to opt-in explicitly.
		return 0, nil
	}
	var sum float64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(realized_pnl_usd), 0) FROM risk_positions
		 WHERE account_id = ? AND status = 'closed'
		   AND realized_pnl_usd IS NOT NULL
		   AND closed_at IS NOT NULL AND closed_at >= ?`,
		accountID, since.UTC().Format(time.RFC3339Nano)).Scan(&sum)
	if err != nil {
		return 0, err
	}
	return sum, nil
}

// AccountOpenExposureUSD returns the sum of cost_usd across all open
// positions for the given account. Used by the trade gate to enforce a
// per-account notional cap on new opens.
//
// Note: cost_usd is the entry-cost basis, not the current mark-to-market
// notional. Using cost is the conservative choice for an OPEN-side cap
// because it ignores price drift in either direction:
//   - On a winning book, mark-to-market would inflate the apparent
//     exposure, blocking new sensible trades.
//   - On a losing book, mark-to-market would shrink it, masking risk.
//
// Operators who want a strict mark-to-market cap should layer the kill
// switch (riskMaxDailyLossUSD) on top.
func (s *Store) AccountOpenExposureUSD(ctx context.Context, accountID string) (float64, error) {
	if strings.TrimSpace(accountID) == "" {
		return 0, nil
	}
	var sum float64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(cost_usd), 0) FROM risk_positions
		 WHERE status = 'open' AND account_id = ?`, accountID).Scan(&sum)
	if err != nil {
		return 0, err
	}
	return sum, nil
}

// MarketOpenExposureUSD returns the sum of cost_usd for open positions on
// a single Polymarket binary event (grouped by poly_event_slug). Both
// outcomes of the same moneyline (YES on Team A and YES on Team B) count
// together so the cap reflects total exposure to that game's outcome.
//
// Returns 0 when the slug is empty or no open position exists.
func (s *Store) MarketOpenExposureUSD(ctx context.Context, accountID, polyEventSlug string) (float64, error) {
	if strings.TrimSpace(accountID) == "" {
		return 0, nil
	}
	slug := strings.Trim(strings.TrimPrefix(strings.TrimSpace(polyEventSlug), "event/"), "/")
	if slug == "" {
		return 0, nil
	}
	var sum float64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(cost_usd), 0) FROM risk_positions
		 WHERE status = 'open' AND account_id = ? AND poly_event_slug = ?`,
		accountID, slug).Scan(&sum)
	if err != nil {
		return 0, err
	}
	return sum, nil
}

// PolyEventSlugForToken resolves the Gamma event slug for a given CLOB
// token id by joining outcomes → markets → events. Returns "" when the
// token is not present in the markets table or the event has no slug.
func (s *Store) PolyEventSlugForToken(ctx context.Context, tokenID string) string {
	tid := NormalizeRiskCLOBTokenID(tokenID)
	if tid == "" {
		return ""
	}
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
		var slug string
		err := s.db.QueryRowContext(ctx,
			`SELECT COALESCE(e.poly_slug, '')
			 FROM outcomes o
			 JOIN markets m ON o.market_id = m.id
			 JOIN events e ON m.event_id = e.id
			 WHERE o.external_id = ?
			 LIMIT 1`, candidate).Scan(&slug)
		if err != nil {
			continue
		}
		slug = strings.Trim(strings.TrimPrefix(strings.TrimSpace(slug), "event/"), "/")
		if slug != "" {
			return slug
		}
	}
	return ""
}
