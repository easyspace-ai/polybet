-- Schema aligned with bot/prisma/schema.prisma (SQLite)

CREATE TABLE IF NOT EXISTS events (
    id TEXT PRIMARY KEY,
    sport TEXT NOT NULL,
    league TEXT NOT NULL,
    home_team TEXT NOT NULL,
    away_team TEXT NOT NULL,
    start_time TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    sx_event_id TEXT,
    poly_event_id TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS markets (
    id TEXT PRIMARY KEY,
    event_id TEXT NOT NULL REFERENCES events(id),
    platform TEXT NOT NULL,
    external_id TEXT NOT NULL,
    start_time TEXT NOT NULL,
    bet_type TEXT NOT NULL DEFAULT '1x2',
    line REAL,
    main_line INTEGER NOT NULL DEFAULT 1,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(platform, external_id)
);

CREATE INDEX IF NOT EXISTS idx_markets_event ON markets(event_id);
CREATE INDEX IF NOT EXISTS idx_markets_status ON markets(status);

CREATE TABLE IF NOT EXISTS canonical_bets (
    id TEXT PRIMARY KEY,
    event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    key TEXT NOT NULL,
    bet_type TEXT NOT NULL,
    side TEXT NOT NULL,
    line REAL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(event_id, key)
);

CREATE TABLE IF NOT EXISTS outcomes (
    id TEXT PRIMARY KEY,
    market_id TEXT NOT NULL REFERENCES markets(id),
    label TEXT NOT NULL,
    external_id TEXT,
    current_odds REAL NOT NULL,
    liquidity_depth REAL NOT NULL,
    liquidity_levels TEXT,
    last_updated TEXT NOT NULL DEFAULT (datetime('now')),
    canonical_bet_id TEXT REFERENCES canonical_bets(id)
);

CREATE INDEX IF NOT EXISTS idx_outcomes_market ON outcomes(market_id);
CREATE INDEX IF NOT EXISTS idx_outcomes_canonical ON outcomes(canonical_bet_id);

CREATE TABLE IF NOT EXISTS team_alias (
    id TEXT PRIMARY KEY,
    canonical TEXT NOT NULL,
    platform TEXT NOT NULL,
    alias TEXT NOT NULL,
    league TEXT NOT NULL,
    UNIQUE(platform, alias, league)
);

CREATE TABLE IF NOT EXISTS trades (
    id TEXT PRIMARY KEY,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    market_id TEXT NOT NULL REFERENCES markets(id),
    outcome_id TEXT NOT NULL REFERENCES outcomes(id),
    side TEXT NOT NULL,
    requested_size REAL NOT NULL,
    executed_size REAL,
    requested_odds REAL NOT NULL,
    fill_odds REAL,
    platform TEXT NOT NULL,
    tx_hash TEXT,
    status TEXT NOT NULL,
    failure_reason TEXT
);

CREATE TABLE IF NOT EXISTS bot_config (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS polymarket_accounts (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    api_key TEXT NOT NULL,
    secret TEXT NOT NULL,
    passphrase TEXT NOT NULL,
    private_key TEXT NOT NULL,
    funder_address TEXT NOT NULL,
    is_active INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS risk_positions (
    id TEXT PRIMARY KEY,
    platform TEXT NOT NULL DEFAULT 'polymarket',
    outcome_id TEXT REFERENCES outcomes(id) ON DELETE SET NULL,
    token_id TEXT NOT NULL,
    title TEXT NOT NULL,
    side_label TEXT NOT NULL,
    avg_entry_cents REAL NOT NULL,
    size_shares REAL NOT NULL,
    cost_usd REAL NOT NULL,
    high_water_cents REAL NOT NULL,
    stop_loss_pct REAL NOT NULL,
    source TEXT NOT NULL DEFAULT 'bot',
    status TEXT NOT NULL DEFAULT 'open',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_risk_positions_status ON risk_positions(status);
CREATE INDEX IF NOT EXISTS idx_risk_positions_token ON risk_positions(token_id);

CREATE TABLE IF NOT EXISTS risk_applied_clob_trades (
    id TEXT PRIMARY KEY,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS risk_tasks (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    position_id TEXT REFERENCES risk_positions(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    next_run_at TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_risk_tasks_status_next ON risk_tasks(status, next_run_at);
