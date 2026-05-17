-- Track realized PnL and closure timestamp on every risk_positions row so
-- the kill-switch evaluator can include closed-today losses (currently
-- only unrealized PnL on open positions counts).
--
-- realized_pnl_usd is nullable: NULL on open positions and on rows closed
-- via the legacy code path that didn't compute PnL (e.g. dust closures
-- where we don't have a meaningful sale price). The kill switch sums only
-- non-NULL values so legacy data does not poison the aggregate.
--
-- closed_at is the wall-clock time the position transitioned to status
-- 'closed'. Independent of updated_at which moves on every UPDATE.
ALTER TABLE risk_positions ADD COLUMN realized_pnl_usd REAL DEFAULT NULL;
ALTER TABLE risk_positions ADD COLUMN closed_at TEXT DEFAULT NULL;

-- Index for the kill-switch sum-since-today query path.
CREATE INDEX IF NOT EXISTS idx_risk_positions_account_closed_at
    ON risk_positions(account_id, closed_at);
