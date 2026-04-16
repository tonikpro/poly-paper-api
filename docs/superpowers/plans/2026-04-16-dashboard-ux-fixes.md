# Dashboard UX Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add worker tick logging, improve Orders page (all statuses + market name + pagination), and merge History into an expandable Positions page.

**Architecture:** Backend changes are additive SQL tweaks in `internal/auth/dashboard_queries.go`. Frontend changes are isolated to the pages and the shared `Layout`/`App` wiring. No OpenAPI regeneration needed — the trades endpoint already supports the call pattern we need.

**Tech Stack:** Go 1.22+, slog, PostgreSQL/pgx, React 18, TypeScript, Axios, Tailwind CSS

---

## File Map

| File | Change |
|------|--------|
| `internal/trading/worker.go` | Add slog.Info for tick start, per-order result, tick summary |
| `internal/auth/dashboard_queries.go` | GetOrders: add LEFT JOIN markets → return `question` field |
| `dashboard/src/api/client.ts` | getTrades: increase default limit to 1000; add `asset_id` param for future use |
| `dashboard/src/pages/Orders.tsx` | Tabs, market name column, status badge, pagination |
| `dashboard/src/pages/Positions.tsx` | Load all trades at mount, group by token_id, expandable rows, pagination |
| `dashboard/src/App.tsx` | Remove `/history` route and `History` import |
| `dashboard/src/components/Layout.tsx` | Remove "History" nav link |
| `dashboard/src/pages/History.tsx` | **Delete** |

---

## Task 1: Worker tick logging

**Files:**
- Modify: `internal/trading/worker.go`

- [ ] **Step 1: Add per-tick info log at the start of `tick()`**

In `worker.go`, replace the early return after the zero-orders check with a logged early return, and add an opening log:

```go
func (w *Worker) tick(ctx context.Context) {
	orders, err := w.svc.repo.GetAllLiveOrders(ctx)
	if err != nil {
		slog.Error("worker: get live orders", "error", err)
		return
	}
	if len(orders) == 0 {
		return
	}

	// count unique tokens
	byToken := make(map[string][]*models.Order)
	for _, o := range orders {
		byToken[o.TokenID] = append(byToken[o.TokenID], o)
	}

	slog.Info("worker tick", "live_orders", len(orders), "tokens", len(byToken))
```

- [ ] **Step 2: Add per-order result log inside the inner loop**

After the `MatchOrder` call, log the outcome. Replace the existing inner loop body:

```go
		for _, order := range tokenOrders {
			limitPrice, err := strconv.ParseFloat(order.Price, 64)
			if err != nil {
				slog.Warn("worker: invalid order price", "order_id", order.ID, "price", order.Price)
				continue
			}
			currentMatched, _ := strconv.ParseFloat(order.SizeMatched, 64)
			originalSize, _ := strconv.ParseFloat(order.OriginalSize, 64)
			remaining := originalSize - currentMatched
			if remaining < 0.000001 {
				continue
			}

			result := MatchOrder(book, order.Side, limitPrice, remaining)
			if result == nil || result.FillSize < 0.000001 {
				slog.Info("worker: no match", "order_id", order.ID, "side", order.Side,
					"limit_price", limitPrice, "remaining", remaining)
				continue
			}

			slog.Info("worker: filling", "order_id", order.ID, "side", order.Side,
				"fill_size", result.FillSize, "fill_price", result.FillPrice,
				"remaining", remaining)

			if err := w.svc.executeFill(ctx, order, result); err != nil {
				slog.Error("worker: execute fill", "order_id", order.ID, "error", err)
			}
		}
```

- [ ] **Step 3: Add tick-summary counters**

Replace the full `tick()` function body to track fills/no-matches:

```go
func (w *Worker) tick(ctx context.Context) {
	orders, err := w.svc.repo.GetAllLiveOrders(ctx)
	if err != nil {
		slog.Error("worker: get live orders", "error", err)
		return
	}
	if len(orders) == 0 {
		return
	}

	byToken := make(map[string][]*models.Order)
	for _, o := range orders {
		byToken[o.TokenID] = append(byToken[o.TokenID], o)
	}

	slog.Info("worker tick", "live_orders", len(orders), "tokens", len(byToken))

	var fills, noMatch, errs int

	for tokenID, tokenOrders := range byToken {
		book, err := w.bookClient.FetchOrderBook(tokenID)
		if err != nil {
			if errors.Is(err, ErrOrderBookNotFound) {
				if cancelErr := w.svc.repo.CancelLiveOrdersByTokenID(ctx, tokenID); cancelErr != nil {
					slog.Error("worker: cancel live orders on 404", "token_id", tokenID, "error", cancelErr)
				}
				continue
			}
			slog.Warn("worker: fetch orderbook failed, skipping token", "token_id", tokenID, "error", err)
			continue
		}

		for _, order := range tokenOrders {
			limitPrice, err := strconv.ParseFloat(order.Price, 64)
			if err != nil {
				slog.Warn("worker: invalid order price", "order_id", order.ID, "price", order.Price)
				continue
			}
			currentMatched, _ := strconv.ParseFloat(order.SizeMatched, 64)
			originalSize, _ := strconv.ParseFloat(order.OriginalSize, 64)
			remaining := originalSize - currentMatched
			if remaining < 0.000001 {
				continue
			}

			result := MatchOrder(book, order.Side, limitPrice, remaining)
			if result == nil || result.FillSize < 0.000001 {
				slog.Info("worker: no match", "order_id", order.ID, "side", order.Side,
					"limit_price", limitPrice, "remaining", remaining)
				noMatch++
				continue
			}

			slog.Info("worker: filling", "order_id", order.ID, "side", order.Side,
				"fill_size", result.FillSize, "fill_price", result.FillPrice)

			if err := w.svc.executeFill(ctx, order, result); err != nil {
				slog.Error("worker: execute fill", "order_id", order.ID, "error", err)
				errs++
			} else {
				fills++
			}
		}
	}

	slog.Info("worker tick done", "fills", fills, "no_match", noMatch, "errors", errs)
}
```

- [ ] **Step 4: Build to verify no compile errors**

```bash
go build ./...
```

Expected: no output (clean build).

- [ ] **Step 5: Commit**

```bash
git add internal/trading/worker.go
git commit -m "feat: add structured logging to matching worker tick"
```

---

## Task 2: GetOrders — add market name to backend response

**Files:**
- Modify: `internal/auth/dashboard_queries.go`

- [ ] **Step 1: Add LEFT JOIN markets to the GetOrders data query**

In `dashboard_queries.go`, update `GetOrders`. Replace the `dataQuery` definition and the `Scan` call:

```go
func (q *DashboardQueries) GetOrders(ctx context.Context, userID string, status *string, limit, offset int) ([]map[string]any, int, error) {
	countQuery := `SELECT COUNT(*) FROM orders WHERE user_id = $1`
	dataQuery := `SELECT o.id, o.token_id, o.asset_id, o.outcome, o.side,
		             o.price::text, o.original_size::text, o.size_matched::text,
		             o.status, o.order_type, o.created_at,
		             COALESCE(m.question, '') AS question
		      FROM orders o
		      LEFT JOIN markets m ON o.market = m.id
		      WHERE o.user_id = $1`
	args := []any{userID}
	argIdx := 2

	if status != nil && *status != "" {
		countQuery += fmt.Sprintf(" AND status = $%d", argIdx)
		dataQuery += fmt.Sprintf(" AND o.status = $%d", argIdx)
		args = append(args, *status)
		argIdx++
	}

	var total int
	if err := q.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	dataQuery += " ORDER BY o.created_at DESC"
	if limit > 0 {
		dataQuery += fmt.Sprintf(" LIMIT %d", limit)
	}
	if offset > 0 {
		dataQuery += fmt.Sprintf(" OFFSET %d", offset)
	}

	rows, err := q.pool.Query(ctx, dataQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var orders []map[string]any
	for rows.Next() {
		var id, tokenID, assetID, outcome, side, price, origSize, sizeMatched, st, orderType, question string
		var createdAt any
		if err := rows.Scan(&id, &tokenID, &assetID, &outcome, &side, &price,
			&origSize, &sizeMatched, &st, &orderType, &createdAt, &question); err != nil {
			return nil, 0, err
		}
		orders = append(orders, map[string]any{
			"id": id, "token_id": tokenID, "asset_id": assetID, "outcome": outcome,
			"side": side, "price": price,
			"original_size": origSize, "size_matched": sizeMatched,
			"status": st, "order_type": orderType, "created_at": createdAt,
			"question": question,
		})
	}
	if orders == nil {
		orders = []map[string]any{}
	}
	return orders, total, nil
}
```

- [ ] **Step 2: Build to verify**

```bash
go build ./...
```

Expected: clean build.

- [ ] **Step 3: Commit**

```bash
git add internal/auth/dashboard_queries.go
git commit -m "feat: add market question to GetOrders dashboard response"
```

---

## Task 3: Orders page — tabs, market column, status badge, pagination

**Files:**
- Modify: `dashboard/src/pages/Orders.tsx`

- [ ] **Step 1: Rewrite Orders.tsx**

Replace the entire file content:

```tsx
import { useEffect, useState, useCallback } from 'react';
import { getOrders, cancelOrder } from '../api/client';

interface Order {
  id: string;
  side: string;
  outcome: string;
  price: string;
  original_size: string;
  size_matched: string;
  order_type: string;
  created_at: string;
  asset_id: string;
  status: string;
  question: string;
}

const TABS = ['All', 'Live', 'Matched', 'Canceled'] as const;
type Tab = typeof TABS[number];

const STATUS_PARAM: Record<Tab, string | undefined> = {
  All: undefined,
  Live: 'LIVE',
  Matched: 'MATCHED',
  Canceled: 'CANCELED',
};

const PAGE_SIZE = 20;

function StatusBadge({ status }: { status: string }) {
  const cls =
    status === 'LIVE'     ? 'bg-blue-100 text-blue-700' :
    status === 'MATCHED'  ? 'bg-green-100 text-green-700' :
    status === 'CANCELED' ? 'bg-gray-100 text-gray-500' :
                            'bg-gray-100 text-gray-500';
  return (
    <span className={`text-xs px-2 py-0.5 rounded-full font-medium ${cls}`}>
      {status}
    </span>
  );
}

export default function Orders() {
  const [tab, setTab] = useState<Tab>('Live');
  const [orders, setOrders] = useState<Order[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);

  const fetchOrders = useCallback(() => {
    const status = STATUS_PARAM[tab];
    getOrders({ status, limit: PAGE_SIZE, offset: (page - 1) * PAGE_SIZE })
      .then(r => {
        setOrders(r.data.orders || []);
        setTotal(r.data.total || 0);
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, [tab, page]);

  useEffect(() => {
    setLoading(true);
    fetchOrders();
  }, [fetchOrders]);

  // Auto-refresh only on Live tab
  useEffect(() => {
    if (tab !== 'Live') return;
    const interval = setInterval(fetchOrders, 5000);
    return () => clearInterval(interval);
  }, [tab, fetchOrders]);

  // Reset to page 1 when tab changes
  useEffect(() => { setPage(1); }, [tab]);

  const handleCancel = (id: string) => {
    setOrders(prev => prev.filter(o => o.id !== id));
    cancelOrder(id).catch(() => fetchOrders());
  };

  const totalPages = Math.ceil(total / PAGE_SIZE);

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Orders</h1>

      {/* Tabs */}
      <div className="flex space-x-1 mb-4 border-b border-gray-200">
        {TABS.map(t => (
          <button
            key={t}
            onClick={() => setTab(t)}
            className={`px-4 py-2 text-sm font-medium border-b-2 -mb-px transition-colors ${
              tab === t
                ? 'border-blue-500 text-blue-600'
                : 'border-transparent text-gray-500 hover:text-gray-700'
            }`}
          >
            {t}
          </button>
        ))}
      </div>

      {loading ? (
        <div className="text-center py-8">Loading...</div>
      ) : orders.length === 0 ? (
        <div className="bg-white rounded-lg shadow p-8 text-center text-gray-500">
          No orders.
        </div>
      ) : (
        <>
          <div className="bg-white rounded-lg shadow overflow-x-auto">
            <table className="min-w-full divide-y divide-gray-200">
              <thead className="bg-gray-50">
                <tr>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Market</th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Side</th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Outcome</th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Price</th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Size</th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Filled</th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Status</th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Type</th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Created</th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Action</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-200">
                {orders.map(o => (
                  <tr key={o.id} className="hover:bg-gray-50">
                    <td className="px-4 py-3 max-w-[220px]">
                      {o.question
                        ? <span className="text-sm" title={o.question}>
                            {o.question.length > 50 ? o.question.slice(0, 50) + '…' : o.question}
                          </span>
                        : <span className="text-xs font-mono text-gray-400" title={o.asset_id}>
                            {o.asset_id?.slice(0, 12)}…
                          </span>
                      }
                    </td>
                    <td className={`px-4 py-3 font-medium ${o.side === 'BUY' ? 'text-green-600' : 'text-red-600'}`}>
                      {o.side}
                    </td>
                    <td className="px-4 py-3 text-sm">{o.outcome || '—'}</td>
                    <td className="px-4 py-3">{o.price}</td>
                    <td className="px-4 py-3">{parseFloat(o.original_size).toFixed(2)}</td>
                    <td className="px-4 py-3 text-sm">
                      {parseFloat(o.size_matched).toFixed(2)} / {parseFloat(o.original_size).toFixed(2)}
                    </td>
                    <td className="px-4 py-3"><StatusBadge status={o.status} /></td>
                    <td className="px-4 py-3 text-sm">{o.order_type}</td>
                    <td className="px-4 py-3 text-xs text-gray-500">
                      {new Date(o.created_at).toLocaleString()}
                    </td>
                    <td className="px-4 py-3">
                      {o.status === 'LIVE' && (
                        <button
                          onClick={() => handleCancel(o.id)}
                          className="text-xs text-red-600 hover:text-red-800 border border-red-200 hover:border-red-400 px-2 py-1 rounded"
                        >
                          Cancel
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {/* Pagination */}
          {totalPages > 1 && (
            <div className="flex items-center justify-between mt-4 text-sm text-gray-600">
              <span>{total} orders · page {page} of {totalPages}</span>
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
        </>
      )}
    </div>
  );
}
```

- [ ] **Step 2: Verify TypeScript compiles**

```bash
cd dashboard && npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add dashboard/src/pages/Orders.tsx
git commit -m "feat: orders page — tabs, market name, status badge, pagination"
```

---

## Task 4: Positions page — merge History in with expandable rows and pagination

**Files:**
- Modify: `dashboard/src/api/client.ts`
- Modify: `dashboard/src/pages/Positions.tsx`
- Modify: `dashboard/src/App.tsx`
- Modify: `dashboard/src/components/Layout.tsx`
- Delete: `dashboard/src/pages/History.tsx`

- [ ] **Step 1: Update getTrades in client.ts to load all**

Replace the `getTrades` line in `dashboard/src/api/client.ts`:

```ts
export const getTrades = (params?: { limit?: number; offset?: number; asset_id?: string }) =>
  api.get('/api/trades', { params });
```

- [ ] **Step 2: Rewrite Positions.tsx**

Replace the entire file content:

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

const PAGE_SIZE = 20;

function StatusBadge({ isOpen, winner }: { isOpen: boolean; winner: boolean | null }) {
  if (isOpen) return <span className="bg-blue-100 text-blue-700 text-xs px-2 py-0.5 rounded-full font-medium">Open</span>;
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

export default function Positions() {
  const [positions, setPositions] = useState<Position[]>([]);
  const [tradesByToken, setTradesByToken] = useState<Map<string, Trade[]>>(new Map());
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    Promise.all([
      getPositions(),
      getTrades({ limit: 1000 }),
    ])
      .then(([posRes, tradeRes]) => {
        setPositions(posRes.data.positions || []);

        const map = new Map<string, Trade[]>();
        for (const t of (tradeRes.data.trades || []) as Trade[]) {
          const list = map.get(t.asset_id) || [];
          list.push(t);
          map.set(t.asset_id, list);
        }
        setTradesByToken(map);
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  const toggleExpand = (id: string) => {
    setExpanded(prev => {
      const next = new Set(prev);
      next.has(id) ? next.delete(id) : next.add(id);
      return next;
    });
  };

  if (loading) return <div className="text-center py-8">Loading...</div>;

  const totalPages = Math.ceil(positions.length / PAGE_SIZE);
  const paginated = positions.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE);

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Positions</h1>

      {positions.length === 0 ? (
        <div className="bg-white rounded-lg shadow p-8 text-center text-gray-500">
          No positions yet.
        </div>
      ) : (
        <>
          <div className="bg-white rounded-lg shadow overflow-x-auto">
            <table className="min-w-full divide-y divide-gray-200">
              <thead className="bg-gray-50">
                <tr>
                  <th className="px-4 py-3 w-6"></th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Market</th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Outcome</th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Size</th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Avg Price</th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Status</th>
                  <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Realized P&L</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-200">
                {paginated.map(p => {
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
                        <td className="px-4 py-3">{parseFloat(p.size).toFixed(2)}</td>
                        <td className="px-4 py-3">{p.avg_price}</td>
                        <td className="px-4 py-3">
                          <StatusBadge isOpen={p.is_open} winner={p.winner} />
                        </td>
                        <td className={`px-4 py-3 font-medium ${pnl > 0 ? 'text-green-600' : pnl < 0 ? 'text-red-600' : 'text-gray-400'}`}>
                          {pnl !== 0 ? `${pnl > 0 ? '+' : ''}$${pnl.toFixed(2)}` : '—'}
                        </td>
                      </tr>

                      {isExpanded && trades.length > 0 && (
                        <tr key={`${p.id}-trades`}>
                          <td colSpan={7} className="px-0 py-0 bg-gray-50">
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

          {totalPages > 1 && (
            <div className="flex items-center justify-between mt-4 text-sm text-gray-600">
              <span>{positions.length} positions · page {page} of {totalPages}</span>
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
        </>
      )}
    </div>
  );
}
```

- [ ] **Step 3: Remove History from App.tsx**

Replace the full content of `dashboard/src/App.tsx`:

```tsx
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { AuthProvider } from './context/AuthContext';
import Layout from './components/Layout';
import ProtectedRoute from './components/ProtectedRoute';
import Login from './pages/Login';
import Register from './pages/Register';
import Dashboard from './pages/Dashboard';
import Orders from './pages/Orders';
import Positions from './pages/Positions';
import Wallet from './pages/Wallet';
import ApiKeys from './pages/ApiKeys';

export default function App() {
  return (
    <AuthProvider>
      <BrowserRouter>
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route path="/register" element={<Register />} />
          <Route path="/" element={<ProtectedRoute><Layout /></ProtectedRoute>}>
            <Route index element={<Dashboard />} />
            <Route path="orders" element={<Orders />} />
            <Route path="positions" element={<Positions />} />
            <Route path="wallet" element={<Wallet />} />
            <Route path="api-keys" element={<ApiKeys />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </AuthProvider>
  );
}
```

- [ ] **Step 4: Remove History link from Layout.tsx**

Replace the nav links section in `dashboard/src/components/Layout.tsx` — remove the History link:

```tsx
              <Link to="/" className="text-gray-600 hover:text-gray-900">Dashboard</Link>
              <Link to="/orders" className="text-gray-600 hover:text-gray-900">Orders</Link>
              <Link to="/positions" className="text-gray-600 hover:text-gray-900">Positions</Link>
              <Link to="/wallet" className="text-gray-600 hover:text-gray-900">Wallet</Link>
              <Link to="/api-keys" className="text-gray-600 hover:text-gray-900">API Keys</Link>
```

- [ ] **Step 5: Delete History.tsx**

```bash
rm dashboard/src/pages/History.tsx
```

- [ ] **Step 6: Verify TypeScript compiles**

```bash
cd dashboard && npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add dashboard/src/api/client.ts dashboard/src/pages/Positions.tsx \
        dashboard/src/App.tsx dashboard/src/components/Layout.tsx
git rm dashboard/src/pages/History.tsx
git commit -m "feat: positions page — expandable trade history, pagination; remove History page"
```
