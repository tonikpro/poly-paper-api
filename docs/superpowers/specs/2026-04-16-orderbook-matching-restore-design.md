# Orderbook Matching Restore Design

**Date:** 2026-04-16  
**Status:** Approved

## Overview

Restore realistic order filling against the live Polymarket orderbook. Orders that cannot be immediately filled (orderbook unavailable or insufficient liquidity) stay `LIVE` and are retried by a background worker. Fill prices reflect actual orderbook levels (weighted average), not the user's requested price.

## Background

The previous refactor (2026-04-15) removed all orderbook matching in favour of instant fills at the requested price. This worked for basic bot testing but is unrealistic: bots can't observe whether their price is actually achievable, partial fills don't happen, and all orders succeed regardless of market conditions.

This design restores matching while keeping the architectural improvements from the simplification: single-transaction fills, no two-phase commit.

## Architecture

### `internal/trading/orderbook.go` (new file, replaces `matcher.go`)

Two responsibilities only:

```
FetchOrderBook(tokenID string) (*OrderBookResponse, error)
MatchOrder(book *OrderBookResponse, side string, limitPrice, size float64) *MatchResult
```

`MatchOrder` is a **pure function** — it takes an already-fetched `OrderBookResponse` and returns a `MatchResult`. It does not make HTTP calls. This allows the background worker to fetch the book once per token and run multiple orders through it.

`MatchResult`:
```go
type MatchResult struct {
    FillSize  float64  // how much was filled
    FillPrice float64  // weighted average fill price
    Remaining float64  // unfilled size
    Partial   bool
}
```

BUY matching: walks asks sorted ascending, fills levels where `ask_price <= limit_price`.  
SELL matching: walks bids sorted descending, fills levels where `bid_price >= limit_price`.  
Fill price = weighted average across matched levels.

### `internal/trading/service.go` — `PlaceOrder`

Single-transaction flow:

```
1. deriveOrderPriceAndSize()
2. fetchAndStoreToken()
3. FetchOrderBook(tokenID)          ← may fail
4. MatchOrder(book, side, price, size)
5. Determine order status:
   - FillSize == OriginalSize        → MATCHED
   - FillSize > 0, FillSize < size:
       FOK                           → CANCELED (no order created)
       FAK                           → CANCELED (partial fill recorded, rest canceled)
       GTC/GTD                       → LIVE (partial fill recorded, order stays open)
   - FillSize == 0                   → LIVE
6. BEGIN TX
   a. DebitWallet (reserve at limit price)
   b. CreateOrder
   c. If FillSize > 0:
      - CreateTrade (at fill price)
      - CreditWallet excess if BUY and fill_price < limit_price
      - CreditWallet received assets
      - UpsertPosition
7. COMMIT
8. Return OrderResponse
```

**Fallback:** if `FetchOrderBook` returns any error (timeout, 5xx, network error) → create order as `LIVE` with no fill. Worker will retry.

**404 from orderbook** (market resolved/closed) → return error to caller, do not create order.

All fills happen in a **single transaction** — no two-phase commit.

### `internal/trading/worker.go` (new file, replaces `worker.go`)

Ticks every second:

```
1. GetAllLiveOrders()
2. Group by token_id
3. For each unique token_id:
   a. FetchOrderBook(tokenID)
   b. If 404 → CancelLiveOrdersByTokenID(tokenID)  ← market resolved
   c. For each LIVE order on this token:
      - MatchOrder(book, side, price, size)
      - If FillSize > 0 → executeFill(order, matchResult)
```

Worker fetches **one orderbook per token**, not one per order. If 100 LIVE orders share a token, that's 1 HTTP request total.

`executeFill` (called by worker):
- `SELECT ... FOR UPDATE` on the order (prevents double-fill if worker runs overlapping ticks)
- Verify order is still `LIVE`
- Single transaction: CreateTrade + UpdateOrderFill + CreditWallet + UpsertPosition
- Handles excess collateral refund for BUY orders where fill_price < limit_price

### `internal/trading/repository.go`

Restore methods removed in the simplification:
- `GetAllLiveOrders(ctx) ([]*models.Order, error)`
- `GetOrderByIDForUpdate(ctx, tx, orderID) (*models.Order, error)`
- `CancelLiveOrdersByTokenID(ctx, tokenID string) error`
- `UpdateOrderFill(ctx, tx, orderID, sizeMatched, status, tradeID string) error`

### Database — `005_restore_live_orders.sql`

```sql
-- Restore fill_key for deduplication in worker
ALTER TABLE trades ADD COLUMN IF NOT EXISTS fill_key TEXT;
ALTER TABLE trades ADD CONSTRAINT trades_fill_key_unique UNIQUE (fill_key);

-- Restore associate_trades for tracking partial fill trade references
ALTER TABLE orders ADD COLUMN IF NOT EXISTS associate_trades JSONB;

-- Ensure LIVE is allowed in orders status CHECK (may already be present)
-- Verify and add if missing
```

### `cmd/server/main.go`

- Construct `OrderBookClient` (replaces `Matcher`)
- Pass to `NewService`
- Start matching worker alongside existing sync worker

## Dashboard Changes

### `dashboard/src/pages/Orders.tsx` (restored)

Table of active `LIVE` orders, polling `GET /orders`:

| Side | Market | Outcome | Price | Size | Filled | Type | Created | Action |
|---|---|---|---|---|---|---|---|---|

- Side: BUY (green) / SELL (red)
- Filled: `size_matched / original_size` (e.g. `60.00 / 100.00`)
- Action: Cancel button → `DELETE /order/{id}`
- Filters: only status=`LIVE` orders shown

### `dashboard/src/pages/Dashboard.tsx`

Remove the `Open Orders` metric card entirely. It was showing all orders regardless of status, which was misleading.

### `dashboard/src/components/Layout.tsx`

Add "Orders" navigation link alongside "History".

### `dashboard/src/App.tsx`

Add route `/orders → Orders`.

## Order Type Behaviour Summary

| Type | Fully filled | Partially filled | Not filled at all |
|---|---|---|---|
| GTC | MATCHED | LIVE (partial trade) | LIVE |
| GTD | MATCHED | LIVE (partial trade) | LIVE |
| FOK | MATCHED | CANCELED | CANCELED |
| FAK | MATCHED | CANCELED (partial trade) | CANCELED |

## Error Handling

| Scenario | Behaviour |
|---|---|
| Orderbook timeout / 5xx | Order created as LIVE, worker retries |
| Orderbook 404 (market closed) | Error returned to caller, no order created |
| Insufficient balance | Error returned, no order created |
| Worker fill race (double-tick) | FOR UPDATE lock prevents double-fill |

## Files Touched

| File | Change |
|---|---|
| `internal/trading/orderbook.go` | New — FetchOrderBook, MatchOrder (pure), MatchResult |
| `internal/trading/worker.go` | New — matching worker, ticks every 1s |
| `internal/trading/service.go` | Update PlaceOrder: fetch book, match, single tx |
| `internal/trading/repository.go` | Restore GetAllLiveOrders, GetOrderByIDForUpdate, CancelLiveOrdersByTokenID, UpdateOrderFill |
| `internal/database/migrations/005_restore_live_orders.sql` | New — restore fill_key, associate_trades |
| `cmd/server/main.go` | Construct OrderBookClient, start worker |
| `dashboard/src/pages/Orders.tsx` | Restored — LIVE orders table with cancel |
| `dashboard/src/pages/Dashboard.tsx` | Remove Open Orders metric card |
| `dashboard/src/components/Layout.tsx` | Add Orders nav link |
| `dashboard/src/App.tsx` | Add /orders route |
