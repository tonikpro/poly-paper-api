-- Allow arbitrary outcome values (e.g. "Up", "Down") not just "YES"/"NO".

ALTER TABLE outcome_tokens DROP CONSTRAINT outcome_tokens_outcome_check;
ALTER TABLE outcome_tokens ADD CONSTRAINT outcome_tokens_outcome_check CHECK (outcome <> '');

ALTER TABLE orders DROP CONSTRAINT orders_outcome_check;
ALTER TABLE orders ADD CONSTRAINT orders_outcome_check CHECK (outcome IS NULL OR outcome <> '');

ALTER TABLE trades DROP CONSTRAINT trades_outcome_check;
ALTER TABLE trades ADD CONSTRAINT trades_outcome_check CHECK (outcome IS NULL OR outcome <> '');

ALTER TABLE positions DROP CONSTRAINT positions_outcome_check;
ALTER TABLE positions ADD CONSTRAINT positions_outcome_check CHECK (outcome <> '');
