# Instant Fill Simplification Design

**Date:** 2026-04-15  
**Status:** Approved

## Overview

Remove Polymarket orderbook matching from the paper trading service. All orders execute instantly at the requested price. This simplifies the backend logic, cleans up the database schema, and merges the Orders/Trades pages in the dashboard into a single History page.

API compatibility with the Polymarket CLOB API is fully preserved.

## Background

Currently, POST /orders creates an order in LIVE status, then attempts to match it against the real Polymarket orderbook. A background worker periodically retries unmatched LIVE orders. This complexity is unnecessary for a mock API where the goal is bot testing — instant execution at the requested price is the correct behavior.

## What Changes

### Backend — `internal/trading/`

**Deleted files:**
- `matcher.go` — orderbook fetching and matching logic
- `worker.go` — background matching worker and its registration in `main.go`

**Deleted methods from `service.go`:**
- `tryMatch`
- `executeFill`
- `matchAgainstBook`
- `MatchLiveOrders`

**`PlaceOrder` rewritten as a single atomic transaction:**

```
POST /orders
  1. deriveOrderPriceAndSize()
  2. fetchAndStoreToken() — unchanged, still lazily caches from Gamma API
  3. BEGIN TX
     a. DebitWallet — reserve funds (BUY: collateral, SELL: conditional tokens)
     b. CreateOrder — status=MATCHED, size_matched=original_size
     c. CreateTrade — fill at requested price
     d. UpsertPosition — update portfolio
  4. COMMIT
  5. Return OrderResponse{success:true, status:"MATCHED"}
```

No LIVE order state. No FOR UPDATE locking. No two-phase fill. All order types (GTC/FOK/FAK/GTD) are accepted for API compatibility but execute identically — instant full fill.

**`Service` struct:**
- Remove `matcher *Matcher` field
- `NewService` no longer takes a `Matcher` argument

### Database — migration `004_simplify_instant_fill.sql`

**`orders` table — remove columns:**
- `associate_trades` (JSONB) — stored partial fill trade references, no longer needed
- Update CHECK constraint: remove `DELAYED` from allowed status values

**`trades` table — remove columns:**
- `transaction_hash` — blockchain-specific, unused in paper trading
- `maker_orders` (JSONB) — blockchain-specific
- `bucket_index` — blockchain-specific
- `fill_key` — deduplication guard for concurrent fills, no longer needed
- `trader_side` — always `TAKER`, carries no information

### Dashboard — `dashboard/src/`

**Deleted pages:**
- `pages/Orders.tsx`
- `pages/Trades.tsx`

**New page:** `pages/History.tsx`

Fetches from `GET /data/trades`. Displays a single table:

| Side | Market | Outcome | Price | Size | P&L | Time |
|---|---|---|---|---|---|---|

- Side: BUY (green) / SELL (red)
- Market: question text truncated, token_id as fallback
- Outcome: YES / NO
- Price: fill price
- Size: fill size
- P&L: shown after market resolution (win/loss badge + dollar amount)
- Time: match_time

**Navigation:** replace "Orders" and "Trades" links with single "History" link.

**`pages/Positions.tsx`:** no changes.

## What Does NOT Change

- All HTTP request/response shapes — 100% Polymarket CLOB API compatible
- `fetchAndStoreToken` — Gamma API caching logic unchanged
- Cancel endpoints (single, batch, all, by market) — unchanged
- Resolution sync worker (`sync/`) — unchanged
- Auth, wallet, proxy — unchanged
- `GET /orders`, `GET /data/trades` API endpoints — both remain, return same shapes

## Files Touched

| File | Change |
|---|---|
| `internal/trading/service.go` | Rewrite `PlaceOrder`, delete matching methods |
| `internal/trading/matcher.go` | Delete |
| `internal/trading/worker.go` | Delete |
| `internal/trading/repository.go` | Remove `GetLiveOrdersForUpdate`, `GetOrderByIDForUpdate`, `CancelLiveOrdersByTokenID`; remove `associate_trades` from `insertOrderSQL` |
| `cmd/server/main.go` | Remove worker registration, remove Matcher construction |
| `internal/database/migrations/004_simplify_instant_fill.sql` | New migration |
| `dashboard/src/pages/Orders.tsx` | Delete |
| `dashboard/src/pages/Trades.tsx` | Delete |
| `dashboard/src/pages/History.tsx` | New file |
| `dashboard/src/components/Layout.tsx` | Update navigation |
| `dashboard/src/App.tsx` | Update routes |
