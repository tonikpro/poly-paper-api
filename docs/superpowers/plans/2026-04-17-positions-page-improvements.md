# Positions Page Improvements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add tab-based Open/Closed split, `created_at` column, net size for closed positions, and server-side offset pagination to the dashboard positions page.

**Architecture:** Backend adds `tab`, `limit`, `offset` query params to `GET /api/positions`, filters and paginates in SQL, and computes `net_size` via subquery. Frontend renders two tab-specific column sets and delegates pagination to the server.

**Tech Stack:** Go + oapi-codegen (OpenAPI → generated types/interface), pgx v5, React + TypeScript + Tailwind CSS

---

## Files

| Action | File | What changes |
|--------|------|-------------|
| Modify | `api/openapi/dashboard.yaml` | Add `tab`, `limit`, `offset` params to `/api/positions`; add `total` to `DashboardPositionsResponse` |
| Regenerate | `api/generated/dashboard/` | `make generate` — updates `GetDashboardPositionsParams` struct and method signature |
| Modify | `internal/auth/dashboard_queries.go` | New `GetPositions` signature; SQL adds `net_size`, `created_at`, `is_open` filter, pagination |
| Modify | `internal/auth/handler.go` | `GetDashboardPositions` uses generated params struct, returns `total` |
| Modify | `dashboard/src/api/client.ts` | `getPositions` accepts `tab`, `limit`, `offset` |
| Modify | `dashboard/src/pages/Positions.tsx` | Tabs, server-side pagination, per-tab column sets |

---

## Task 1: Update OpenAPI spec and regenerate

**Files:**
- Modify: `api/openapi/dashboard.yaml:151-164`
- Regenerate: `api/generated/dashboard/` (do not hand-edit)

- [ ] **Step 1: Add params and total to the spec**

In `api/openapi/dashboard.yaml`, replace the `/api/positions` block (lines 151–164):

```yaml
  /api/positions:
    get:
      operationId: getDashboardPositions
      tags: [dashboard]
      summary: Get user positions for dashboard view
      security:
        - bearerAuth: []
      parameters:
        - name: tab
          in: query
          schema:
            type: string
            enum: [open, closed]
            default: open
        - name: limit
          in: query
          schema:
            type: integer
            default: 20
        - name: offset
          in: query
          schema:
            type: integer
            default: 0
      responses:
        "200":
          description: Positions list
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/DashboardPositionsResponse"
```

Also update `DashboardPositionsResponse` schema (around line 285) to add `total`:

```yaml
    DashboardPositionsResponse:
      type: object
      properties:
        positions:
          type: array
          items:
            type: object
            additionalProperties: true
        total:
          type: integer
```

- [ ] **Step 2: Regenerate**

```bash
make generate
```

Expected: no errors; `api/generated/dashboard/server.gen.go` now contains:

```go
type GetDashboardPositionsParams struct {
    Tab    *string `form:"tab,omitempty"    json:"tab,omitempty"`
    Limit  *int    `form:"limit,omitempty"  json:"limit,omitempty"`
    Offset *int    `form:"offset,omitempty" json:"offset,omitempty"`
}
```

And the interface method signature changes to:

```go
GetDashboardPositions(w http.ResponseWriter, r *http.Request, params GetDashboardPositionsParams)
```

- [ ] **Step 3: Fix handler stub to match new signature (compile fix)**

In `internal/auth/handler.go`, update `GetDashboardPositions` to accept the new params arg (keep existing behaviour for now — full wiring comes in Task 3):

```go
func (h *DashboardHandler) GetDashboardPositions(w http.ResponseWriter, r *http.Request, params dashboard.GetDashboardPositionsParams) {
	userID := GetUserID(r.Context())
	positions, err := h.queries.GetPositions(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get positions"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"positions": positions})
}
```

- [ ] **Step 4: Verify it compiles**

```bash
go build ./...
```

Expected: exits 0, no output.

- [ ] **Step 5: Commit**

```bash
git add api/openapi/dashboard.yaml api/generated/dashboard/ internal/auth/handler.go
git commit -m "feat: add tab/limit/offset params to GET /api/positions OpenAPI spec"
```

---

## Task 2: Update `GetPositions` SQL query

**Files:**
- Modify: `internal/auth/dashboard_queries.go:127-163`

- [ ] **Step 1: Replace `GetPositions` with the new implementation**

Replace the entire `GetPositions` function (lines 127–163) in `internal/auth/dashboard_queries.go`:

```go
func (q *DashboardQueries) GetPositions(ctx context.Context, userID string, isOpen bool, limit, offset int) ([]map[string]any, int, error) {
	const baseWhere = `
		FROM positions p
		LEFT JOIN outcome_tokens ot ON p.token_id = ot.token_id
		LEFT JOIN markets m ON p.market_id = m.id
		WHERE p.user_id = $1
		  AND (p.size > 0 OR ABS(p.realized_pnl) > 0.000001)
		  AND (ot.winner IS NULL) = $2`

	var total int
	if err := q.pool.QueryRow(ctx,
		`SELECT COUNT(*)`+baseWhere,
		userID, isOpen,
	).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := q.pool.Query(ctx,
		`SELECT p.id, p.token_id, p.outcome, p.size::text, p.avg_price::text, p.realized_pnl::text,
		        p.created_at,
		        ot.winner,
		        COALESCE(m.question, '') AS question,
		        ot.winner IS NULL AS is_open,
		        COALESCE((
		            SELECT SUM(CASE WHEN t.side = 'BUY' THEN t.size::numeric ELSE -t.size::numeric END)
		            FROM trades t
		            WHERE t.user_id = p.user_id AND t.asset_id = p.token_id
		        ), 0)::text AS net_size`+
			baseWhere+`
		ORDER BY p.created_at DESC
		LIMIT $3 OFFSET $4`,
		userID, isOpen, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var positions []map[string]any
	for rows.Next() {
		var id, tokenID, outcome, size, avgPrice, rpnl, question, netSize string
		var createdAt any
		var winner *bool
		var isOpenRow bool
		if err := rows.Scan(&id, &tokenID, &outcome, &size, &avgPrice, &rpnl,
			&createdAt, &winner, &question, &isOpenRow, &netSize); err != nil {
			return nil, 0, err
		}
		positions = append(positions, map[string]any{
			"id": id, "token_id": tokenID, "outcome": outcome,
			"size": size, "avg_price": avgPrice, "realized_pnl": rpnl,
			"created_at": createdAt, "winner": winner, "question": question,
			"is_open": isOpenRow, "net_size": netSize,
		})
	}
	if positions == nil {
		positions = []map[string]any{}
	}
	return positions, total, nil
}
```

- [ ] **Step 2: Verify it compiles (handler still calls old 1-arg signature — this will fail)**

```bash
go build ./...
```

Expected: compile error in `handler.go` — `GetPositions` now requires 4 args. Good — move to Task 3.

---

## Task 3: Wire handler to the updated query

**Files:**
- Modify: `internal/auth/handler.go:157-165`

- [ ] **Step 1: Replace `GetDashboardPositions` with full implementation**

Replace `GetDashboardPositions` in `internal/auth/handler.go`:

```go
func (h *DashboardHandler) GetDashboardPositions(w http.ResponseWriter, r *http.Request, params dashboard.GetDashboardPositionsParams) {
	userID := GetUserID(r.Context())

	tab := "open"
	if params.Tab != nil && *params.Tab == "closed" {
		tab = "closed"
	}
	isOpen := tab == "open"

	limit := 20
	if params.Limit != nil && *params.Limit > 0 && *params.Limit <= 100 {
		limit = *params.Limit
	}
	offset := 0
	if params.Offset != nil && *params.Offset > 0 {
		offset = *params.Offset
	}

	positions, total, err := h.queries.GetPositions(r.Context(), userID, isOpen, limit, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get positions"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"positions": positions,
		"total":     total,
		"limit":     limit,
		"offset":    offset,
	})
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./...
```

Expected: exits 0, no output.

- [ ] **Step 3: Smoke-test with curl (requires running server)**

```bash
make docker-up   # start postgres if not running
make run &       # start server in background
sleep 2

# register and get token
TOKEN=$(curl -s -X POST localhost:8080/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"plan-test@test.com","password":"test1234"}' | jq -r .token)

# open positions tab (empty — no trades yet, just verify shape)
curl -s localhost:8080/api/positions?tab=open&limit=5&offset=0 \
  -H "Authorization: Bearer $TOKEN" | jq .
```

Expected response shape:
```json
{ "positions": [], "total": 0, "limit": 5, "offset": 0 }
```

- [ ] **Step 4: Commit**

```bash
git add internal/auth/dashboard_queries.go internal/auth/handler.go
git commit -m "feat: positions API — server-side pagination, tab filter, created_at, net_size"
```

---

## Task 4: Update frontend API client

**Files:**
- Modify: `dashboard/src/api/client.ts:44`

- [ ] **Step 1: Update `getPositions` signature**

Replace line 44 in `dashboard/src/api/client.ts`:

```ts
export const getPositions = (params?: { tab?: 'open' | 'closed'; limit?: number; offset?: number }) =>
  api.get('/api/positions', { params });
```

- [ ] **Step 2: Commit**

```bash
git add dashboard/src/api/client.ts
git commit -m "feat: update getPositions to accept tab/limit/offset params"
```

---

## Task 5: Rewrite Positions.tsx

**Files:**
- Modify: `dashboard/src/pages/Positions.tsx`

- [ ] **Step 1: Replace the entire file**

```tsx
import { useEffect, useState } from 'react';
import { getPositions, getTrades } from '../api/client';

interface Position {
  id: string;
  token_id: string;
  outcome: string;
  size: string;
  avg_price: string;
  realized_pnl: string;
  winner: boolean | null;
  question: string;
  is_open: boolean;
  created_at: string;
  net_size: string;
}

interface Trade {
  id: string;
  asset_id: string;
  side: string;
  price: string;
  size: string;
  status: string;
  match_time: string;
  outcome: string;
  winner: boolean | null;
  profit_loss: number | null;
}

const LIMIT = 20;

function ResultBadge({ winner }: { winner: boolean | null }) {
  if (winner === null) return <span className="bg-gray-100 text-gray-500 text-xs px-2 py-0.5 rounded-full">Settled</span>;
  return winner
    ? <span className="bg-green-100 text-green-700 text-xs px-2 py-0.5 rounded-full font-medium">Won</span>
    : <span className="bg-red-100 text-red-700 text-xs px-2 py-0.5 rounded-full font-medium">Lost</span>;
}

function TradeResultBadge({ side, winner }: { side: string; winner: boolean | null }) {
  if (side !== 'BUY') return <span className="text-gray-400 text-xs">—</span>;
  if (winner === null) return <span className="bg-gray-100 text-gray-500 text-xs px-2 py-0.5 rounded-full">Pending</span>;
  return winner
    ? <span className="bg-green-100 text-green-700 text-xs px-2 py-0.5 rounded-full font-medium">Win</span>
    : <span className="bg-red-100 text-red-700 text-xs px-2 py-0.5 rounded-full font-medium">Loss</span>;
}

function formatDate(iso: string): string {
  return new Date(iso).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' });
}

export default function Positions() {
  const [activeTab, setActiveTab] = useState<'open' | 'closed'>('open');
  const [positions, setPositions] = useState<Position[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const [tradesByToken, setTradesByToken] = useState<Map<string, Trade[]>>(new Map());
  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  // Load trades once for the expand sub-table
  useEffect(() => {
    getTrades({ limit: 1000 })
      .then(res => {
        const map = new Map<string, Trade[]>();
        for (const t of (res.data.trades || []) as Trade[]) {
          const list = map.get(t.asset_id) || [];
          list.push(t);
          map.set(t.asset_id, list);
        }
        setTradesByToken(map);
      })
      .catch(() => {});
  }, []);

  // Load positions when tab or page changes
  useEffect(() => {
    setLoading(true);
    setExpanded(new Set());
    getPositions({ tab: activeTab, limit: LIMIT, offset: (page - 1) * LIMIT })
      .then(res => {
        setPositions(res.data.positions || []);
        setTotal(res.data.total || 0);
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, [activeTab, page]);

  const switchTab = (tab: 'open' | 'closed') => {
    setActiveTab(tab);
    setPage(1);
  };

  const toggleExpand = (id: string) => {
    setExpanded(prev => {
      const next = new Set(prev);
      next.has(id) ? next.delete(id) : next.add(id);
      return next;
    });
  };

  const totalPages = Math.ceil(total / LIMIT);

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Positions</h1>

      <div className="bg-white rounded-lg shadow overflow-hidden">
        {/* Tab bar */}
        <div className="flex border-b border-gray-200 px-0">
          <button
            onClick={() => switchTab('open')}
            className={`px-6 py-3 text-sm font-medium border-b-2 transition-colors ${
              activeTab === 'open'
                ? 'border-indigo-500 text-indigo-600'
                : 'border-transparent text-gray-500 hover:text-gray-700'
            }`}
          >
            Open
          </button>
          <button
            onClick={() => switchTab('closed')}
            className={`px-6 py-3 text-sm font-medium border-b-2 transition-colors ${
              activeTab === 'closed'
                ? 'border-indigo-500 text-indigo-600'
                : 'border-transparent text-gray-500 hover:text-gray-700'
            }`}
          >
            Closed
          </button>
        </div>

        {loading ? (
          <div className="text-center py-8 text-gray-500">Loading...</div>
        ) : positions.length === 0 ? (
          <div className="p-8 text-center text-gray-500">
            No {activeTab} positions.
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="min-w-full divide-y divide-gray-200">
              <thead className="bg-gray-50">
                {activeTab === 'open' ? (
                  <tr>
                    <th className="px-4 py-3 w-6"></th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Market</th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Outcome</th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Size</th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Avg Price</th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Realized P&L</th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-indigo-600 uppercase">Opened ↓</th>
                  </tr>
                ) : (
                  <tr>
                    <th className="px-4 py-3 w-6"></th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Market</th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Outcome</th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Net Size</th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Avg Price</th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Result</th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Realized P&L</th>
                    <th className="px-4 py-3 text-left text-xs font-medium text-indigo-600 uppercase">Opened ↓</th>
                  </tr>
                )}
              </thead>
              <tbody className="divide-y divide-gray-200">
                {positions.map(p => {
                  const pnl = parseFloat(p.realized_pnl);
                  const trades = tradesByToken.get(p.token_id) || [];
                  const isExpanded = expanded.has(p.id);

                  return (
                    <>
                      <tr
                        key={p.id}
                        className="hover:bg-gray-50 cursor-pointer select-none"
                        onClick={() => toggleExpand(p.id)}
                      >
                        <td className="px-4 py-3 text-gray-400 text-xs">
                          {trades.length > 0 ? (isExpanded ? '▾' : '▸') : ''}
                        </td>
                        <td className="px-4 py-3 max-w-[260px]">
                          {p.question
                            ? <span className="text-sm" title={p.question}>
                                {p.question.length > 60 ? p.question.slice(0, 60) + '…' : p.question}
                              </span>
                            : <span className="text-xs font-mono text-gray-400" title={p.token_id}>
                                {p.token_id.slice(0, 14)}…
                              </span>
                          }
                        </td>
                        <td className="px-4 py-3 font-medium">{p.outcome}</td>

                        {activeTab === 'open' ? (
                          <>
                            <td className="px-4 py-3">{parseFloat(p.size) > 0 ? parseFloat(p.size).toFixed(2) : '—'}</td>
                            <td className="px-4 py-3">{p.avg_price}</td>
                            <td className={`px-4 py-3 font-medium ${pnl > 0 ? 'text-green-600' : pnl < 0 ? 'text-red-600' : 'text-gray-400'}`}>
                              {pnl !== 0 ? `${pnl > 0 ? '+' : ''}$${pnl.toFixed(2)}` : '—'}
                            </td>
                          </>
                        ) : (
                          <>
                            <td className="px-4 py-3">{parseFloat(p.net_size).toFixed(2)}</td>
                            <td className="px-4 py-3">{p.avg_price}</td>
                            <td className="px-4 py-3"><ResultBadge winner={p.winner} /></td>
                            <td className={`px-4 py-3 font-medium ${pnl > 0 ? 'text-green-600' : pnl < 0 ? 'text-red-600' : 'text-gray-400'}`}>
                              {pnl !== 0 ? `${pnl > 0 ? '+' : ''}$${pnl.toFixed(2)}` : '—'}
                            </td>
                          </>
                        )}

                        <td className="px-4 py-3 text-gray-500 text-sm">
                          {p.created_at ? formatDate(p.created_at) : '—'}
                        </td>
                      </tr>

                      {isExpanded && trades.length > 0 && (
                        <tr key={`${p.id}-trades`}>
                          <td colSpan={activeTab === 'open' ? 7 : 8} className="px-0 py-0 bg-gray-50">
                            <div className="px-8 py-3">
                              <table className="min-w-full text-xs">
                                <thead>
                                  <tr className="text-gray-400 uppercase">
                                    <th className="px-2 py-1 text-left">Time</th>
                                    <th className="px-2 py-1 text-left">Side</th>
                                    <th className="px-2 py-1 text-left">Price</th>
                                    <th className="px-2 py-1 text-left">Size</th>
                                    <th className="px-2 py-1 text-left">Result</th>
                                    <th className="px-2 py-1 text-left">P&L</th>
                                  </tr>
                                </thead>
                                <tbody className="divide-y divide-gray-100">
                                  {trades.map(t => (
                                    <tr key={t.id} className="text-gray-700">
                                      <td className="px-2 py-1.5 text-gray-500">
                                        {new Date(t.match_time).toLocaleString()}
                                      </td>
                                      <td className={`px-2 py-1.5 font-medium ${t.side === 'BUY' ? 'text-green-600' : 'text-red-600'}`}>
                                        {t.side}
                                      </td>
                                      <td className="px-2 py-1.5">{t.price}</td>
                                      <td className="px-2 py-1.5">{t.size}</td>
                                      <td className="px-2 py-1.5">
                                        <TradeResultBadge side={t.side} winner={t.winner} />
                                      </td>
                                      <td className="px-2 py-1.5">
                                        {t.profit_loss !== null && t.profit_loss !== undefined
                                          ? <span className={t.profit_loss >= 0 ? 'text-green-600' : 'text-red-600'}>
                                              {t.profit_loss >= 0 ? '+' : ''}${t.profit_loss.toFixed(2)}
                                            </span>
                                          : <span className="text-gray-400">—</span>
                                        }
                                      </td>
                                    </tr>
                                  ))}
                                </tbody>
                              </table>
                            </div>
                          </td>
                        </tr>
                      )}
                    </>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}

        {/* Pagination */}
        {totalPages > 1 && (
          <div className="flex items-center justify-between px-4 py-3 border-t border-gray-200 text-sm text-gray-600">
            <span>{total} positions · page {page} of {totalPages}</span>
            <div className="flex space-x-2">
              <button
                onClick={() => setPage(p => p - 1)}
                disabled={page === 1}
                className="px-3 py-1 rounded border disabled:opacity-40 hover:bg-gray-50"
              >
                Prev
              </button>
              <button
                onClick={() => setPage(p => p + 1)}
                disabled={page >= totalPages}
                className="px-3 py-1 rounded border disabled:opacity-40 hover:bg-gray-50"
              >
                Next
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Check TypeScript compiles**

```bash
cd dashboard && npx tsc --noEmit
```

Expected: exits 0, no type errors.

- [ ] **Step 3: Commit**

```bash
git add dashboard/src/pages/Positions.tsx
git commit -m "feat: positions page — tabs, server-side pagination, net_size, created_at column"
```

---

## Task 6: Manual end-to-end verification

- [ ] **Step 1: Start the stack**

```bash
make docker-up
make run &
make dev-dashboard
```

Open http://localhost:5173, log in, go to Positions.

- [ ] **Step 2: Verify Open tab**

- Shows open positions sorted newest-first with "Opened" column
- Pagination controls appear when > 20 positions
- Expanding a row shows trades sub-table

- [ ] **Step 3: Verify Closed tab**

- Shows "Net Size" column with computed value (not `—`)
- "Result" badge: Won / Lost / Settled
- "Opened" column shows original entry date
- Switching from Open → Closed resets to page 1

- [ ] **Step 4: Final commit if any tweaks needed**

```bash
git add -p
git commit -m "fix: positions page adjustments from manual testing"
```
