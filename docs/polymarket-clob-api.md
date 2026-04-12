# Polymarket CLOB API Reference for This Repo

Verified against official Polymarket docs and the official TypeScript SDK on April 12, 2026. This file is the local default reference for CLOB endpoint shapes, auth headers, and repo-specific behavior.

## Why this file exists

Use this before browsing:

- Official docs: `https://docs.polymarket.com/api-reference`
- Official TS SDK: `Polymarket/clob-client`

This repo is a paper-trading server that mixes:

- local implementations for auth, order placement, order queries, balances, and cancels
- public market-data proxying to real `https://clob.polymarket.com`
- Gamma lookups for unknown token-to-market mapping during order placement

## Current app mapping

Implemented locally:

- `POST /auth/api-key`
- `GET /auth/derive-api-key`
- `GET /auth/api-keys`
- `DELETE /auth/api-key`
- `POST /order`
- `POST /orders`
- `DELETE /order`
- `DELETE /orders`
- `DELETE /cancel-all`
- `DELETE /cancel-market-orders`
- `GET /data/order/{orderId}`
- `GET /data/orders`
- `GET /data/trades`
- `GET /balance-allowance`
- `GET /balance-allowance/update`
- `GET /notifications` stubbed to `[]`
- `DELETE /notifications` accepts official body shape and returns empty `200`
- `GET /order-scoring` stubbed to `{ "scoring": false }`
- `POST /orders-scoring` stubbed to `{ "scoring": false }`
- `POST /v1/heartbeats` returns official-looking `{ "heartbeat_id": "paper-heartbeat" }`

Proxied upstream to real Polymarket CLOB:

- `GET /time`
- `GET /tick-size`
- `GET /tick-size/{token_id}`
- `GET /neg-risk`
- `GET /fee-rate`
- `GET /fee-rate/{token_id}`
- `GET /book`
- `POST /books`
- `GET /midpoint`
- `POST /midpoints`
- `GET /price`
- `GET /prices`
- `POST /prices`
- `GET /spread`
- `POST /spreads`
- `GET /last-trade-price`
- `GET /last-trades-prices`
- `POST /last-trades-prices`
- `GET /sampling-simplified-markets`
- `GET /sampling-markets`
- `GET /simplified-markets`
- `GET /markets`
- `GET /markets/{marketId}`
- `GET /live-activity/events/{eventId}`

Known official surface not currently exposed by this repo:

- `GET /prices-history`
- readonly API key endpoints
- builder API key endpoints
- closed-only / ban-status auth helpers

## Auth model

### Public

No auth required for market-data endpoints.

### L1 auth

Used to create or derive API credentials. Headers:

- `POLY_ADDRESS`: wallet or signer address
- `POLY_SIGNATURE`: EIP-712 signature
- `POLY_TIMESTAMP`: request timestamp as string
- `POLY_NONCE`: nonce string, often `"0"`

L1 endpoints:

- `POST /auth/api-key`
- `GET /auth/derive-api-key`

Response shape:

```json
{
  "apiKey": "string",
  "secret": "string",
  "passphrase": "string"
}
```

### L2 auth

Used for order management and private queries. Headers:

- `POLY_ADDRESS`
- `POLY_SIGNATURE`: HMAC signature for request method + path + body + timestamp
- `POLY_TIMESTAMP`
- `POLY_API_KEY`
- `POLY_PASSPHRASE`

This repo validates L2 auth on:

- auth key listing/deletion
- order placement and cancellation
- balance/order/trade/notification/scoring endpoints
- heartbeat

## Core request and response objects

### `SignedOrder`

- `salt`: unique order salt
- `maker`: maker address
- `signer`: signing address
- `taker`: taker address, often zero address for public orders
- `tokenId`: outcome token ID
- `makerAmount`: maker-side amount as string
- `takerAmount`: taker-side amount as string
- `expiration`: unix timestamp string
- `nonce`: onchain cancel nonce string
- `feeRateBps`: fee rate string
- `side`: `BUY` or `SELL`
- `signatureType`: wallet/signature mode integer
- `signature`: signed order payload

### `POST /order` body

```json
{
  "order": { "tokenId": "...", "makerAmount": "...", "takerAmount": "...", "side": "BUY" },
  "owner": "0x...",
  "orderType": "GTC",
  "postOnly": false
}
```

Fields:

- `owner`: Polymarket profile/funder address associated with the order
- `orderType`: `GTC`, `FOK`, `GTD`, `FAK`
- `postOnly`: optional maker-only flag

### Order response

- `success`: boolean
- `errorMsg`: empty on success
- `orderID`: server order id
- `transactionsHashes`: hash list, often empty in this repo
- `status`: typically `LIVE`, `MATCHED`, or `CANCELED`
- `takingAmount`: string
- `makingAmount`: string

### Open order object

- `id`
- `status`
- `owner`
- `maker_address`
- `market`
- `asset_id`
- `side`
- `original_size`
- `size_matched`
- `price`
- `associate_trades`
- `outcome`
- `created_at`
- `expiration`
- `order_type`

### Trade object

- `id`
- `taker_order_id`
- `market`
- `asset_id`
- `side`
- `size`
- `fee_rate_bps`
- `price`
- `status`
- `match_time`
- `last_update`
- `outcome`
- `owner`
- `maker_address`
- `bucket_index`
- `transaction_hash`
- `trader_side`: `TAKER` or `MAKER`
- `maker_orders`: maker-side fills

## Endpoint reference

### System and auth

| Method | Path | Auth | Params | Notes |
| --- | --- | --- | --- | --- |
| `GET` | `/time` | public | none | Server timestamp. This repo proxies upstream. |
| `POST` | `/auth/api-key` | L1 | headers only | Create API creds. |
| `GET` | `/auth/derive-api-key` | L1 | headers only | Return existing API creds for signer/profile. |
| `GET` | `/auth/api-keys` | L2 | headers only | Returns `{ "apiKeys": [{ "apiKey", "secret", "passphrase" }] }`. |
| `DELETE` | `/auth/api-key` | L2 | headers only in this repo | Official surface deletes a key; this repo deletes the key referenced by `POLY_API_KEY`. |

### Trading and cancels

| Method | Path | Auth | Params | Notes |
| --- | --- | --- | --- | --- |
| `POST` | `/order` | L2 | body: `order`, `owner`, `orderType`, optional `postOnly` | Main single-order endpoint. |
| `POST` | `/orders` | L2 | body: array of `/order` payloads | Batch placement. Local OpenAPI caps at 15. |
| `DELETE` | `/order` | L2 | body: `{ "orderID": "..." }` | Returns Polymarket-style `{ canceled, not_canceled }`. |
| `DELETE` | `/orders` | L2 | body: `["orderId1", "orderId2"]` | Cancel many. |
| `DELETE` | `/cancel-all` | L2 | no body | Cancel all open orders for caller. |
| `DELETE` | `/cancel-market-orders` | L2 | body: `{ "market"?: "condition_id", "asset_id"?: "token_id" }` | Supports market-only, asset-only, both, or neither. If neither is provided, it behaves like cancel-all. |

### Private queries

| Method | Path | Auth | Params | Notes |
| --- | --- | --- | --- | --- |
| `GET` | `/data/order/{orderId}` | L2 | path: `orderId` | Fetch one order. |
| `GET` | `/data/orders` | L2 | query: `market`, `asset_id`, `next_cursor` | Open orders. Repo returns `{ next_cursor, data }`. |
| `GET` | `/data/trades` | L2 | query: `market`, `asset_id`, `next_cursor` | Trade history. The official docs currently also show `/trades`; the TS SDK still exports `/data/trades`. This repo follows the SDK path. |
| `GET` | `/balance-allowance` | L2 | query: `asset_type`, optional `token_id`, optional `signature_type` | `asset_type` is `COLLATERAL` or `CONDITIONAL`. |
| `GET` | `/balance-allowance/update` | L2 | none in repo | Refresh allowance. This repo just returns current balance/allowance view. |
| `GET` | `/notifications` | L2 | none | Stubbed empty list in this repo. |
| `DELETE` | `/notifications` | L2 | body: `{ "ids": ["..."] }` | Stubbed. Accepts the official body shape and returns empty `200`. |
| `GET` | `/order-scoring` | L2 | query: `orderId` in repo, SDK type names `order_id` | Scoring status. Repo always returns false. |
| `POST` | `/orders-scoring` | L2 | body: `{ "orderIds": ["..."] }` | Bulk scoring. Official SDK models per-order booleans; repo returns a single false flag. |
| `POST` | `/v1/heartbeats` | L2 | body: `{ "heartbeat_id": "..." }` | Keepalive/heartbeat. Repo is a stub but returns official-looking `{ "heartbeat_id": "paper-heartbeat" }`. |

### Public market data

Shared request helpers:

- single-token reads generally use query `token_id`
- `/price` also requires `side=BUY|SELL`
- batch endpoints use arrays of `{ token_id, side? }`

`BookParams` from the official TS SDK:

```json
{ "token_id": "token", "side": "BUY" }
```

`side` is optional or ignored on some batch reads but is part of the shared SDK helper type.

| Method | Path | Auth | Params | Returns |
| --- | --- | --- | --- | --- |
| `GET` | `/tick-size` | public | `token_id` | Tick size string such as `"0.01"`. |
| `GET` | `/tick-size/{token_id}` | public | path `token_id` | Proxied upstream. Path-parameter variant from official docs. |
| `GET` | `/neg-risk` | public | `token_id` | Whether market uses neg-risk mechanics. |
| `GET` | `/fee-rate` | public | `token_id` | Proxied upstream. Returns official payload like `{ "base_fee": 30 }`. |
| `GET` | `/fee-rate/{token_id}` | public | path `token_id` | Proxied upstream. Path-parameter variant from official docs. |
| `GET` | `/book` | public | `token_id` | Full order book summary for one token. |
| `POST` | `/books` | public | body: `[{ "token_id": "..." }]` | Array of order book summaries. |
| `GET` | `/midpoint` | public | `token_id` | Mid price for one token. |
| `POST` | `/midpoints` | public | body: `[{ "token_id": "..." }]` | Mid prices for many tokens. |
| `GET` | `/price` | public | `token_id`, `side` | Price for a side on one token. |
| `GET` | `/prices` | public | `token_ids`, `sides` | Proxied upstream. Official query-parameter batch variant. |
| `POST` | `/prices` | public | body: `[{ "token_id": "...", "side": "BUY" }]` | Many side-specific prices. |
| `GET` | `/spread` | public | `token_id` | Best ask minus best bid. |
| `POST` | `/spreads` | public | body: `[{ "token_id": "..." }]` | Many spreads. |
| `GET` | `/last-trade-price` | public | `token_id` | Most recent trade price. |
| `GET` | `/last-trades-prices` | public | `token_ids` | Proxied upstream. Official query-parameter batch variant. |
| `POST` | `/last-trades-prices` | public | body: `[{ "token_id": "..." }]` | Many last-trade prices. |
| `GET` | `/sampling-simplified-markets` | public | none | Sampling feed of simplified markets. |
| `GET` | `/sampling-markets` | public | none | Sampling feed of markets. |
| `GET` | `/simplified-markets` | public | none | Full simplified market list. |
| `GET` | `/markets` | public | optional `next_cursor` | Paginated market list. |
| `GET` | `/markets/{marketId}` | public | path: `marketId` | Single market detail. |
| `GET` | `/live-activity/events/{eventId}` | public | path: `eventId` | Live activity/trade events for one event. |

Useful `/book` response fields from the official docs:

- `market`: condition ID
- `asset_id`: token ID
- `timestamp`
- `hash`
- `bids`: descending by price
- `asks`: ascending by price
- `min_order_size`
- `tick_size`
- `neg_risk`
- `last_trade_price`

## Repo-specific behavior notes

- Order placement derives `price` and `size` from `makerAmount` and `takerAmount`, not from a top-level request field.
- If a token is unknown locally, the service fetches Gamma market data and stores the market/token mapping before placing the order.
- Matching is paper-only and attempts immediate execution against the upstream Polymarket order book.
- `FOK` is canceled if not fully fillable.
- `FAK` partially fills then cancels the remainder.
- Balance allowance in paper mode is effectively wallet balance mirrored as allowance.
- Several official endpoints are intentionally stubbed here for compatibility, not full parity.

## Gaps and mismatches to remember

- Official docs and the TS SDK are not perfectly aligned on every path and response shape.
- The TS SDK currently exposes several endpoints not implemented in this repo, especially readonly keys, builder keys, and price history.
- Official SDK type `ApiKeyCreds` uses `key`, while raw REST responses commonly use `apiKey`. This repo returns raw REST-style `apiKey`.

## When future work should still verify externally

Use this file first. Only re-check the internet if:

- you need an endpoint not listed under this repo
- Polymarket auth/signing behavior appears to have changed
- you need exact current response examples for unimplemented upstream-only endpoints
- you need newer SDK-only surfaces such as RFQ or builder-specific flows

## Primary sources used to build this file

- Official docs index: `https://docs.polymarket.com/index`
- Official API reference root: `https://docs.polymarket.com/api-reference`
- Official CLOB method docs such as L2 methods and cancel/order docs
- Official TS SDK: `https://github.com/Polymarket/clob-client`
- SDK endpoint constants: `src/endpoints.ts`
- SDK types: `src/types.ts`
