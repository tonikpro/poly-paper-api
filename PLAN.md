# Paper Trading API for Polymarket CLOB

## Context

Build a paper trading service that replicates Polymarket's CLOB (Central Limit Order Book) trading API so algo trading bots can be tested without real money. Bots use Polymarket's public APIs directly for market data (prices, orderbook, events). This service only handles:
- **Trading operations**: place/cancel orders, track positions, fill orders against real prices
- **Account management**: registration, auth, virtual balances
- **Resolution sync**: background poller that watches Polymarket for market resolutions to settle paper positions
- **Dashboard**: React SPA for account/portfolio management

**Tech stack**: Go + Chi + oapi-codegen, PostgreSQL + pgx, React (monorepo under `/dashboard`)

## Current Status (April 12, 2026)

The repo now implements the core paper-trading loop and a substantial Polymarket CLOB-compatible surface, but it does not yet fully match the latest official REST + TypeScript SDK surface.

Implemented locally:
- Dashboard auth and wallet flows
- L1/L2 CLOB auth for core trading flows
- Order placement, order queries, trade queries, balance allowance
- Cancel single, batch, all, and by market
- Compatibility stubs for notifications, scoring, heartbeat, and fee-rate

Proxied to live Polymarket CLOB:
- `GET /time`
- `GET /tick-size`
- `GET /neg-risk`
- `GET /book`, `POST /books`
- `GET /midpoint`, `POST /midpoints`
- `GET /price`, `POST /prices`
- `GET /spread`, `POST /spreads`
- `GET /last-trade-price`, `POST /last-trades-prices`
- `GET /sampling-simplified-markets`
- `GET /sampling-markets`
- `GET /simplified-markets`
- `GET /markets`
- `GET /markets/{marketId}`
- `GET /live-activity/events/{eventId}`

Known parity gaps against the latest official docs and `Polymarket/clob-client`:
- Missing official endpoints: `GET /prices-history`, readonly API key endpoints, builder API key endpoints, closed-only / ban-status auth helpers
- Notifications and scoring endpoints are stubs and should not be treated as full parity
- `/data/trades` is kept for SDK compatibility; official docs also expose newer `/trades`-style ledger wording in some places

Primary local reference for future CLOB work:
- `docs/polymarket-clob-api.md`

---

## Phase 1: Project Structure & Database Foundation

### Step 1.1 — Project layout & dependencies

Create the following directory structure:

```
poly/
├── cmd/
│   └── server/
│       └── main.go              # entrypoint
├── api/
│   ├── openapi/
│   │   ├── dashboard.yaml       # OpenAPI 3.0 spec for dashboard API (/auth/*, /api/*)
│   │   └── clob.yaml            # OpenAPI 3.0 spec for CLOB API (/clob/*)
│   └── generated/
│       ├── dashboard/           # oapi-codegen output (types, server interface, spec)
│       └── clob/                # oapi-codegen output (types, server interface, spec)
├── internal/
│   ├── config/
│   │   └── config.go            # env-based config (DB, port, Polymarket URLs, JWT secret)
│   ├── database/
│   │   ├── postgres.go          # pgx pool setup
│   │   └── migrations/          # SQL migration files
│   ├── auth/
│   │   ├── handler.go           # implements generated dashboard server interface
│   │   ├── middleware.go        # JWT + L1/L2 middleware
│   │   ├── service.go           # auth business logic
│   │   └── repository.go       # user DB queries
│   ├── trading/
│   │   ├── handler.go           # implements generated CLOB server interface
│   │   ├── service.go           # order logic, matching
│   │   ├── repository.go       # orders/positions/trades DB queries
│   │   └── matcher.go          # price checking against Polymarket
│   ├── wallet/
│   │   ├── handler.go           # implements wallet parts of dashboard interface
│   │   ├── service.go           # balance logic
│   │   └── repository.go       # balance DB queries
│   ├── proxy/
│   │   └── proxy.go            # reverse proxy to real Polymarket CLOB for market data endpoints
│   ├── sync/
│   │   ├── poller.go            # background resolution poller
│   │   └── resolver.go         # settle positions on resolution
│   └── models/
│       └── models.go            # shared domain types
├── dashboard/                   # React SPA (Phase 5)
├── go.mod
├── go.sum
├── Makefile
└── docker-compose.yml           # PostgreSQL for local dev
```

Install dependencies:
- `github.com/go-chi/chi/v5` — router
- `github.com/jackc/pgx/v5` — PostgreSQL driver
- `github.com/golang-jwt/jwt/v5` — JWT auth for dashboard
- `github.com/ethereum/go-ethereum` — EIP-712 signature verification for L1 CLOB auth
- `golang.org/x/crypto` — bcrypt for passwords
- `github.com/kelseyhightower/envconfig` — config from env vars

Dev tools (code generation):
- `github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen` — generates Go types, server interfaces, and spec embedding from OpenAPI 3.0 YAML

### Step 1.2 — OpenAPI specs

Write two OpenAPI 3.0 specs:

**`api/openapi/dashboard.yaml`** — Dashboard API:
- `/auth/register`, `/auth/login` (public)
- `/api/wallet`, `/api/orders`, `/api/positions`, `/api/trades`, `/api/eth-address` (JWT-protected)

**`api/openapi/clob.yaml`** — CLOB API (targets Polymarket compatibility for the implemented subset):

All paths are relative to the CLOB base URL. Bots set `host = "http://localhost:8080/clob"` and the SDK appends paths directly. The Chi router mounts the CLOB handler group under `/clob`.

Auth & system (public/L1):
- `GET /` — health check (public)
- `GET /time` — server time (public)
- `POST /auth/api-key` (L1), `GET /auth/derive-api-key` (L1)

Auth & system (L2):
- `GET /auth/api-keys` (L2), `DELETE /auth/api-key` (L2)

Trading (L2):
- `POST /order` — place single order
- `POST /orders` — place batch orders
- `DELETE /order` — cancel single order (body: `{"orderID": "..."}`)
- `DELETE /orders` — cancel batch orders (body: `["id1", "id2"]`)
- `DELETE /cancel-all` — cancel all
- `DELETE /cancel-market-orders` — cancel all for a market

Query (L2):
- `GET /data/order/{id}` — get order by ID
- `GET /data/orders` — get user's orders (cursor-paginated)
- `GET /data/trades` — get user's trades (cursor-paginated)
- `GET /balance-allowance` — get balance + allowance per asset
- `GET /balance-allowance/update` — update/refresh balance allowance
- `GET /notifications` — get notifications
- `DELETE /notifications` — drop notifications
- `GET /order-scoring` — check if order is scoring
- `POST /orders-scoring` — check if orders are scoring
- `POST /v1/heartbeats` — bot heartbeat

Market data (public, no auth — proxied to real Polymarket CLOB):
- `GET /tick-size` — tick size for a token
- `GET /neg-risk` — neg-risk flag for a token
- `GET /fee-rate` — fee rate in bps
- `GET /book` — order book for a token
- `POST /books` — order books for multiple tokens
- `GET /midpoint` — midpoint price for a token
- `POST /midpoints` — midpoints for multiple tokens
- `GET /price` — price for a token + side
- `POST /prices` — prices for multiple tokens
- `GET /spread` — spread for a token
- `POST /spreads` — spreads for multiple tokens
- `GET /last-trade-price` — last trade price for a token
- `POST /last-trades-prices` — last trade prices for multiple tokens
- `GET /sampling-simplified-markets` — simplified market list
- `GET /sampling-markets` — market list
- `GET /simplified-markets` — simplified market list (full)
- `GET /markets` — all markets
- `GET /markets/{id}` — single market
- `GET /live-activity/events/{id}` — market trade events

All public/market-data endpoints proxy to `https://clob.polymarket.com` — forward the request, return the response as-is. This lets bots use a single CLOB client with one base URL.

Request/response schemas in `clob.yaml` should stay as close as possible to Polymarket's JSON format, but the latest official API surface is larger than the currently implemented subset. Keep `docs/polymarket-clob-api.md` in sync whenever parity changes.

### Step 1.3 — Code generation

Run `oapi-codegen` to generate from each spec:
- **Types** (`types.gen.go`) — request/response structs
- **Server interface** (`server.gen.go`) — Chi-compatible handler interface
- **Spec** (`spec.gen.go`) — embedded OpenAPI spec for validation

Config files (`oapi-codegen.yaml`) in each output directory. `Makefile` target: `make generate`.

Handler files in `internal/` implement the generated interfaces.

### Step 1.4 — Database schema (migrations)

**`001_init.sql`**:

```sql
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
    token_id VARCHAR(255) NOT NULL DEFAULT '',              -- empty for COLLATERAL, token_id for CONDITIONAL
    balance NUMERIC(20, 6) NOT NULL DEFAULT 0 CHECK (balance >= 0),
    allowance NUMERIC(20, 6) NOT NULL DEFAULT 0 CHECK (allowance >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(user_id, asset_type, token_id)
);

-- Market/condition/token mapping (synced from Polymarket)
CREATE TABLE markets (
    id VARCHAR(255) PRIMARY KEY,             -- Polymarket market ID
    condition_id VARCHAR(255) NOT NULL,      -- CTF condition ID
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
    token_id VARCHAR(255) PRIMARY KEY,       -- the asset_id bots trade with
    market_id VARCHAR(255) NOT NULL REFERENCES markets(id),
    outcome VARCHAR(10) NOT NULL CHECK (outcome IN ('YES', 'NO')),
    winner BOOLEAN                           -- NULL = unresolved, true = winning token, false = losing
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
    owner VARCHAR(42) NOT NULL,              -- owner address
    market VARCHAR(255),                     -- condition_id / market slug
    asset_id VARCHAR(255) NOT NULL,          -- same as token_id
    outcome VARCHAR(10) CHECK (outcome IN ('YES', 'NO')),
    associate_trades JSONB DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Idempotency: prevent duplicate order submission
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
    transaction_hash VARCHAR(66) DEFAULT '',  -- empty for paper trades
    trader_side VARCHAR(10) DEFAULT 'TAKER' CHECK (trader_side IN ('TAKER', 'MAKER')),
    maker_orders JSONB DEFAULT '[]',
    -- Idempotent fill key: prevent double-fills under concurrency
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

CREATE INDEX idx_orders_user_status ON orders(user_id, status);
CREATE INDEX idx_orders_token_status ON orders(token_id, status);
CREATE INDEX idx_orders_maker_salt ON orders(maker, salt);
CREATE INDEX idx_positions_user ON positions(user_id);
CREATE INDEX idx_positions_market ON positions(market_id);
CREATE INDEX idx_trades_user ON trades(user_id);
CREATE INDEX idx_outcome_tokens_market ON outcome_tokens(market_id);
CREATE INDEX idx_wallets_user ON wallets(user_id);
CREATE INDEX idx_used_nonces_address ON used_nonces(eth_address);
```

### Step 1.5 — Config, DB connection, docker-compose

- `docker-compose.yml` with PostgreSQL 16
- `config.go`: load from env (DATABASE_URL, PORT, JWT_SECRET, POLYMARKET_API_URL)
- `postgres.go`: pgx pool initialization with context

---

## Phase 2: Authentication & API Keys

Two completely separate auth systems with separate route groups and middleware.

### Step 2.1 — Dashboard auth (JWT) — `/auth/*` + `/api/*`

**Public routes (no auth):**
- `POST /auth/register` — email + password → creates user (generates an Ethereum keypair, stores address + encrypted private key), creates wallet (default $1000), returns JWT
- `POST /auth/login` — returns JWT token

**Protected dashboard routes (JWT middleware):**
- `GET /api/wallet` — view balance
- `POST /api/wallet/deposit` — add virtual funds
- `POST /api/wallet/withdraw` — remove virtual funds
- `GET /api/orders` — view orders (with filters)
- `GET /api/positions` — view positions
- `GET /api/trades` — view trade history
- `GET /api/eth-address` — show user's Ethereum address + private key (so they can configure their bot)

JWT middleware: checks `Authorization: Bearer <token>` header, extracts user ID from claims.

### Step 2.2 — CLOB auth (Polymarket-compatible) — `/clob/*`

Core Polymarket-compatible auth with two levels for the main bot workflow. The latest official auth surface also includes readonly, builder, and ban-status endpoints that are not yet implemented here.

**L1 auth — EIP-712 wallet signatures (for API key creation):**

Headers (same as Polymarket):
- `POLY_ADDRESS` — Ethereum address (0x...)
- `POLY_SIGNATURE` — EIP-712 signature
- `POLY_TIMESTAMP` — UNIX timestamp
- `POLY_NONCE` — nonce (default: 0)

EIP-712 domain:
```
name: "ClobAuthDomain"
version: "1"
chainId: 137
```

EIP-712 message type `ClobAuth`:
```
address (address) — signer wallet
timestamp (string) — UNIX timestamp
nonce (uint256) — nonce value
message (string) — "This message attests that I control the given wallet"
```

Server verifies:
1. Recover signer from EIP-712 signature, check it matches `POLY_ADDRESS`
2. Look up user by `eth_address`
3. **Replay protection**: reject if `(address, nonce)` pair already exists in `used_nonces` table
4. **Timestamp window**: reject if `POLY_TIMESTAMP` is outside ±300 seconds of server time (symmetric window tolerates clock skew). Log drift > 30s for monitoring.
5. On success: insert into `used_nonces`

**L2 auth — HMAC-SHA256 (for all trading operations):**

Headers (same as Polymarket):
- `POLY_ADDRESS` — Ethereum address
- `POLY_SIGNATURE` — HMAC-SHA256 signature (base64)
- `POLY_TIMESTAMP` — UNIX timestamp
- `POLY_API_KEY` — the API key
- `POLY_PASSPHRASE` — the passphrase

HMAC signature canonical string and encoding:
```
method   = uppercase HTTP method (e.g. "GET", "POST", "DELETE")
path     = request path including query string, as-sent (e.g. "/order?id=abc")
           query params are NOT re-sorted — use the exact string from the request
body     = minified JSON (separators ",", ":", no trailing whitespace)
           single quotes replaced with double quotes (Python SDK compat)
           empty string if no body (GET/DELETE without body)
timestamp = POLY_TIMESTAMP header value as string

message  = "{timestamp}{method}{path}{body}"
signature = url_safe_base64(hmac_sha256(base64_decode(secret), message))
```
URL-safe base64: standard base64 with `+`→`-`, `/`→`_`, padding `=` kept.

Server verifies:
1. Look up API key by `POLY_API_KEY`
2. **Address binding**: verify the API key belongs to `POLY_ADDRESS` (api_keys → user → eth_address must match)
3. **Passphrase binding**: verify `POLY_PASSPHRASE` matches the stored passphrase for this key
4. Decode stored secret, recompute HMAC, compare with `POLY_SIGNATURE`
5. **Timestamp window**: reject if `POLY_TIMESTAMP` is outside ±300 seconds of server time. Log drift > 30s.

**Order-level binding** (enforced at POST `/clob/order`):
- `order.maker` or `order.signer` must equal the authenticated `POLY_ADDRESS`
- `owner` field must equal the authenticated `POLY_ADDRESS`
- Reject with 401 if mismatched — prevents submitting orders on behalf of other users

**API key endpoints (current implementation):**

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | `/clob/auth/api-key` | L1 | Create new API key → returns `{apiKey, secret, passphrase}` |
| GET | `/clob/auth/derive-api-key` | L1 | Derive (retrieve) existing API key |
| GET | `/clob/auth/api-keys` | L2 | List API keys for the user as structured key objects |
| DELETE | `/clob/auth/api-key` | L2 | Delete an API key |

Response format matches Polymarket's `ApiCreds`:
```json
{
  "apiKey": "...",
  "secret": "...",
  "passphrase": "..."
}
```

**Bot workflow (compatible with the main real-Polymarket flow):**
1. User registers on dashboard → gets Ethereum address + private key
2. Bot configures private key + base URL `localhost:8080/clob`
3. Bot calls `create_or_derive_api_creds()` → L1 auth → gets API key/secret/passphrase
4. Bot trades using L2 headers — current goal is SDK compatibility for core trading, not full parity with every newer auth surface

---

## Phase 3: Core Trading API (CLOB-Compatible)

All CLOB endpoints live under `/clob/*` with L2 auth middleware. The core paths mirror Polymarket closely enough for the main bot flow, but some newer endpoints and some response envelopes still differ from the latest official API.

### Step 3.1 — Order endpoints

All paths below are relative to the `/clob` mount point (e.g. `/clob/order`). The SDK sends requests to `{host}/order` where `host = "http://localhost:8080/clob"`.

| Method | Path | Body | Description |
|--------|------|------|-------------|
| POST | `/order` | signed order JSON | Place single order |
| POST | `/orders` | array of signed orders | Place batch orders (up to 15) |
| DELETE | `/order` | `{"orderID": "uuid"}` | Cancel single order (ID in body, NOT path) |
| DELETE | `/orders` | `["id1", "id2"]` | Cancel batch orders (array in body) |
| DELETE | `/cancel-all` | none | Cancel all user's orders |
| DELETE | `/cancel-market-orders` | `{"market": "...", "asset_id": "..."}` | Supports market-only, asset-only, both, or no filters |
| GET | `/data/order/{id}` | — | Get order by ID |
| GET | `/data/orders` | — | Get user's orders (cursor-paginated) |
| GET | `/data/trades` | — | Get user's trades (cursor-paginated) |

Additional official CLOB surface not yet implemented locally:
- `GET /prices-history`
- readonly API key endpoints
- builder API key endpoints
- closed-only / ban-status helpers

### Step 3.2 — Wire-compatible order format

**POST `/clob/order` request body** (must match Polymarket exactly):
```json
{
  "order": {
    "salt": "12345",
    "maker": "0x...",
    "signer": "0x...",
    "taker": "0x...",
    "tokenId": "token_id_here",
    "makerAmount": "50000000",
    "takerAmount": "100000000",
    "expiration": "0",
    "nonce": "0",
    "feeRateBps": "0",
    "side": "BUY",
    "signatureType": 0,
    "signature": "0x..."
  },
  "owner": "0x...",
  "orderType": "GTC",
  "postOnly": false
}
```

**POST `/clob/order` response** (must match Polymarket exactly):
```json
{
  "success": true,
  "errorMsg": "",
  "orderID": "uuid",
  "transactionsHashes": [],
  "status": "LIVE",
  "takingAmount": "100000000",
  "makingAmount": "50000000"
}
```

**GET `/clob/data/orders` response** (cursor-paginated):
```json
{
  "next_cursor": "cursor_string",
  "data": [{
    "id": "uuid",
    "status": "LIVE",
    "owner": "0x...",
    "maker_address": "0x...",
    "market": "condition_id",
    "asset_id": "token_id",
    "side": "BUY",
    "original_size": "100",
    "size_matched": "0",
    "price": "0.50",
    "associate_trades": [],
    "outcome": "YES",
    "created_at": "2025-01-01T00:00:00Z",
    "expiration": "0",
    "order_type": "GTC"
  }]
}
```

Server validates the signed order fields but does NOT verify the EIP-712 order signature on-chain (paper trading — no real settlement). Store all fields to return them in GET responses.

### Step 3.3 — Parity follow-up backlog

To move from "core SDK-compatible" to "latest API-compatible", prioritize:
- Add missing official endpoints: `GET /prices-history`, readonly API keys, builder API keys, closed-only / ban-status
- Replace notification/scoring stubs with real behavior or explicitly document them as non-parity endpoints in the OpenAPI spec

### Step 3.4 — Order matching against real prices

`matcher.go` — when an order is placed:

1. Fetch current best bid/ask from Polymarket's public API: `GET https://clob.polymarket.com/book?token_id={id}`
2. **BUY order**: if order price >= best ask on Polymarket, fill at the ask price
3. **SELL order**: if order price <= best bid on Polymarket, fill at the bid price
4. **Partial fills**: if order size > available liquidity at that level, fill what's available, rest stays open
5. **MARKET orders** (FOK/FAK): fill immediately at best available price or reject
6. For open LIMIT orders that didn't fill immediately: background worker checks periodically

**Concurrency safety — all fills run inside a single DB transaction:**
```sql
BEGIN;
  SELECT * FROM orders WHERE id = $1 AND status = 'LIVE' FOR UPDATE;
  -- Check remaining size, compute fill
  INSERT INTO trades (...) VALUES (...) ON CONFLICT (fill_key) DO NOTHING;  -- idempotent
  UPDATE orders SET size_matched = ..., status = ... WHERE id = $1;
  UPDATE positions SET ... WHERE user_id = $1 AND token_id = $2;
  UPDATE wallets SET balance = balance - $amount WHERE ... AND balance >= $amount;  -- balance check
COMMIT;
```

Key guarantees:
- `SELECT ... FOR UPDATE` prevents concurrent fills on same order
- `fill_key` unique constraint prevents double-fills
- Balance check in UPDATE prevents negative balances
- Single transaction = all-or-nothing

### Step 3.5 — Position & trade endpoints

**GET `/clob/data/trades` response** (cursor-paginated, matches Polymarket):
```json
{
  "next_cursor": "cursor_string",
  "data": [{
    "id": "uuid",
    "taker_order_id": "uuid",
    "market": "condition_id",
    "asset_id": "token_id",
    "side": "BUY",
    "size": "100",
    "fee_rate_bps": "0",
    "price": "0.50",
    "status": "MATCHED",
    "match_time": "2025-01-01T00:00:00Z",
    "last_update": "2025-01-01T00:00:00Z",
    "outcome": "YES",
    "owner": "0x...",
    "maker_address": "0x...",
    "bucket_index": 0,
    "transaction_hash": "",
    "trader_side": "TAKER",
    "maker_orders": []
  }]
}
```

**GET `/balance-allowance` response** (matches Polymarket's `BalanceAllowanceResponse`):

Query params: `asset_type` (COLLATERAL or CONDITIONAL), `token_id` (for conditional), `signature_type`
```json
{
  "balance": "1000.000000",
  "allowance": "1000.000000"
}
```

For paper trading, `allowance` always equals `balance` (no on-chain approval needed). The `wallets` table stores per-asset balances: one row for COLLATERAL (USDC equivalent), one row per CONDITIONAL token the user holds.

Other L2 endpoints:

| Method | Path | Description |
|--------|------|-------------|
| GET | `/balance-allowance` | Get balance + allowance per asset type |
| GET | `/balance-allowance/update` | Refresh balance allowance |
| GET | `/notifications` | Get user notifications |
| DELETE | `/notifications` | Drop notifications |
| GET | `/order-scoring` | Check if order is scoring |
| POST | `/orders-scoring` | Check if orders are scoring |
| POST | `/v1/heartbeats` | Bot heartbeat keep-alive |

Public endpoints (no auth, handled locally):

| Method | Path | Description |
|--------|------|-------------|
| GET | `/` | Health check |
| GET | `/time` | Server UNIX timestamp |

Public endpoints (no auth, proxied to `https://clob.polymarket.com`):

| Method | Path | Description |
|--------|------|-------------|
| GET | `/tick-size` | Tick size for a token |
| GET | `/neg-risk` | Neg-risk flag for a token |
| GET | `/fee-rate` | Fee rate in bps |
| GET | `/book` | Order book for a token |
| POST | `/books` | Order books for multiple tokens |
| GET | `/midpoint` | Midpoint price |
| POST | `/midpoints` | Midpoints for multiple tokens |
| GET | `/price` | Price for a token + side |
| POST | `/prices` | Prices for multiple tokens |
| GET | `/spread` | Spread for a token |
| POST | `/spreads` | Spreads for multiple tokens |
| GET | `/last-trade-price` | Last trade price |
| POST | `/last-trades-prices` | Last trade prices for multiple tokens |
| GET | `/sampling-simplified-markets` | Simplified market list |
| GET | `/sampling-markets` | Market list |
| GET | `/simplified-markets` | Simplified market list (full) |
| GET | `/markets` | All markets |
| GET | `/markets/{id}` | Single market by ID |
| GET | `/live-activity/events/{id}` | Market trade events |

Proxy implementation: a single reverse-proxy handler forwards the request to `https://clob.polymarket.com` with the same path, query string, and headers (excluding POLY_* auth headers). Returns the upstream response body and status code as-is. Caching optional (short TTL for book/price, longer for markets).

### Step 3.6 — Order lifecycle background worker

A goroutine that periodically (every 5-10s):
1. `SELECT ... FOR UPDATE SKIP LOCKED` all LIVE limit orders (avoids blocking request-path fills)
2. Groups by token_id to batch Polymarket API calls
3. For each token, fetches current orderbook from Polymarket
4. Fills orders that now match (using same transactional fill logic as Step 3.3)
5. Respects FOK/FAK/GTD semantics: expire GTD orders past expiration, reject partial FOK

---

## Phase 4: Market Sync & Resolution

### Step 4.1 — Market/token sync poller

`poller.go` — background goroutine running every 60 seconds:

1. Query distinct `token_id` values from open orders and positions
2. For each token, look up via Gamma API: `GET https://gamma-api.polymarket.com/markets?clob_token_ids={id}`
3. Upsert into `markets` table (condition_id, question, slug, active, closed, neg_risk, tick_size)
4. Upsert into `outcome_tokens` table (token_id → market_id, outcome YES/NO)
5. If market is resolved (closed=true, has winning outcome) → set `outcome_tokens.winner = true/false`

This ensures we always have the correct **market → condition → outcome token** mapping, not just a flat token_id.

### Step 4.2 — Position settlement

`resolver.go` — triggered when poller detects a newly resolved market.

**Two settlement paths depending on market type:**

#### Universal payout rule (both binary and neg-risk)

Every token in `outcome_tokens` has a `winner` boolean set at resolution time.

**One rule for all tokens, all market types:**
```
payout_per_share = winner ? 1.0 : 0.0
```

That's it. The `winner` flag encodes everything — no need for separate binary vs. multi-outcome logic at settlement time.

#### How `winner` is set (differs by market type)

**Binary markets (`neg_risk = false`):**
Two tokens (YES + NO). If the market resolves YES → YES token gets `winner=true`, NO token gets `winner=false`. Vice versa if resolves NO.

**Neg-risk markets (`neg_risk = true`):**
Multi-outcome market (e.g., "Who wins?" with candidates A, B, C, Other). Each sub-outcome has its own YES/NO token pair, but only **one sub-outcome resolves true**.

```
markets (id=M1, neg_risk=true)
  └── outcome_tokens: [
        (token_YES_A, outcome="YES"),  -- YES on candidate A
        (token_NO_A,  outcome="NO"),   -- NO on candidate A
        (token_YES_B, outcome="YES"),  -- YES on candidate B
        (token_NO_B,  outcome="NO"),   -- NO on candidate B
        ...
      ]
```

When candidate A wins:
- `token_YES_A` → `winner=true` (pays $1)
- `token_NO_A` → `winner=false` (pays $0)
- `token_YES_B` → `winner=false` (pays $0)
- `token_NO_B` → `winner=true` (pays $1)
- ...same pattern for all other sub-outcomes

This is correct because holding NO on a loser is economically equivalent to holding YES on the winner (NegRiskAdapter conversion). We don't implement the on-chain conversion — just apply the universal payout rule.

If no named outcome wins, the "Other" sub-outcome resolves true.

#### Settlement transaction (both market types):

```sql
BEGIN;
  -- Lock all positions for tokens in this market
  SELECT * FROM positions p
    JOIN outcome_tokens ot ON p.token_id = ot.token_id
    WHERE ot.market_id = $market_id
    FOR UPDATE;

  -- For each position:
  --   payout_per_share = (ot.winner = true) ? 1.0 : 0.0
  --   total_payout = position.size * payout_per_share
  --   realized_pnl = total_payout - (position.size * position.avg_price)

  -- Credit COLLATERAL wallet
  UPDATE wallets SET balance = balance + $total_payout
    WHERE user_id = $uid AND asset_type = 'COLLATERAL' AND token_id = '';

  -- Zero out CONDITIONAL wallet for this token
  UPDATE wallets SET balance = 0
    WHERE user_id = $uid AND asset_type = 'CONDITIONAL' AND token_id = $token_id;

  -- Close position
  UPDATE positions SET size = 0, realized_pnl = realized_pnl + $rpnl
    WHERE user_id = $uid AND token_id = $token_id;

  -- Cancel remaining LIVE orders for all tokens in this market
  UPDATE orders SET status = 'CANCELED'
    WHERE token_id = ANY($market_token_ids) AND status = 'LIVE';
COMMIT;
```

Reference: [Polymarket Neg-Risk Adapter](https://github.com/Polymarket/neg-risk-ctf-adapter), [Neg-Risk Docs](https://docs.polymarket.com/advanced/neg-risk)

---

## Phase 5: React Dashboard

### Step 5.1 — React project setup

```
dashboard/
├── src/
│   ├── components/
│   │   ├── Layout.tsx
│   │   ├── Navbar.tsx
│   │   └── ProtectedRoute.tsx
│   ├── pages/
│   │   ├── Login.tsx
│   │   ├── Register.tsx
│   │   ├── Dashboard.tsx
│   │   ├── Orders.tsx
│   │   ├── Positions.tsx
│   │   ├── Trades.tsx
│   │   ├── Wallet.tsx
│   │   └── ApiKeys.tsx
│   ├── api/
│   │   └── client.ts           # axios/fetch wrapper
│   ├── context/
│   │   └── AuthContext.tsx
│   ├── App.tsx
│   └── main.tsx
├── package.json
├── tsconfig.json
└── vite.config.ts
```

Tech: React + TypeScript + Vite + React Router + Tailwind CSS

### Step 5.2 — Pages

- **Login/Register**: email + password forms
- **Dashboard**: overview — balance, total PnL, active orders count, open positions count
- **Orders**: table of orders with status, cancel button
- **Positions**: table of current positions with unrealized PnL (fetched from Polymarket current price)
- **Trades**: history table with pagination
- **Wallet**: current balance, deposit/withdraw virtual funds
- **API Keys**: generate/revoke API keys, show key/secret/passphrase once on creation

### Step 5.3 — Serving

The dashboard is served separately from the Go API server. In dev, Vite proxies `/auth` and `/api` requests to the Go backend. In production, serve the `dashboard/dist` output with any static file server (nginx, caddy, etc.) and proxy API routes to the Go backend.

---

## Phase 6: Testing & SDK Contract Validation

### Step 6.1 — Integration tests (Go)

- Test order placement, matching, cancellation flows
- Test position settlement on resolution
- Test auth flows (L1 EIP-712 + L2 HMAC + JWT)
- Test concurrent fill safety (parallel requests on same order)
- Test replay protection (reused nonce/timestamp rejection)
- Test balance-allowance returns correct per-asset balances

### Step 6.2 — SDK contract tests (py-clob-client)

Create a `tests/sdk_compat/` directory with Python scripts that use the **real `py-clob-client`** SDK against our server. This is the ultimate compatibility validation.

```python
# test_sdk_compat.py
from py_clob_client.client import ClobClient

# Point at paper trading server
client = ClobClient("http://localhost:8080/clob", key=PRIVATE_KEY)

# L1: create API credentials (must work with zero SDK changes)
creds = client.create_or_derive_api_creds()
client.set_api_creds(creds)

# L2: get API keys
keys = client.get_api_keys()

# Trading
order = client.create_and_post_order(OrderArgs(...))
orders = client.get_orders()
trades = client.get_trades()
balance = client.get_balance_allowance(params)

# Cancel
client.cancel_all()
```

If any SDK call fails with a deserialization or auth error, our wire format is wrong — fix it.

### Step 6.3 — Rate limiting & logging

- Add structured logging (slog)
- Add basic rate limiting middleware

### Step 6.4 — Makefile targets

- `make generate` — run oapi-codegen for both specs
- `make run` — start server
- `make migrate` — run DB migrations
- `make dev-dashboard` — start React dev server
- `make build` — build Go binary with embedded frontend
- `make docker-up` — start PostgreSQL
- `make test` — run Go integration tests
- `make test-sdk` — run py-clob-client compatibility tests

---

## Execution Order for Claude Code

Each phase should be implemented and tested before moving to the next:

1. **Phase 1** → verify: `docker-compose up`, `make generate` produces code, migrations run, server starts
2. **Phase 2** → verify: register user, login, get JWT, create API key via L1 EIP-712, access protected route with L2 HMAC
3. **Phase 3** → verify: place order via py-clob-client SDK against localhost, see it match against real Polymarket prices, check position/trade created, verify balance-allowance response format
4. **Phase 4** → verify: poller syncs market/token mapping, detects resolved market, positions settle correctly per outcome token, balance updates
5. **Phase 5** → verify: React dashboard shows orders/positions/trades, auth works end-to-end
6. **Phase 6** → verify: `make test` passes, `make test-sdk` passes with real py-clob-client

---

## Verification

After each phase, run:
```bash
# Start infra
docker-compose up -d

# Run migrations
make migrate

# Start server
make run

# Test with curl
curl -X POST localhost:8080/auth/register -d '{"email":"test@test.com","password":"test1234"}'
curl -X POST localhost:8080/auth/login -d '{"email":"test@test.com","password":"test1234"}'
# Use returned JWT to get eth private key, then use py-clob-client SDK:
# client = ClobClient("http://localhost:8080/clob", key=PRIVATE_KEY)
# creds = client.create_or_derive_api_creds()
```

## Sources

- [Polymarket CLOB Introduction](https://docs.polymarket.com/developers/CLOB/introduction)
- [Polymarket API Endpoints](https://docs.polymarket.com/quickstart/reference/endpoints)
- [Polymarket Public Methods](https://docs.polymarket.com/developers/CLOB/clients/methods-public)
- [Polymarket L2 Methods](https://docs.polymarket.com/developers/CLOB/clients/methods-l2)
- [Polymarket Orders](https://docs.polymarket.com/developers/CLOB/orders/orders)
- [Polymarket Cancel Orders](https://docs.polymarket.com/developers/CLOB/orders/cancel-orders)
- [Polymarket Trades](https://docs.polymarket.com/developers/CLOB/trades/trades-overview)
- [Polymarket Authentication](https://docs.polymarket.com/api-reference/authentication)
- [Polymarket Gamma API](https://gamma-api.polymarket.com/)
