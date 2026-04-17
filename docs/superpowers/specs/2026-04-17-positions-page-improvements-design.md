# Positions Page Improvements — Design

**Date:** 2026-04-17  
**Status:** Approved

## Problem

The positions page has four UX issues:
1. All positions (open and closed) are shown in a single unsorted list — hard to scan
2. No `created_at` column, so there's no way to know when a position was opened
3. Closed positions show `—` for size, which loses information about how large the position was
4. Pagination is client-side — all positions loaded upfront regardless of count

## Solution Summary

- Add `created_at` column (labeled "Opened"), sort by it descending at DB level
- Split into two tabs: **Open** and **Closed** — tab filter applied server-side
- For closed positions, compute and show **Net Size** = SUM(BUY trades) − SUM(SELL trades), computed server-side
- Switch to **server-side pagination** (offset-based: `limit` + `offset`)

## Approach

**Backend-heavy:** Backend adds `created_at`, `net_size`, tab filtering, and offset pagination to `GetPositions`. Frontend becomes thin — no filtering, no client-side sorting, no full dataset in memory.

Rejected alternatives:
- *Frontend computes net_size* — trades are fetched lazily and with a limit of 1000; unreliable for closed positions
- *Two separate API endpoints* — unnecessary surface area; a single `is_open` filter param suffices
- *Cursor-based pagination* — offset is simpler and consistent with `GetOrders` on the dashboard

## Backend Changes

### `internal/auth/dashboard_queries.go` — `GetPositions`

**New signature:**
```go
func (q *DashboardQueries) GetPositions(ctx context.Context, userID string, isOpen bool, limit, offset int) ([]map[string]any, int, error)
```

Returns `(positions, totalCount, error)` — total count is for the current tab (matching `is_open` filter).

**New query logic:**

1. Count query (for pagination UI):
```sql
SELECT COUNT(*)
FROM positions p
LEFT JOIN outcome_tokens ot ON p.token_id = ot.token_id
WHERE p.user_id = $1
  AND (p.size > 0 OR ABS(p.realized_pnl) > 0.000001)
  AND (ot.winner IS NULL) = $2   -- $2 = isOpen
```

2. Data query:
```sql
SELECT p.id, p.token_id, p.outcome, p.size::text, p.avg_price::text,
       p.realized_pnl::text, p.created_at,
       ot.winner,
       COALESCE(m.question, '') AS question,
       ot.winner IS NULL AS is_open,
       COALESCE((
         SELECT SUM(CASE WHEN t.side = 'BUY' THEN t.size::numeric ELSE -t.size::numeric END)
         FROM trades t
         WHERE t.user_id = p.user_id AND t.asset_id = p.token_id
       ), 0)::text AS net_size
FROM positions p
LEFT JOIN outcome_tokens ot ON p.token_id = ot.token_id
LEFT JOIN markets m ON p.market_id = m.id
WHERE p.user_id = $1
  AND (p.size > 0 OR ABS(p.realized_pnl) > 0.000001)
  AND (ot.winner IS NULL) = $2
ORDER BY p.created_at DESC
LIMIT $3 OFFSET $4
```

Returned map adds `created_at` (RFC3339 string) and `net_size` (numeric string).

No schema migration needed — `created_at` already exists on `positions`.

### `internal/auth/handler.go` — positions handler

Read query params `tab` (default `"open"`), `limit` (default `20`, max `100`), `offset` (default `0`).
Derive `isOpen = tab != "closed"`.
Call updated `GetPositions(ctx, userID, isOpen, limit, offset)`.
Return JSON:
```json
{
  "positions": [...],
  "total": 42,
  "limit": 20,
  "offset": 0
}
```

## Frontend Changes

### `dashboard/src/api/client.ts`

Update `getPositions` signature:
```ts
export const getPositions = (params?: { tab?: 'open' | 'closed'; limit?: number; offset?: number }) =>
  api.get('/api/positions', { params });
```

### `dashboard/src/pages/Positions.tsx`

**State:**
- `activeTab: 'open' | 'closed'` (default `'open'`)
- `page: number` (default `1`) — reset to 1 on tab switch
- `total: number` — from API response, used for pagination UI
- Remove local `PAGE_SIZE` constant; use `LIMIT = 20` sent to backend

**On mount and on tab/page change:** call `getPositions({ tab: activeTab, limit: LIMIT, offset: (page-1)*LIMIT })`

**Tab bar** (above the table):
- "Open" tab with count badge (blue) — count fetched from API `total` when tab is active; for inactive tab show cached last value or omit badge
- "Closed" tab with count badge (gray)
- Active tab underlined in indigo

**Open tab columns** (7):
`(expand)` | Market | Outcome | Size | Avg Price | Realized P&L | Opened ↓

- `Size`: `parseFloat(p.size).toFixed(2)`, or `—` if zero
- `Opened`: `p.created_at` formatted as `Apr 15, 2026`
- No status badge — all rows here are open

**Closed tab columns** (8):
`(expand)` | Market | Outcome | Net Size | Avg Price | Result | Realized P&L | Opened ↓

- `Net Size`: `p.net_size` formatted to 2 decimal places (never `—`)
- `Result`: Won / Lost / Settled badge (existing `StatusBadge` logic minus the "Open" branch)
- `Opened`: same as open tab

**Pagination UI:**
- `total` from API response drives page count: `Math.ceil(total / LIMIT)`
- "Prev" / "Next" buttons same style as current
- Label: `{total} positions · page {page} of {totalPages}`

**Trades expand:** still loaded via `getTrades({ limit: 1000 })` on mount (unchanged) — sub-table is out of scope.

## Data Flow

```
GET /api/positions?tab=open&limit=20&offset=0
  └─ handler: parse tab, limit, offset
  └─ GetPositions(userID, isOpen=true, limit=20, offset=0)
       └─ SQL: COUNT + paginated SELECT with net_size subquery
  └─ JSON: { positions: [...], total: 42, limit: 20, offset: 0 }
  └─ frontend: render tab, table, pagination
```

## Out of Scope

- Changing the expand/trades sub-table layout or its data loading
- Sorting by columns other than `created_at`
- Filtering by outcome, market, or PnL
