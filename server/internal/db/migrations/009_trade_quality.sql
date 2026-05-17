-- trade_quality stores per-fill execution quality metrics for slippage analysis.
-- Populated by tradesvc (BUY) and risksvc close paths (SELL / hedge BUY).
CREATE TABLE IF NOT EXISTS trade_quality (
    id              TEXT PRIMARY KEY,
    created_at      TEXT NOT NULL,
    account_id      TEXT,
    side            TEXT NOT NULL,                 -- 'buy' | 'sell'
    order_type      TEXT NOT NULL,                 -- 'FOK' | 'FAK' | hedge variants
    token_id        TEXT NOT NULL,
    -- Reference price the strategy expected at decision time (0–1 probability).
    expected_odds   REAL,
    -- Actual fill price recorded by the CLOB (0–1 probability).
    fill_odds       REAL,
    -- Limit price submitted to CLOB (0–1).
    limit_odds      REAL,
    -- Best bid/ask observed at submit time (0–1).
    best_bid        REAL,
    best_ask        REAL,
    -- Slippage in basis points: positive = WORSE than expected for the side.
    --   buy:  (fill - expected) / expected * 10000
    --   sell: (expected - fill) / expected * 10000
    slippage_bps    REAL,
    -- Notional size in USDC for buys, shares for sells.
    size            REAL,
    -- Execution latency from order build start to CLOB ack (ms).
    submit_latency_ms INTEGER,
    -- Optional foreign key to trades.id (nullable; close path doesn't insert into trades).
    trade_id        TEXT,
    -- Optional task_id linking back to the close task that generated the SELL.
    risk_task_id    TEXT,
    -- Free-form context (e.g. ladder tier, refresh telemetry).
    notes           TEXT
);
CREATE INDEX IF NOT EXISTS idx_trade_quality_token_at ON trade_quality(token_id, created_at);
CREATE INDEX IF NOT EXISTS idx_trade_quality_account_at ON trade_quality(account_id, created_at);
