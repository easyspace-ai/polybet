-- User-initiated: positions hidden from risk monitoring UI (per Polymarket account + token + side).

CREATE TABLE IF NOT EXISTS risk_hidden_positions (
    account_id TEXT NOT NULL,
    token_id    TEXT NOT NULL,
    side_label  TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (account_id, token_id, side_label)
);

CREATE INDEX IF NOT EXISTS idx_risk_hidden_account ON risk_hidden_positions(account_id);
