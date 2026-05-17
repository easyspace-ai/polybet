-- Add realized_pnl_usd to trade_quality so the analytics aggregate endpoint
-- can show realized losses per token / per account in addition to slippage.
-- The column is nullable: open BUY rows and rows from before this migration
-- store NULL; the close-side rows backfill it on every fresh fill.
--
-- Plan for /api/trade-quality/aggregate v2:
--   sum(realized_pnl_usd) per side, per orderType so dashboards can show
--   "today's realized PnL split by close mode" and spot if FAK is bleeding
--   more than FOK after the slippage cap is in place.
ALTER TABLE trade_quality ADD COLUMN realized_pnl_usd REAL DEFAULT NULL;
