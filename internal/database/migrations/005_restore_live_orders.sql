-- 005_restore_live_orders.sql
-- Restore fill_key on trades for worker deduplication.
-- fill_key is NULL for PlaceOrder trades (not needed), non-null for worker fills.
-- PostgreSQL UNIQUE ignores NULLs so multiple NULL values are allowed.

ALTER TABLE trades ADD COLUMN IF NOT EXISTS fill_key TEXT;
ALTER TABLE trades DROP CONSTRAINT IF EXISTS trades_fill_key_unique;
ALTER TABLE trades ADD CONSTRAINT trades_fill_key_unique UNIQUE (fill_key);
