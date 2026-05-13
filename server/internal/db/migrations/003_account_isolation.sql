-- Per-account isolation for trades and risk positions

ALTER TABLE trades ADD COLUMN account_id TEXT REFERENCES polymarket_accounts(id);
CREATE INDEX idx_trades_account ON trades(account_id);

ALTER TABLE risk_positions ADD COLUMN account_id TEXT REFERENCES polymarket_accounts(id);
CREATE INDEX idx_risk_positions_account ON risk_positions(account_id);

ALTER TABLE risk_applied_clob_trades ADD COLUMN account_id TEXT REFERENCES polymarket_accounts(id);
CREATE INDEX idx_risk_applied_account ON risk_applied_clob_trades(account_id);
