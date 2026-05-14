-- Migration: separate official position data from local risk configs
-- NOTE: this migration assumes a fresh DB or that old data will be rebuilt.

CREATE TABLE IF NOT EXISTS risk_position_configs (
    position_id TEXT PRIMARY KEY REFERENCES risk_positions(id) ON DELETE CASCADE,
    high_water_cents REAL NOT NULL,
    stop_loss_pct    REAL NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

ALTER TABLE risk_tasks ADD COLUMN reason TEXT;
ALTER TABLE trades ADD COLUMN source TEXT DEFAULT 'bot';

CREATE INDEX IF NOT EXISTS idx_risk_tasks_reason ON risk_tasks(reason, status);
CREATE INDEX IF NOT EXISTS idx_trades_source     ON trades(source);
