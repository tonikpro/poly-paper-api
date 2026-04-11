CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Users
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    eth_address VARCHAR(42) UNIQUE,
    eth_private_key_encrypted BYTEA,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- API keys (mimic Polymarket's L1/L2 key model)
CREATE TABLE api_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    api_key VARCHAR(64) UNIQUE NOT NULL,
    api_secret VARCHAR(128) NOT NULL,
    passphrase VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- L1 nonce tracking (replay protection)
CREATE TABLE used_nonces (
    eth_address VARCHAR(42) NOT NULL,
    nonce BIGINT NOT NULL,
    timestamp BIGINT NOT NULL,
    used_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (eth_address, nonce)
);

-- Virtual wallets — per asset type (collateral + conditional tokens)
CREATE TABLE wallets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    asset_type VARCHAR(20) NOT NULL DEFAULT 'COLLATERAL' CHECK (asset_type IN ('COLLATERAL', 'CONDITIONAL')),
    token_id VARCHAR(255) NOT NULL DEFAULT '',
    balance NUMERIC(20, 6) NOT NULL DEFAULT 0 CHECK (balance >= 0),
    allowance NUMERIC(20, 6) NOT NULL DEFAULT 0 CHECK (allowance >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(user_id, asset_type, token_id)
);

-- Market/condition/token mapping (synced from Polymarket)
CREATE TABLE markets (
    id VARCHAR(255) PRIMARY KEY,
    condition_id VARCHAR(255) NOT NULL,
    question TEXT NOT NULL,
    slug VARCHAR(255),
    active BOOLEAN NOT NULL DEFAULT true,
    closed BOOLEAN NOT NULL DEFAULT false,
    neg_risk BOOLEAN NOT NULL DEFAULT false,
    tick_size VARCHAR(10) NOT NULL DEFAULT '0.01',
    min_order_size VARCHAR(20) NOT NULL DEFAULT '5',
    synced_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Outcome tokens — each market has 2+ tokens (YES/NO)
CREATE TABLE outcome_tokens (
    token_id VARCHAR(255) PRIMARY KEY,
    market_id VARCHAR(255) NOT NULL REFERENCES markets(id),
    outcome VARCHAR(10) NOT NULL CHECK (outcome IN ('YES', 'NO')),
    winner BOOLEAN
);

-- Orders — full signed-order fields for wire compatibility
CREATE TABLE orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    -- Wire-compatible fields (stored as strings, matching Polymarket's JSON format)
    salt VARCHAR(78) NOT NULL,
    maker VARCHAR(42) NOT NULL,
    signer VARCHAR(42) NOT NULL,
    taker VARCHAR(42) NOT NULL,
    token_id VARCHAR(255) NOT NULL,
    maker_amount VARCHAR(78) NOT NULL,
    taker_amount VARCHAR(78) NOT NULL,
    side VARCHAR(4) NOT NULL CHECK (side IN ('BUY', 'SELL')),
    expiration VARCHAR(78) NOT NULL DEFAULT '0',
    nonce VARCHAR(78) NOT NULL DEFAULT '0',
    fee_rate_bps VARCHAR(10) NOT NULL DEFAULT '0',
    signature_type INTEGER NOT NULL DEFAULT 0,
    signature TEXT NOT NULL,
    -- Derived fields for internal use
    price NUMERIC(10, 4) NOT NULL CHECK (price >= 0 AND price <= 1),
    original_size NUMERIC(20, 6) NOT NULL CHECK (original_size > 0),
    size_matched NUMERIC(20, 6) NOT NULL DEFAULT 0 CHECK (size_matched >= 0 AND size_matched <= original_size),
    status VARCHAR(20) NOT NULL DEFAULT 'LIVE' CHECK (status IN ('LIVE', 'MATCHED', 'CANCELED', 'DELAYED')),
    order_type VARCHAR(4) NOT NULL DEFAULT 'GTC' CHECK (order_type IN ('GTC', 'FOK', 'GTD', 'FAK')),
    post_only BOOLEAN NOT NULL DEFAULT false,
    owner VARCHAR(42) NOT NULL,
    market VARCHAR(255),
    asset_id VARCHAR(255) NOT NULL,
    outcome VARCHAR(10) CHECK (outcome IN ('YES', 'NO')),
    associate_trades JSONB DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(maker, salt)
);

-- Trades (fills) — wire-compatible fields
CREATE TABLE trades (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    taker_order_id UUID NOT NULL REFERENCES orders(id),
    user_id UUID NOT NULL REFERENCES users(id),
    market VARCHAR(255) NOT NULL,
    asset_id VARCHAR(255) NOT NULL,
    side VARCHAR(4) NOT NULL CHECK (side IN ('BUY', 'SELL')),
    size VARCHAR(78) NOT NULL,
    fee_rate_bps VARCHAR(10) NOT NULL DEFAULT '0',
    price VARCHAR(20) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'MATCHED' CHECK (status IN ('MATCHED', 'MINED', 'CONFIRMED', 'RETRYING')),
    match_time TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_update TIMESTAMPTZ NOT NULL DEFAULT now(),
    outcome VARCHAR(10) CHECK (outcome IS NULL OR outcome IN ('YES', 'NO')),
    owner VARCHAR(42) NOT NULL,
    maker_address VARCHAR(42),
    bucket_index INTEGER DEFAULT 0,
    transaction_hash VARCHAR(66) DEFAULT '',
    trader_side VARCHAR(10) DEFAULT 'TAKER' CHECK (trader_side IN ('TAKER', 'MAKER')),
    maker_orders JSONB DEFAULT '[]',
    fill_key VARCHAR(255) UNIQUE
);

-- Positions
CREATE TABLE positions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    token_id VARCHAR(255) NOT NULL,
    market_id VARCHAR(255) REFERENCES markets(id),
    outcome VARCHAR(10) NOT NULL CHECK (outcome IN ('YES', 'NO')),
    size NUMERIC(20, 6) NOT NULL DEFAULT 0 CHECK (size >= 0),
    avg_price NUMERIC(10, 4) NOT NULL DEFAULT 0 CHECK (avg_price >= 0),
    realized_pnl NUMERIC(20, 6) NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(user_id, token_id)
);

-- Indexes
CREATE INDEX idx_orders_user_status ON orders(user_id, status);
CREATE INDEX idx_orders_token_status ON orders(token_id, status);
CREATE INDEX idx_orders_maker_salt ON orders(maker, salt);
CREATE INDEX idx_positions_user ON positions(user_id);
CREATE INDEX idx_positions_market ON positions(market_id);
CREATE INDEX idx_trades_user ON trades(user_id);
CREATE INDEX idx_outcome_tokens_market ON outcome_tokens(market_id);
CREATE INDEX idx_wallets_user ON wallets(user_id);
CREATE INDEX idx_used_nonces_address ON used_nonces(eth_address);
