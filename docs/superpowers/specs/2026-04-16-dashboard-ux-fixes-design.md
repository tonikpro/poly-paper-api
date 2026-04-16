# Dashboard UX Fixes Design

**Date:** 2026-04-16

## Overview

Four focused fixes to improve observability and usability of the paper trading dashboard:
1. Worker tick logging
2. Orders page: all statuses + market name + pagination
3. Positions page: expandable trade history + pagination
4. Remove History page

---

## 1. Worker Tick Logging

**File:** `internal/trading/worker.go`

Add structured `slog.Info` logs to `tick()`:

- **Start of tick:** `"worker tick"` with `live_orders=N` and `tokens=M`. Skip logging if 0 orders.
- **Per order:** after `MatchOrder` returns, log `"worker order"` with `order_id`, `side`, `price`, `remaining`, and either `fill_size`+`fill_price` (if matched) or `"no match"`.
- **End of tick:** `"worker tick done"` with `fills=N`, `no_match=M`, `errors=K`.

No changes to error logging (already present).

---

## 2. Orders Page

### Backend — `internal/auth/dashboard_queries.go` `GetOrders`

Add `LEFT JOIN markets m ON o.market = m.id` to the data query. Return `COALESCE(m.question, '') AS question` alongside existing fields.

The status filter already works; no change needed there.

### Frontend — `dashboard/src/pages/Orders.tsx`

**Tab filter:** All / Live / Matched / Canceled. Tabs drive the `status` param passed to `getOrders`. Auto-refresh interval (5 s) only active on the Live tab.

**New columns:**
- **Market** — `question` truncated to 50 chars with `title` tooltip; falls back to `asset_id` slice if empty.
- **Status** — colored badge: Live=blue, Matched=green, Canceled=gray.

**Pagination:** 20 rows per page. `limit=20`, `offset=(page-1)*20`. Show Prev/Next buttons; disable Prev on page 1, disable Next when `total <= page*20`.

**Column order:** Market | Side | Outcome | Price | Size | Filled | Status | Type | Created | Action

---

## 3. Positions Page with Inline Trade History

### Backend — `internal/auth/dashboard_queries.go` `GetTrades`

Add optional `assetID *string` parameter. When non-nil, append `AND t.asset_id = $N` to both count and data queries.

Dashboard handler `GetTrades` in `internal/auth/handler.go`: read `asset_id` query param and pass to `GetTrades`.

### Frontend — `dashboard/src/pages/Positions.tsx`

**Data loading:** on mount, fire `getPositions()` and `getTrades({ limit: 1000 })` in parallel. Group trades into `Map<token_id, Trade[]>` on the client.

**Expandable rows:** clicking a position row toggles an expanded sub-row. The sub-row spans all columns and renders a compact table of fills:

| Time | Side | Price | Size | Result | P&L |

Uses `winner` and `profit_loss` already returned by `GetTrades`.

**Pagination:** 20 positions per page, client-side slice of the full positions array. Prev/Next buttons.

**Expanded state:** tracked in `Set<string>` of position IDs. Survives page changes within the session.

**Remove History page:**
- Delete `dashboard/src/pages/History.tsx`
- Remove `/history` route from `dashboard/src/App.tsx`
- Remove "History" nav item from `dashboard/src/components/Layout.tsx`

---

## Data Relationships

```
positions.token_id  ←→  trades.asset_id
positions.user_id   ←→  trades.user_id
```

Client-side grouping: `trades.filter(t => t.asset_id === position.token_id)`.

---

## Non-Goals

- No server-side pagination for trades (load all, group client-side)
- No real-time push for positions (page-level polling is out of scope)
- No changes to CLOB API endpoints
