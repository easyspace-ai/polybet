package store

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/easyspace-ai/polybet/internal/domain"
)

// TradeQuality is one row of execution-quality telemetry. Populated on every
// fill (and optionally on submit failures so retries can be analysed).
//
// Slippage convention (slippageBps):
//
//	BUY:  positive means we paid MORE than expected (= worse for us)
//	SELL: positive means we received LESS than expected (= worse for us)
//	      Both directions therefore agree: positive bps == worse fill.
type TradeQuality struct {
	ID              string
	CreatedAt       time.Time
	AccountID       string
	Side            string  // buy | sell
	OrderType       string  // FOK | FAK | hedge_fok_buy
	TokenID         string
	ExpectedOdds    float64 // 0–1 probability
	FillOdds        float64 // 0–1 probability
	LimitOdds       float64 // 0–1 probability
	BestBid         float64
	BestAsk         float64
	SlippageBps     float64
	Size            float64
	SubmitLatencyMs int64
	TradeID         string
	RiskTaskID      string
	Notes           string
	// RealizedPnLUSD is populated on SELL completions where the close path
	// could compute it (FOK exact fill, FAK proxy). NULL on BUY rows and
	// on dust / ghost-balance closures. Sum-aggregated by the realized-PnL
	// analytics endpoint.
	RealizedPnLUSD float64
}

// SlippageBpsBuy returns the buy-side slippage in basis points: > 0 means the
// fill was worse than expected (paid more for the same outcome). When inputs
// are non-positive or NaN, returns 0.
func SlippageBpsBuy(expected, fill float64) float64 {
	if expected <= 0 || fill <= 0 {
		return 0
	}
	return (fill - expected) / expected * 10000.0
}

// SlippageBpsSell returns the sell-side slippage in basis points: > 0 means
// the fill was worse than expected (received less). When inputs are
// non-positive or NaN, returns 0.
func SlippageBpsSell(expected, fill float64) float64 {
	if expected <= 0 || fill <= 0 {
		return 0
	}
	return (expected - fill) / expected * 10000.0
}

// InsertTradeQuality persists a quality row. ID is generated when empty.
// CreatedAt defaults to now (UTC) when zero.
func (s *Store) InsertTradeQuality(ctx context.Context, q *TradeQuality) error {
	if q == nil {
		return nil
	}
	if strings.TrimSpace(q.ID) == "" {
		q.ID = domain.NewID()
	}
	if q.CreatedAt.IsZero() {
		q.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO trade_quality(
			id, created_at, account_id, side, order_type, token_id,
			expected_odds, fill_odds, limit_odds, best_bid, best_ask, slippage_bps,
			size, submit_latency_ms, trade_id, risk_task_id, notes, realized_pnl_usd
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		q.ID, q.CreatedAt.UTC().Format(time.RFC3339Nano),
		nullableStr(q.AccountID), q.Side, q.OrderType, q.TokenID,
		nullableFloat(q.ExpectedOdds), nullableFloat(q.FillOdds), nullableFloat(q.LimitOdds),
		nullableFloat(q.BestBid), nullableFloat(q.BestAsk), nullableFloat(q.SlippageBps),
		nullableFloat(q.Size), nullableInt(q.SubmitLatencyMs),
		nullableStr(q.TradeID), nullableStr(q.RiskTaskID), nullableStr(q.Notes),
		nullableFloat(q.RealizedPnLUSD),
	)
	return err
}

// TradeQualityAggregate is a summary across a window.
type TradeQualityAggregate struct {
	Count          int     `json:"count"`
	AvgSlippageBps float64 `json:"avgSlippageBps"`
	MaxSlippageBps float64 `json:"maxSlippageBps"`
	BuyCount       int     `json:"buyCount"`
	SellCount      int     `json:"sellCount"`
	BuyAvgBps      float64 `json:"buyAvgBps"`
	SellAvgBps     float64 `json:"sellAvgBps"`
	// RealizedPnLUSD sums realized_pnl_usd over the window. Only SELL
	// rows that recorded a fill price contribute (BUY rows + dust /
	// ghost-balance closures stay NULL and are excluded by the WHERE).
	RealizedPnLUSD float64 `json:"realizedPnlUsd"`
}

// AggregateTradeQuality returns aggregate slippage stats for the given
// account across rows newer than `since`. accountID empty = all accounts.
func (s *Store) AggregateTradeQuality(ctx context.Context, accountID string, since time.Time) (TradeQualityAggregate, error) {
	out := TradeQualityAggregate{}
	args := []any{}
	where := []string{"slippage_bps IS NOT NULL"}
	if strings.TrimSpace(accountID) != "" {
		where = append(where, "account_id = ?")
		args = append(args, accountID)
	}
	if !since.IsZero() {
		where = append(where, "created_at >= ?")
		args = append(args, since.UTC().Format(time.RFC3339Nano))
	}
	q := `SELECT
            COUNT(1),
            COALESCE(AVG(slippage_bps), 0),
            COALESCE(MAX(slippage_bps), 0),
            SUM(CASE WHEN side='buy' THEN 1 ELSE 0 END),
            SUM(CASE WHEN side='sell' THEN 1 ELSE 0 END),
            COALESCE(AVG(CASE WHEN side='buy' THEN slippage_bps END), 0),
            COALESCE(AVG(CASE WHEN side='sell' THEN slippage_bps END), 0),
            COALESCE(SUM(realized_pnl_usd), 0)
        FROM trade_quality WHERE ` + strings.Join(where, " AND ")
	row := s.db.QueryRowContext(ctx, q, args...)
	var buyCount, sellCount sql.NullInt64
	if err := row.Scan(
		&out.Count,
		&out.AvgSlippageBps,
		&out.MaxSlippageBps,
		&buyCount,
		&sellCount,
		&out.BuyAvgBps,
		&out.SellAvgBps,
		&out.RealizedPnLUSD,
	); err != nil {
		return out, err
	}
	if buyCount.Valid {
		out.BuyCount = int(buyCount.Int64)
	}
	if sellCount.Valid {
		out.SellCount = int(sellCount.Int64)
	}
	return out, nil
}

// EventRealizedPnL is one row of the per-event realized PnL aggregator.
// Rendered by /api/risk/realized-pnl-by-event for operator review.
type EventRealizedPnL struct {
	PolyEventSlug  string  `json:"polyEventSlug"`
	RealizedPnLUSD float64 `json:"realizedPnlUsd"`
	Fills          int     `json:"fills"`
}

// RealizedPnLByEvent groups SUM(realized_pnl_usd) by Polymarket event slug
// across SELL fills on the given account within the rolling window. Joins
// trade_quality.token_id → outcomes.external_id → markets → events to
// resolve the slug; rows whose token doesn't map to a synced event are
// dropped. Sorted with most-negative (worst) PnL first so the operator
// sees the costliest games at the top.
//
// Self-contained against trade_quality + the existing markets/events
// hierarchy (no dependency on risk_positions.closed_at / realized_pnl_usd
// columns) so it ships independently of any other in-flight DB changes.
func (s *Store) RealizedPnLByEvent(ctx context.Context, accountID string, since time.Time, limit int) ([]EventRealizedPnL, error) {
	if strings.TrimSpace(accountID) == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	args := []any{accountID}
	whereTime := ""
	if !since.IsZero() {
		whereTime = " AND tq.created_at >= ?"
		args = append(args, since.UTC().Format(time.RFC3339Nano))
	}
	args = append(args, limit)
	q := `SELECT COALESCE(e.poly_slug,''),
	             COALESCE(SUM(tq.realized_pnl_usd), 0),
	             COUNT(1)
	      FROM trade_quality tq
	      JOIN outcomes o ON o.external_id = tq.token_id
	      JOIN markets m  ON m.id = o.market_id
	      JOIN events e   ON e.id = m.event_id
	      WHERE tq.account_id = ? AND tq.side = 'sell'
	            AND tq.realized_pnl_usd IS NOT NULL
	            AND COALESCE(e.poly_slug,'') != ''` + whereTime + `
	      GROUP BY e.poly_slug
	      ORDER BY SUM(tq.realized_pnl_usd) ASC
	      LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]EventRealizedPnL, 0)
	for rows.Next() {
		var r EventRealizedPnL
		if err := rows.Scan(&r.PolyEventSlug, &r.RealizedPnLUSD, &r.Fills); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListRecentTradeQuality returns the most recent N rows (newest first).
// accountID empty = all accounts.
func (s *Store) ListRecentTradeQuality(ctx context.Context, accountID string, limit int) ([]TradeQuality, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	args := []any{}
	where := ""
	if strings.TrimSpace(accountID) != "" {
		where = "WHERE account_id = ?"
		args = append(args, accountID)
	}
	args = append(args, limit)
	q := `SELECT id, created_at, COALESCE(account_id,''), side, order_type, token_id,
			COALESCE(expected_odds,0), COALESCE(fill_odds,0), COALESCE(limit_odds,0),
			COALESCE(best_bid,0), COALESCE(best_ask,0), COALESCE(slippage_bps,0),
			COALESCE(size,0), COALESCE(submit_latency_ms,0),
			COALESCE(trade_id,''), COALESCE(risk_task_id,''), COALESCE(notes,''),
			COALESCE(realized_pnl_usd,0)
		FROM trade_quality ` + where + ` ORDER BY created_at DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TradeQuality, 0)
	for rows.Next() {
		var t TradeQuality
		var created string
		if err := rows.Scan(&t.ID, &created, &t.AccountID, &t.Side, &t.OrderType, &t.TokenID,
			&t.ExpectedOdds, &t.FillOdds, &t.LimitOdds, &t.BestBid, &t.BestAsk, &t.SlippageBps,
			&t.Size, &t.SubmitLatencyMs, &t.TradeID, &t.RiskTaskID, &t.Notes, &t.RealizedPnLUSD); err != nil {
			return nil, err
		}
		t.CreatedAt = parseSQLiteTime(created)
		out = append(out, t)
	}
	return out, rows.Err()
}

func nullableStr(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func nullableFloat(f float64) any {
	if f == 0 {
		return nil
	}
	return f
}

func nullableInt(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}
