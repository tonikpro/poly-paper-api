# CLOB Endpoint Parity

For Polymarket CLOB API details, consult `docs/polymarket-clob-api.md` first.

## Implemented Locally

| Method | Path | Notes |
|---|---|---|
| POST | `/clob/auth/api-key` | Create API key (L1) |
| GET | `/clob/auth/api-key` | Derive/get existing API key (L1) |
| GET | `/clob/auth/api-keys` | List all API keys (L1) |
| DELETE | `/clob/auth/api-key` | Delete API key (L1) |
| POST | `/clob/order` | Place single order (L2) |
| POST | `/clob/orders` | Place batch orders (L2) |
| DELETE | `/clob/order` | Cancel single order (L2) |
| DELETE | `/clob/orders` | Cancel batch orders (L2) |
| DELETE | `/clob/orders/all` | Cancel all open orders (L2) |
| DELETE | `/clob/orders/by-market` | Cancel all orders for a market (L2) |
| GET | `/clob/data/orders` | Query open orders (L2) |
| GET | `/clob/data/trades` | Query trade history (L2) |
| GET | `/clob/balance-allowance` | Get USDC balance and allowance (L2) |
| GET | `/clob/notifications` | Stub (200, empty list) |
| GET | `/clob/scoring` | Stub (200, empty) |
| GET | `/clob/are-orders-scoring` | Stub (200, false) |
| GET | `/clob/tick-size` | Proxied |

## Proxied to Live Polymarket

All requests strip the `/clob` prefix before forwarding to `POLYMARKET_CLOB_URL`.

| Endpoints |
|---|
| `GET /time` |
| `GET /tick-size`, `GET /tick-size/{token_id}` |
| `GET /neg-risk` |
| `GET /book`, `POST /books` |
| `GET /midpoint`, `POST /midpoints` |
| `GET /price`, `GET /prices`, `POST /prices` |
| `GET /spread`, `POST /spreads` |
| `GET /last-trade-price`, `GET /last-trades-prices`, `POST /last-trades-prices` |
| `GET /sampling-simplified-markets`, `GET /sampling-markets` |
| `GET /simplified-markets`, `GET /markets`, `GET /markets/{marketId}` |
| `GET /live-activity/events/{eventId}` |

## Known Gaps

- `GET /prices-history` — not implemented
- Readonly API key endpoints — not implemented
- Builder API key endpoints — not implemented
- Closed-only / ban-status auth helpers — not implemented
- Notifications and scoring are stubs, not real data
- `/data/trades` is kept for SDK compatibility; newer `/trades`-style ledger endpoints not present
