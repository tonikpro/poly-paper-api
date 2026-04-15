# Architecture

## Overview

Paper trading service mimicking Polymarket's CLOB API. All market data is proxied live; only trading, auth, and settlement are local.

## Request Routing

```
Client
  ├── GET  /health                 → inline handler (main.go)
  ├── POST /auth/register          → DashboardHandler (auth pkg)
  ├── POST /auth/login             → DashboardHandler (auth pkg)
  ├── GET  /api/*                  → DashboardHandler  ← JWT middleware
  ├── GET  /api/stats              → DashboardHandler.GetStats (JWT)
  ├── *    /api/api-keys           → DashboardHandler (JWT)
  └── *    /clob/*                 → CLOBServer  ← CLOBAuthMiddleware
                ├── /clob/auth/*   → CLOBAuthHandler  ← L1 (EIP-712)
                ├── /clob/order    → CLOBTradingHandler  ← L2 (HMAC)
                ├── /clob/orders   → CLOBTradingHandler  ← L2
                ├── /clob/data/*   → CLOBTradingHandler  ← L2
                └── /clob/book     → ProxyHandler (no auth)
                    /clob/markets  → ProxyHandler (no auth)
                    /clob/...      → ProxyHandler (no auth)
```

**Rate limiting**: 100 req/min per IP (`internal/middleware.RateLimiter`).

## Package Map

### `cmd/server/main.go`
Entry point. Wires all services/handlers, mounts routers, starts background workers, handles graceful shutdown.

### `internal/config`
`envconfig`-based struct loaded once at startup. All env vars have sensible defaults for local dev.

### `internal/database`
- `postgres.go` — creates `pgxpool.Pool`
- `migrate.go` — runs embedded SQL migrations at startup via `go:embed`
- `migrations/` — sequential SQL files (`001_init.sql`, `002_use_id_as_api_key.sql`, `003_relax_outcome_check.sql`)

### `internal/models`
Shared domain types (no logic). Two families:
- **Wire types** — match Polymarket JSON exactly (`SignedOrder`, `PostOrderRequest`, `OpenOrder`, `Trade`)
- **DB types** — internal representations (`Order`, `Position`, `Wallet`)

### `internal/auth`
| File | Responsibility |
|---|---|
| `service.go` | Registration/login (bcrypt), JWT issue/validate, L1 EIP-712 verify, L2 HMAC-SHA256 verify, API key CRUD, Ethereum key generation |
| `repository.go` | User and API key DB queries |
| `middleware.go` | `JWTMiddleware`, `L1Middleware`, `L2Middleware` — inject `user_id`/`eth_address` into context |
| `clob_middleware.go` | `CLOBAuthMiddleware` — routes to L1 or L2 per path, or skips auth for public endpoints |
| `handler.go` | Dashboard HTTP handlers (register, login, deposit, withdraw, positions) |
| `clob_handler.go` | CLOB auth HTTP handlers (create/derive/get/delete API key) |
| `dashboard_queries.go` | Dashboard-specific DB queries (wallet balance, deposit, stats) |

Key implementation details:
- Ethereum wallet is auto-generated on registration; private key stored AES-256 encrypted (`ENCRYPTION_KEY`)
- L1 nonce replay protection via `used_nonces` table (±300s timestamp window)
- L2 HMAC: `HMAC-SHA256(secret, timestamp + method + path + body)` — path is WITHOUT `/clob` prefix

### `internal/trading`
| File | Responsibility |
|---|---|
| `service.go` | Order placement, cancellation (single/batch/all/by-market), queries (orders/trades/positions), balance/allowance, token resolution from Gamma API |
| `repository.go` | All orders/trades/positions/wallets DB queries |
| `handler.go` | CLOB HTTP handlers for trading endpoints |
| `proxy.go` | `ProxyHandler` — strips `/clob` prefix, forwards to `POLYMARKET_CLOB_URL` |

**Order fill flow** (instant fill — no orderbook matching):
1. `PlaceOrder` — validate, derive price/size from maker/taker amounts, resolve token from Gamma if unknown
2. Single atomic transaction: debit wallet → insert order (status=MATCHED) → insert trade → credit wallet → upsert position
3. Returns `OrderResponse{status: "MATCHED"}` synchronously — no background workers, no LIVE order state

### `internal/sync`
| File | Responsibility |
|---|---|
| `poller.go` | Background goroutine (60s interval + immediate first run). Fetches all token IDs with LIVE orders or open positions, queries Gamma API in batches of 10, upserts markets and outcome tokens |
| `resolver.go` | `SettleMarket` — on market close, pays out winner (1.0/share) and zeroes loser positions in a single DB transaction |

### `internal/proxy`
Thin package — `internal/trading/proxy.go` (`ProxyHandler`) handles the actual proxying within the trading package.

### `internal/middleware`
`RateLimiter` — per-IP token bucket (100 req/min, cleanup goroutine every 60s).

### `internal/server`
`CLOBServer` composes `CLOBAuthHandler`, `CLOBTradingHandler`, `ProxyHandler` into a single struct implementing the generated `clob.ServerInterface`. `clob.Unimplemented` is embedded to satisfy unimplemented endpoints with stub 501 responses.

## Database Schema

```
users              — email/password, generated eth wallet (private key AES-encrypted)
api_keys           — L2 keys (id=api_key, secret, passphrase), belong to user
used_nonces        — replay protection for L1 (eth_address + nonce PK)
wallets            — virtual balances; COLLATERAL (token_id='') or CONDITIONAL (per token)
markets            — Polymarket markets synced from Gamma (condition_id, active/closed, tick_size)
outcome_tokens     — YES/NO tokens per market, winner flag set on resolution
orders             — full wire-compatible order fields + derived price/size/status
trades             — fill records linked to orders
positions          — per-user per-token; tracks size, avg_price, realized_pnl
```

Key constraints:
- `orders.UNIQUE(maker, salt)` — deduplication
- `wallets.UNIQUE(user_id, asset_type, token_id)`
- `positions.UNIQUE(user_id, token_id)`

## Background Workers

| Worker | Interval | What it does |
|---|---|---|
| `SyncPoller` | 60s (+ immediate) | Fetches Gamma for active token markets, settles resolved ones |

## Design Decisions

These are intentional simplifications for a **paper / demo trading account**. Do not treat them as bugs or missing features.

### Order types are ignored
`order_type` (GTC / FOK / FAK / GTD) and `post_only` are accepted for Polymarket CLOB API compatibility and stored in the database, but all orders execute identically — instant full fill at the requested price. Bots that rely on order-type semantics (e.g. FOK rejecting on partial fill) will get false-positive results; this is acceptable for a mock environment.

### Resolved markets are not blocked
`PlaceOrder` does not check `outcome_tokens.winner` before filling. Clients can submit orders on already-resolved markets and receive payout on settlement. This is intentional — enforcing market state is not a goal of the paper trading environment.

## OpenAPI Code Generation

`api/openapi/dashboard.yaml` → `api/generated/dashboard/` (types + `ServerInterface`)  
`api/openapi/clob.yaml` → `api/generated/clob/` (types + `ServerInterface`)

Run `make generate` after editing specs. **Never edit generated files directly.**

New endpoints require:
1. Add to `.yaml` spec
2. `make generate`
3. Implement in the appropriate handler
4. Wire in `CLOBServer` or `DashboardHandler`
