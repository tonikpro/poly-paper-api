-- Remove columns only needed for orderbook matching.
-- fill_key UNIQUE constraint is auto-dropped with the column.

ALTER TABLE orders DROP COLUMN IF EXISTS associate_trades;

ALTER TABLE trades DROP COLUMN IF EXISTS fill_key;
ALTER TABLE trades DROP COLUMN IF EXISTS transaction_hash;
ALTER TABLE trades DROP COLUMN IF EXISTS maker_orders;
ALTER TABLE trades DROP COLUMN IF EXISTS bucket_index;
ALTER TABLE trades DROP COLUMN IF EXISTS trader_side;
