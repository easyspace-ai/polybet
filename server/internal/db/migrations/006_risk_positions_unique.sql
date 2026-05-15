-- At most one open/closing risk row per logical key (Polymarket account + CLOB token + side).
-- Application code runs normalizeAndDedupeRiskPositions() immediately before this migration step.

CREATE UNIQUE INDEX IF NOT EXISTS idx_risk_positions_open_key
ON risk_positions(COALESCE(account_id, ''), token_id, side_label)
WHERE status IN ('open', 'closing');
