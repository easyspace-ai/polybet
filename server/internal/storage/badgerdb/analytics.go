package badgerdb

import (
	"context"
	"sort"
	"strings"
	"time"
)

// AnalyticsTradeRow is one closed position for the trades report.
type AnalyticsTradeRow struct {
	PositionID       string
	Title            string
	PolyEventSlug    string
	InvestedUSD      float64
	RealizedPnLUSD   float64
	ReturnPct        float64
	SettlementAt     time.Time
	SettlementSource string
	SettlementDate   string
	PendingOfficial  bool
	ClosedAt         time.Time
}

// AnalyticsDailyRow aggregates settled positions for one calendar day (ET).
type AnalyticsDailyRow struct {
	Date              string
	TotalInvestedUSD  float64
	TradeCount        int
	WinCount          int
	WinRate           float64
	ProfitUSD         float64
	ProfitAmountRate  float64
}

// AnalyticsTotals is the footer sum for a trades query.
type AnalyticsTotals struct {
	TotalInvestedUSD float64
	TotalProfitUSD   float64
	ReturnPct        float64
	TradeCount       int
}

type analyticsCacheEntry struct {
	rows    []AnalyticsTradeRow
	builtAt time.Time
}

// effectiveSettlementDate prefers official settlement day (ET), then closed-at fallback.
func effectiveSettlementDate(r AnalyticsTradeRow) string {
	if s := strings.TrimSpace(r.SettlementDate); s != "" {
		return s
	}
	if !r.ClosedAt.IsZero() {
		return settlementDateKeyET(r.ClosedAt)
	}
	return ""
}

func inAnalyticsDateRange(day, fromDate, toDate string) bool {
	if fromDate == "" && toDate == "" {
		return true
	}
	if day == "" {
		return false
	}
	if fromDate != "" && day < fromDate {
		return false
	}
	if toDate != "" && day > toDate {
		return false
	}
	return true
}

// ListClosedPositionsForAnalytics returns closed positions with realized PnL for an account.
func (d *DB) ListClosedPositionsForAnalytics(ctx context.Context, accountID string) ([]AnalyticsTradeRow, error) {
	if d == nil {
		return nil, nil
	}
	accountID = strings.TrimSpace(accountID)
	rows, err := d.scanPositions(ctx, func(p RiskPosDoc) bool {
		if accountID != "" && p.AccountID != accountID {
			return false
		}
		return p.Status == "closed" && p.RealizedPnLUSD != nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]AnalyticsTradeRow, 0, len(rows))
	for _, p := range rows {
		invested := p.InvestedUSD
		if invested <= 0 {
			invested = p.CostUSD
		}
		pnl := 0.0
		if p.RealizedPnLUSD != nil {
			pnl = *p.RealizedPnLUSD
		}
		retPct := 0.0
		if invested > 0 {
			retPct = pnl / invested * 100
		}
		var closedAt time.Time
		if p.ClosedAt != nil {
			closedAt = *p.ClosedAt
		}
		settleAt, settleSrc, pending := d.resolveSettlementForPosition(ctx, p.TokenID, closedAt)
		settlementDate := settlementDateKeyET(settleAt)
		if settlementDate == "" && !closedAt.IsZero() {
			settlementDate = settlementDateKeyET(closedAt)
		}
		row := AnalyticsTradeRow{
			PositionID:       p.ID,
			Title:            p.Title,
			PolyEventSlug:    p.PolyEventSlug,
			InvestedUSD:      invested,
			RealizedPnLUSD:   pnl,
			ReturnPct:        retPct,
			SettlementAt:     settleAt,
			SettlementSource: settleSrc,
			SettlementDate:   settlementDate,
			PendingOfficial:  pending,
			ClosedAt:         closedAt,
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SettlementDate != out[j].SettlementDate {
			return out[i].SettlementDate > out[j].SettlementDate
		}
		return out[i].ClosedAt.After(out[j].ClosedAt)
	})
	return out, nil
}

func (d *DB) resolveSettlementForPosition(ctx context.Context, tokenID string, closedAt time.Time) (time.Time, string, bool) {
	if t, src, ok := d.ResolvedAtForToken(ctx, tokenID); ok {
		return t, src, false
	}
	if !closedAt.IsZero() {
		return closedAt, "closed_at_fallback", true
	}
	return time.Time{}, "", true
}

// AggregateAnalyticsDaily groups trade rows by settlement date (ET) within [fromDate, toDate] inclusive.
// Dates are YYYY-MM-DD strings.
func AggregateAnalyticsDaily(rows []AnalyticsTradeRow, fromDate, toDate string) []AnalyticsDailyRow {
	type acc struct {
		invested float64
		count    int
		wins     int
		profit   float64
	}
	byDay := map[string]*acc{}
	for _, r := range rows {
		day := effectiveSettlementDate(r)
		if !inAnalyticsDateRange(day, fromDate, toDate) {
			continue
		}
		a := byDay[day]
		if a == nil {
			a = &acc{}
			byDay[day] = a
		}
		a.invested += r.InvestedUSD
		a.count++
		if r.RealizedPnLUSD > 0 {
			a.wins++
		}
		a.profit += r.RealizedPnLUSD
	}
	days := make([]string, 0, len(byDay))
	for d := range byDay {
		days = append(days, d)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(days)))
	out := make([]AnalyticsDailyRow, 0, len(days))
	for _, day := range days {
		a := byDay[day]
		winRate := 0.0
		if a.count > 0 {
			winRate = float64(a.wins) / float64(a.count) * 100
		}
		amtRate := 0.0
		if a.invested > 0 {
			amtRate = a.profit / a.invested * 100
		}
		out = append(out, AnalyticsDailyRow{
			Date:             day,
			TotalInvestedUSD: a.invested,
			TradeCount:       a.count,
			WinCount:         a.wins,
			WinRate:          winRate,
			ProfitUSD:        a.profit,
			ProfitAmountRate: amtRate,
		})
	}
	return out
}

// FilterAnalyticsTrades applies win/loss filter and returns rows plus totals.
func FilterAnalyticsTrades(rows []AnalyticsTradeRow, result string, fromDate, toDate string) ([]AnalyticsTradeRow, AnalyticsTotals) {
	result = strings.ToLower(strings.TrimSpace(result))
	filtered := make([]AnalyticsTradeRow, 0, len(rows))
	for _, r := range rows {
		if !inAnalyticsDateRange(effectiveSettlementDate(r), fromDate, toDate) {
			continue
		}
		switch result {
		case "win":
			if r.RealizedPnLUSD <= 0 {
				continue
			}
		case "loss":
			if r.RealizedPnLUSD >= 0 {
				continue
			}
		}
		filtered = append(filtered, r)
	}
	var tot AnalyticsTotals
	for _, r := range filtered {
		tot.TotalInvestedUSD += r.InvestedUSD
		tot.TotalProfitUSD += r.RealizedPnLUSD
		tot.TradeCount++
	}
	if tot.TotalInvestedUSD > 0 {
		tot.ReturnPct = tot.TotalProfitUSD / tot.TotalInvestedUSD * 100
	}
	return filtered, tot
}

// PaginateAnalyticsTrades slices filtered rows for HTTP pagination.
func PaginateAnalyticsTrades(rows []AnalyticsTradeRow, offset, limit int) []AnalyticsTradeRow {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || offset >= len(rows) {
		return nil
	}
	end := offset + limit
	if end > len(rows) {
		end = len(rows)
	}
	out := make([]AnalyticsTradeRow, end-offset)
	copy(out, rows[offset:end])
	return out
}
