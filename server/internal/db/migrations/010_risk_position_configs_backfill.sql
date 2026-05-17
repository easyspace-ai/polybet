-- Backfill risk_position_configs for any open risk_positions row that is
-- missing a config (legacy rows from before migration 004, or rows whose
-- config was deleted out-of-band). New positions always get a config row
-- inserted by store.CreateRiskPosition; this migration is a one-time
-- safety net that closes the gap between the previous SQL fallback (10%)
-- and the configured default (20%) so legacy positions stop running on a
-- silently tighter trail than the dashboard reports.
--
-- High-water defaults to the position's avg_entry_cents which matches what
-- a fresh position gets at open. Stop-loss-pct uses the constant value
-- carried in store.DefaultStopLossPct (20).
--
-- The INSERT OR IGNORE pattern means rows that already have a config are
-- left untouched; only the gap is filled. Idempotent across re-runs.
INSERT OR IGNORE INTO risk_position_configs(position_id, high_water_cents, stop_loss_pct, created_at, updated_at)
SELECT rp.id, rp.avg_entry_cents, 20.0, datetime('now'), datetime('now')
FROM risk_positions rp
LEFT JOIN risk_position_configs rpc ON rp.id = rpc.position_id
WHERE rpc.position_id IS NULL;
