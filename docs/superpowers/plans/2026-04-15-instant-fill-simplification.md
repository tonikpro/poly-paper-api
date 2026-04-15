# Instant Fill Simplification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove Polymarket orderbook matching from POST /orders — all orders execute instantly at the requested price, background worker is deleted, DB schema is cleaned up, and the dashboard merges Orders/Trades into a single History page.

**Architecture:** `PlaceOrder` becomes one atomic DB transaction: debit wallet → create order (MATCHED) → create trade → credit wallet → update position. No LIVE order state, no `Matcher`, no background worker. API response shapes are unchanged for 100% Polymarket CLOB compatibility.

**Tech Stack:** Go 1.22, pgx v5, PostgreSQL 15, React 18 + TypeScript, Tailwind CSS.

---

## File Map

| File | Change |
|---|---|
| `internal/database/migrations/004_simplify_instant_fill.sql` | **Create** — drop obsolete columns |
| `internal/trading/matcher.go` | **Delete** |
| `internal/trading/worker.go` | **Delete** |
| `internal/trading/service.go` | **Rewrite** PlaceOrder; delete tryMatch, executeFill, matchAgainstBook, MatchLiveOrders |
| `internal/trading/repository.go` | **Modify** — remove dead methods, update INSERT/SELECT SQL |
| `internal/models/models.go` | **Modify** — remove AssociateTrades from Order/OpenOrder, remove FillKey from Trade |
| `cmd/server/main.go` | **Modify** — remove Matcher + StartMatchWorker |
| `internal/trading/service_test.go` | **Modify** — delete matchAgainstBook tests |
| `dashboard/src/pages/Orders.tsx` | **Delete** |
| `dashboard/src/pages/Trades.tsx` | **Delete** |
| `dashboard/src/pages/History.tsx` | **Create** |
| `dashboard/src/App.tsx` | **Modify** — update routes |
| `dashboard/src/components/Layout.tsx` | **Modify** — update nav links |

---

## Task 1: DB Migration

**Files:**
- Create: `internal/database/migrations/004_simplify_instant_fill.sql`

- [ ] **Step 1: Write the migration**

```sql
-- Remove columns only needed for orderbook matching.
-- fill_key UNIQUE constraint is auto-dropped with the column.

ALTER TABLE orders DROP COLUMN IF EXISTS associate_trades;

ALTER TABLE trades DROP COLUMN IF EXISTS fill_key;
ALTER TABLE trades DROP COLUMN IF EXISTS transaction_hash;
ALTER TABLE trades DROP COLUMN IF EXISTS maker_orders;
ALTER TABLE trades DROP COLUMN IF EXISTS bucket_index;
ALTER TABLE trades DROP COLUMN IF EXISTS trader_side;
```

- [ ] **Step 2: Verify migration applies cleanly**

```bash
make docker-up
make run   # migrations run automatically on startup; look for "migration applied: 004"
```

Expected: server starts, no migration error in logs.

- [ ] **Step 3: Commit**

```bash
git add internal/database/migrations/004_simplify_instant_fill.sql
git commit -m "db: remove matching-only columns (004 migration)"
```

---

## Task 2: Update Models

**Files:**
- Modify: `internal/models/models.go`

- [ ] **Step 1: Remove AssociateTrades from Order and OpenOrder; remove FillKey from Trade**

In `models.go`, apply these changes:

In the `Order` struct, remove:
```go
AssociateTrades []string  `json:"associate_trades"`
```

In the `OpenOrder` struct, remove:
```go
AssociateTrades []string `json:"associate_trades"`
```

In the `Trade` struct, remove:
```go
FillKey         string `json:"-"`
```

The `Trade` struct keeps `BucketIndex`, `TransactionHash`, `TraderSide`, `MakerOrders` fields — they are no longer stored in DB but will be set to defaults so the CLOB API response shape stays identical.

- [ ] **Step 2: Build to check no compilation errors**

```bash
go build ./...
```

Expected: compile errors in repository.go and service.go (they reference removed fields — that's expected, we fix them in the next tasks).

- [ ] **Step 3: Commit**

```bash
git add internal/models/models.go
git commit -m "models: remove associate_trades and fill_key fields"
```

---

## Task 3: Update Repository

**Files:**
- Modify: `internal/trading/repository.go`

Changes: remove `associate_trades` from all ORDER INSERT/SELECT SQL and scan calls; update `CreateTrade` to not insert the removed columns; update `GetTradesByUserID` scan; delete `GetOrderByIDForUpdate`, `GetLiveOrdersForUpdate`, `CancelLiveOrdersByTokenID`, `UpdateOrderFill`.

- [ ] **Step 1: Replace insertOrderSQL and insertOrderArgs**

Replace the existing `insertOrderSQL` and `insertOrderArgs` functions with:

```go
func insertOrderSQL(_ *models.Order) string {
	return `INSERT INTO orders (user_id, salt, maker, signer, taker, token_id,
			maker_amount, taker_amount, side, expiration, nonce, fee_rate_bps,
			signature_type, signature, price, original_size, size_matched, status,
			order_type, post_only, owner, market, asset_id, outcome)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24)
		 RETURNING id, created_at, updated_at`
}

func insertOrderArgs(o *models.Order) []any {
	var outcome interface{} = o.Outcome
	if o.Outcome == "" {
		outcome = nil
	}
	return []any{
		o.UserID, o.Salt, o.Maker, o.Signer, o.Taker, o.TokenID,
		o.MakerAmount, o.TakerAmount, o.Side, o.Expiration, o.Nonce, o.FeeRateBps,
		o.SignatureType, o.Signature, o.Price, o.OriginalSize, o.SizeMatched, o.Status,
		o.OrderType, o.PostOnly, o.Owner, o.Market, o.AssetID, outcome,
	}
}
```

- [ ] **Step 2: Replace GetOrderByID (remove associate_trades from SELECT)**

Replace the body of `GetOrderByID`:

```go
func (r *Repository) GetOrderByID(ctx context.Context, orderID string) (*models.Order, error) {
	return r.scanOrder(r.pool.QueryRow(ctx,
		`SELECT id, user_id, salt, maker, signer, taker, token_id,
			maker_amount, taker_amount, side, expiration, nonce, fee_rate_bps,
			signature_type, signature, price, original_size, size_matched, status,
			order_type, post_only, owner, market, asset_id, outcome,
			created_at, updated_at
		 FROM orders WHERE id = $1`, orderID))
}
```

- [ ] **Step 3: Replace GetOrdersByUserID and GetAllOrdersByUserID (remove associate_trades)**

Replace `GetOrdersByUserID`:

```go
func (r *Repository) GetOrdersByUserID(ctx context.Context, userID string, market, assetID, cursor *string) ([]*models.Order, string, error) {
	query := `SELECT id, user_id, salt, maker, signer, taker, token_id,
			maker_amount, taker_amount, side, expiration, nonce, fee_rate_bps,
			signature_type, signature, price, original_size, size_matched, status,
			order_type, post_only, owner, market, asset_id, outcome,
			created_at, updated_at
		 FROM orders WHERE user_id = $1 AND status = 'LIVE'`
	args := []any{userID}
	argIdx := 2

	if market != nil && *market != "" {
		query += fmt.Sprintf(" AND market = $%d", argIdx)
		args = append(args, *market)
		argIdx++
	}
	if assetID != nil && *assetID != "" {
		query += fmt.Sprintf(" AND asset_id = $%d", argIdx)
		args = append(args, *assetID)
		argIdx++
	}
	if cursor != nil && *cursor != "" {
		query += fmt.Sprintf(" AND id < $%d", argIdx)
		args = append(args, *cursor)
	}
	query += " ORDER BY created_at DESC LIMIT 101"

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("get orders: %w", err)
	}
	defer rows.Close()

	orders, err := r.scanOrders(rows)
	if err != nil {
		return nil, "", err
	}

	var nextCursor string
	if len(orders) > 100 {
		nextCursor = orders[99].ID
		orders = orders[:100]
	}
	return orders, nextCursor, nil
}
```

Replace `GetAllOrdersByUserID`:

```go
func (r *Repository) GetAllOrdersByUserID(ctx context.Context, userID string) ([]*models.Order, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, user_id, salt, maker, signer, taker, token_id,
			maker_amount, taker_amount, side, expiration, nonce, fee_rate_bps,
			signature_type, signature, price, original_size, size_matched, status,
			order_type, post_only, owner, market, asset_id, outcome,
			created_at, updated_at
		 FROM orders WHERE user_id = $1 AND status = 'LIVE'
		 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("get all orders: %w", err)
	}
	defer rows.Close()
	return r.scanOrders(rows)
}
```

- [ ] **Step 4: Replace scanOrder and scanOrders (remove associateTrades scan var)**

Replace `scanOrder`:

```go
func (r *Repository) scanOrder(row pgx.Row) (*models.Order, error) {
	o := &models.Order{}
	err := row.Scan(
		&o.ID, &o.UserID, &o.Salt, &o.Maker, &o.Signer, &o.Taker, &o.TokenID,
		&o.MakerAmount, &o.TakerAmount, &o.Side, &o.Expiration, &o.Nonce, &o.FeeRateBps,
		&o.SignatureType, &o.Signature, &o.Price, &o.OriginalSize, &o.SizeMatched, &o.Status,
		&o.OrderType, &o.PostOnly, &o.Owner, &o.Market, &o.AssetID, &o.Outcome,
		&o.CreatedAt, &o.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan order: %w", err)
	}
	return o, nil
}
```

Replace `scanOrders`:

```go
func (r *Repository) scanOrders(rows pgx.Rows) ([]*models.Order, error) {
	var orders []*models.Order
	for rows.Next() {
		o := &models.Order{}
		if err := rows.Scan(
			&o.ID, &o.UserID, &o.Salt, &o.Maker, &o.Signer, &o.Taker, &o.TokenID,
			&o.MakerAmount, &o.TakerAmount, &o.Side, &o.Expiration, &o.Nonce, &o.FeeRateBps,
			&o.SignatureType, &o.Signature, &o.Price, &o.OriginalSize, &o.SizeMatched, &o.Status,
			&o.OrderType, &o.PostOnly, &o.Owner, &o.Market, &o.AssetID, &o.Outcome,
			&o.CreatedAt, &o.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan order: %w", err)
		}
		orders = append(orders, o)
	}
	return orders, nil
}
```

- [ ] **Step 5: Replace CreateTrade (remove dropped columns, set defaults on struct)**

Replace the entire `CreateTrade` method:

```go
func (r *Repository) CreateTrade(ctx context.Context, tx pgx.Tx, t *models.Trade) error {
	var matchTime, lastUpdate time.Time
	err := tx.QueryRow(ctx,
		`INSERT INTO trades (taker_order_id, user_id, market, asset_id, side, size,
			fee_rate_bps, price, status, outcome, owner, maker_address)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		 RETURNING id, match_time, last_update`,
		t.TakerOrderID, t.UserID, t.Market, t.AssetID, t.Side, t.Size,
		t.FeeRateBps, t.Price, t.Status, t.Outcome, t.Owner, t.MakerAddress,
	).Scan(&t.ID, &matchTime, &lastUpdate)
	if err != nil {
		return fmt.Errorf("create trade: %w", err)
	}
	t.MatchTime = matchTime.Format(time.RFC3339)
	t.LastUpdate = lastUpdate.Format(time.RFC3339)
	// Set API-compatible defaults for fields no longer stored in DB
	t.TraderSide = "TAKER"
	t.TransactionHash = ""
	t.BucketIndex = 0
	t.MakerOrders = []byte("[]")
	return nil
}
```

- [ ] **Step 6: Replace GetTradesByUserID (remove dropped columns from SELECT and scan)**

Replace the entire `GetTradesByUserID` method:

```go
func (r *Repository) GetTradesByUserID(ctx context.Context, userID string, market, assetID, cursor *string) ([]models.Trade, string, error) {
	query := `SELECT id, taker_order_id, user_id, market, asset_id, side, size,
			fee_rate_bps, price, status, match_time, last_update, outcome,
			owner, maker_address
		 FROM trades WHERE user_id = $1`
	args := []any{userID}
	argIdx := 2

	if market != nil && *market != "" {
		query += fmt.Sprintf(" AND market = $%d", argIdx)
		args = append(args, *market)
		argIdx++
	}
	if assetID != nil && *assetID != "" {
		query += fmt.Sprintf(" AND asset_id = $%d", argIdx)
		args = append(args, *assetID)
		argIdx++
	}
	if cursor != nil && *cursor != "" {
		query += fmt.Sprintf(" AND id < $%d", argIdx)
		args = append(args, *cursor)
	}
	query += " ORDER BY match_time DESC LIMIT 101"

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("get trades: %w", err)
	}
	defer rows.Close()

	var trades []models.Trade
	for rows.Next() {
		var t models.Trade
		var matchTime, lastUpdate time.Time
		if err := rows.Scan(
			&t.ID, &t.TakerOrderID, &t.UserID, &t.Market, &t.AssetID, &t.Side,
			&t.Size, &t.FeeRateBps, &t.Price, &t.Status, &matchTime, &lastUpdate,
			&t.Outcome, &t.Owner, &t.MakerAddress,
		); err != nil {
			return nil, "", fmt.Errorf("scan trade: %w", err)
		}
		t.MatchTime = matchTime.Format(time.RFC3339)
		t.LastUpdate = lastUpdate.Format(time.RFC3339)
		t.TraderSide = "TAKER"
		t.TransactionHash = ""
		t.BucketIndex = 0
		t.MakerOrders = []byte("[]")
		trades = append(trades, t)
	}

	var nextCursor string
	if len(trades) > 100 {
		nextCursor = trades[99].ID
		trades = trades[:100]
	}
	return trades, nextCursor, nil
}
```

- [ ] **Step 7: Delete dead repository methods**

Delete these three methods entirely from `repository.go`:
- `GetOrderByIDForUpdate` (lines ~72–80)
- `GetLiveOrdersForUpdate` (lines ~82–95)
- `CancelLiveOrdersByTokenID` (lines ~284–332)
- `UpdateOrderFill` (lines ~334–340)

- [ ] **Step 8: Build to check**

```bash
go build ./...
```

Expected: compile errors only in `service.go` (still references Matcher and deleted methods) — that's fine, fixed in Task 4.

- [ ] **Step 9: Commit**

```bash
git add internal/trading/repository.go
git commit -m "trading: update repository for instant fill (remove dead methods and dropped columns)"
```

---

## Task 4: Rewrite Service

**Files:**
- Modify: `internal/trading/service.go`

- [ ] **Step 1: Replace PlaceOrder**

Replace the entire `PlaceOrder` method with:

```go
// PlaceOrder validates, creates, and instantly fills an order in one transaction.
func (s *Service) PlaceOrder(ctx context.Context, userID string, req *models.PostOrderRequest) (*models.OrderResponse, error) {
	signed := req.Order

	price, size, err := deriveOrderPriceAndSize(signed.Side, signed.MakerAmount, signed.TakerAmount)
	if err != nil {
		return nil, fmt.Errorf("invalid order amounts: %w", err)
	}

	token, err := s.repo.GetOutcomeToken(ctx, signed.TokenID)
	if err != nil {
		return nil, fmt.Errorf("lookup token: %w", err)
	}
	if token == nil {
		token, err = s.fetchAndStoreToken(ctx, signed.TokenID)
		if err != nil {
			return nil, fmt.Errorf("resolve token: %w", err)
		}
	}

	priceStr := fmt.Sprintf("%.4f", price)
	sizeStr := fmt.Sprintf("%.6f", size)

	order := &models.Order{
		UserID:        userID,
		Salt:          strconv.FormatInt(signed.Salt, 10),
		Maker:         signed.Maker,
		Signer:        signed.Signer,
		Taker:         signed.Taker,
		TokenID:       signed.TokenID,
		MakerAmount:   signed.MakerAmount,
		TakerAmount:   signed.TakerAmount,
		Side:          signed.Side,
		Expiration:    signed.Expiration,
		Nonce:         signed.Nonce,
		FeeRateBps:    signed.FeeRateBps,
		SignatureType: signed.SignatureType,
		Signature:     signed.Signature,
		Price:         priceStr,
		OriginalSize:  sizeStr,
		SizeMatched:   sizeStr, // fully filled immediately
		Status:        "MATCHED",
		OrderType:     req.OrderType,
		PostOnly:      req.PostOnly,
		Owner:         req.Owner,
		Market:        token.MarketID,
		AssetID:       signed.TokenID,
		Outcome:       token.Outcome,
	}

	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Debit reserved funds
	if order.Side == "BUY" {
		reserveStr := fmt.Sprintf("%.6f", size*price)
		if err := s.repo.DebitWallet(ctx, tx, userID, "COLLATERAL", "", reserveStr); err != nil {
			return nil, fmt.Errorf("insufficient balance: %w", err)
		}
	} else {
		if err := s.repo.DebitWallet(ctx, tx, userID, "CONDITIONAL", signed.TokenID, sizeStr); err != nil {
			return nil, fmt.Errorf("insufficient conditional balance: %w", err)
		}
	}

	// 2. Persist order (already MATCHED)
	if err := s.repo.CreateOrderTx(ctx, tx, order); err != nil {
		return nil, fmt.Errorf("create order: %w", err)
	}

	// 3. Create fill record
	trade := &models.Trade{
		TakerOrderID: order.ID,
		UserID:       userID,
		Market:       order.Market,
		AssetID:      order.AssetID,
		Side:         order.Side,
		Size:         sizeStr,
		FeeRateBps:   order.FeeRateBps,
		Price:        priceStr,
		Status:       "MATCHED",
		Outcome:      order.Outcome,
		Owner:        order.Owner,
		MakerAddress: order.Maker,
	}
	if err := s.repo.CreateTrade(ctx, tx, trade); err != nil {
		return nil, fmt.Errorf("create trade: %w", err)
	}

	// 4. Credit received assets
	if order.Side == "BUY" {
		if err := s.repo.CreditWallet(ctx, tx, userID, "CONDITIONAL", signed.TokenID, sizeStr); err != nil {
			return nil, fmt.Errorf("credit conditional: %w", err)
		}
	} else {
		costStr := fmt.Sprintf("%.6f", size*price)
		if err := s.repo.CreditWallet(ctx, tx, userID, "COLLATERAL", "", costStr); err != nil {
			return nil, fmt.Errorf("credit collateral: %w", err)
		}
	}

	// 5. Update position
	pos, err := s.repo.GetPositionForUpdate(ctx, tx, userID, signed.TokenID)
	if err != nil {
		return nil, fmt.Errorf("get position: %w", err)
	}
	if order.Side == "BUY" {
		var newSize, newAvg float64
		if pos != nil {
			existingSize, _ := strconv.ParseFloat(pos.Size, 64)
			existingAvg, _ := strconv.ParseFloat(pos.AvgPrice, 64)
			newSize = existingSize + size
			newAvg = (existingSize*existingAvg + size*price) / newSize
		} else {
			newSize = size
			newAvg = price
		}
		if err := s.repo.UpsertPosition(ctx, tx, userID, signed.TokenID, order.Market, order.Outcome,
			fmt.Sprintf("%.6f", newSize), fmt.Sprintf("%.4f", newAvg)); err != nil {
			return nil, fmt.Errorf("upsert position: %w", err)
		}
	} else {
		if pos != nil {
			existingSize, _ := strconv.ParseFloat(pos.Size, 64)
			newSize := existingSize - size
			if newSize < 0.000001 {
				newSize = 0
			}
			if err := s.repo.UpsertPosition(ctx, tx, userID, signed.TokenID, order.Market, order.Outcome,
				fmt.Sprintf("%.6f", newSize), pos.AvgPrice); err != nil {
				return nil, fmt.Errorf("upsert position: %w", err)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return &models.OrderResponse{
		Success:            true,
		ErrorMsg:           "",
		OrderID:            order.ID,
		TransactionsHashes: []string{},
		Status:             "MATCHED",
		TakingAmount:       order.TakerAmount,
		MakingAmount:       order.MakerAmount,
	}, nil
}
```

- [ ] **Step 2: Delete matching methods from service.go**

Delete these methods entirely:
- `tryMatch` (lines ~167–193)
- `executeFill` (lines ~196–315)
- `matchAgainstBook` (lines ~498–550)
- `MatchLiveOrders` (lines ~416–495)

- [ ] **Step 3: Remove Matcher from Service struct and constructor**

Replace the `Service` struct and `NewService`:

```go
type Service struct {
	repo       *Repository
	gammaURL   string
	httpClient *http.Client
}

func NewService(repo *Repository, gammaURL string) *Service {
	return &Service{
		repo:       repo,
		gammaURL:   gammaURL,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}
```

- [ ] **Step 4: Clean up unused imports in service.go**

Remove from the import block: `"errors"`, `"sort"`.

The remaining imports should be:
```go
import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tonikpro/poly-paper-api/internal/models"
)
```

- [ ] **Step 5: Build to verify**

```bash
go build ./...
```

Expected: compile errors only in `cmd/server/main.go` (still references `NewMatcher` and `StartMatchWorker`). Fixed in Task 5.

- [ ] **Step 6: Commit**

```bash
git add internal/trading/service.go
git commit -m "trading: rewrite PlaceOrder as instant fill, remove matching logic"
```

---

## Task 5: Delete Dead Files and Fix main.go

**Files:**
- Delete: `internal/trading/matcher.go`
- Delete: `internal/trading/worker.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Delete matcher.go and worker.go**

```bash
rm internal/trading/matcher.go internal/trading/worker.go
```

- [ ] **Step 2: Update main.go — remove Matcher and StartMatchWorker**

In `cmd/server/main.go`, replace:

```go
tradingRepo := trading.NewRepository(pool)
matcher := trading.NewMatcher(cfg.PolymarketCLOBURL)
tradingSvc := trading.NewService(tradingRepo, matcher, cfg.PolymarketGammaURL)
```

with:

```go
tradingRepo := trading.NewRepository(pool)
tradingSvc := trading.NewService(tradingRepo, cfg.PolymarketGammaURL)
```

And delete this line (~126):
```go
trading.StartMatchWorker(ctx, tradingSvc, 10*time.Second)
```

- [ ] **Step 3: Build to verify clean compile**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 4: Run all tests**

```bash
make test
```

Expected: `TestDeriveOrderPriceAndSize_*` pass. `TestMatchAgainstBook_*` will FAIL — fixed in Task 6.

- [ ] **Step 5: Commit**

```bash
git add cmd/server/main.go
git rm internal/trading/matcher.go internal/trading/worker.go
git commit -m "trading: delete matcher and worker, update main.go"
```

---

## Task 6: Update Tests

**Files:**
- Modify: `internal/trading/service_test.go`

The `TestMatchAgainstBook_*` tests reference `matchAgainstBook` and `OrderBookResponse`/`OrderBookRow` which no longer exist. Delete them. Keep `TestDeriveOrderPriceAndSize_*`.

- [ ] **Step 1: Replace service_test.go**

Replace the entire file with:

```go
package trading

import (
	"testing"
)

func TestDeriveOrderPriceAndSize_Buy(t *testing.T) {
	// BUY: maker gives 50 USDC (50000000 raw), receives 100 tokens (100000000 raw)
	// price = 50000000/100000000 = 0.5, size = 100000000/1e6 = 100
	price, size, err := deriveOrderPriceAndSize("BUY", "50000000", "100000000")
	if err != nil {
		t.Fatal(err)
	}
	if price != 0.5 {
		t.Errorf("price: got %f, want 0.5", price)
	}
	if size != 100 {
		t.Errorf("size: got %f, want 100", size)
	}
}

func TestDeriveOrderPriceAndSize_Sell(t *testing.T) {
	// SELL: maker gives 100 tokens (100000000 raw), receives 70 USDC (70000000 raw)
	// price = 70000000/100000000 = 0.7, size = 100000000/1e6 = 100
	price, size, err := deriveOrderPriceAndSize("SELL", "100000000", "70000000")
	if err != nil {
		t.Fatal(err)
	}
	if price != 0.7 {
		t.Errorf("price: got %f, want 0.7", price)
	}
	if size != 100 {
		t.Errorf("size: got %f, want 100", size)
	}
}

func TestDeriveOrderPriceAndSize_Invalid(t *testing.T) {
	_, _, err := deriveOrderPriceAndSize("BUY", "invalid", "100")
	if err == nil {
		t.Fatal("expected error for invalid makerAmount")
	}

	_, _, err = deriveOrderPriceAndSize("BUY", "100", "invalid")
	if err == nil {
		t.Fatal("expected error for invalid takerAmount")
	}
}
```

- [ ] **Step 2: Run tests**

```bash
make test
```

Expected: all 3 `TestDeriveOrderPriceAndSize_*` tests PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/trading/service_test.go
git commit -m "tests: remove matchAgainstBook tests (function deleted)"
```

---

## Task 7: Dashboard — History Page

**Files:**
- Delete: `dashboard/src/pages/Orders.tsx`
- Delete: `dashboard/src/pages/Trades.tsx`
- Create: `dashboard/src/pages/History.tsx`

Data source: `GET /api/trades` (via `getTrades` from `api/client.ts`), which returns `{ trades: Trade[], total: number }` where each Trade has: `id`, `asset_id`, `side`, `price`, `size`, `status`, `match_time`, `outcome`, `winner`, `profit_loss`.

- [ ] **Step 1: Delete old pages**

```bash
rm dashboard/src/pages/Orders.tsx dashboard/src/pages/Trades.tsx
```

- [ ] **Step 2: Create History.tsx**

```tsx
import { useEffect, useState } from 'react';
import { getTrades } from '../api/client';

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

export default function History() {
  const [trades, setTrades] = useState<Trade[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    getTrades({ limit: 100 })
      .then(r => setTrades(r.data.trades || []))
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  if (loading) return <div className="text-center py-8">Loading...</div>;

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Trade History</h1>
      {trades.length === 0 ? (
        <div className="bg-white rounded-lg shadow p-8 text-center text-gray-500">
          No trades yet. Place orders using your bot.
        </div>
      ) : (
        <div className="bg-white rounded-lg shadow overflow-x-auto">
          <table className="min-w-full divide-y divide-gray-200">
            <thead className="bg-gray-50">
              <tr>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Side</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Outcome</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Price</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Size</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Result</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">P&L</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Token</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Time</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-200">
              {trades.map(t => (
                <tr key={t.id} className="hover:bg-gray-50">
                  <td className={`px-4 py-3 font-medium ${t.side === 'BUY' ? 'text-green-600' : 'text-red-600'}`}>
                    {t.side}
                  </td>
                  <td className="px-4 py-3 text-sm">{t.outcome || '—'}</td>
                  <td className="px-4 py-3">{t.price}</td>
                  <td className="px-4 py-3">{t.size}</td>
                  <td className="px-4 py-3">
                    <ResultBadge side={t.side} winner={t.winner} />
                  </td>
                  <td className="px-4 py-3 font-medium">
                    {t.profit_loss !== null && t.profit_loss !== undefined
                      ? <span className={t.profit_loss >= 0 ? 'text-green-600' : 'text-red-600'}>
                          {t.profit_loss >= 0 ? '+' : ''}${t.profit_loss.toFixed(2)}
                        </span>
                      : <span className="text-gray-400 text-xs">—</span>
                    }
                  </td>
                  <td className="px-4 py-3 text-xs font-mono truncate max-w-[100px]" title={t.asset_id}>
                    {t.asset_id?.slice(0, 10)}…
                  </td>
                  <td className="px-4 py-3 text-xs text-gray-500">
                    {new Date(t.match_time).toLocaleString()}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

function ResultBadge({ side, winner }: { side: string; winner: boolean | null }) {
  if (side !== 'BUY') return <span className="text-gray-400 text-xs">—</span>;
  if (winner === null) return <span className="bg-gray-100 text-gray-500 text-xs px-2 py-0.5 rounded-full">Pending</span>;
  return winner
    ? <span className="bg-green-100 text-green-700 text-xs px-2 py-0.5 rounded-full font-medium">Win</span>
    : <span className="bg-red-100 text-red-700 text-xs px-2 py-0.5 rounded-full font-medium">Loss</span>;
}
```

- [ ] **Step 3: Commit**

```bash
git add dashboard/src/pages/History.tsx
git rm dashboard/src/pages/Orders.tsx dashboard/src/pages/Trades.tsx
git commit -m "dashboard: replace Orders+Trades pages with unified History page"
```

---

## Task 8: Dashboard — Update Routes and Navigation

**Files:**
- Modify: `dashboard/src/App.tsx`
- Modify: `dashboard/src/components/Layout.tsx`

- [ ] **Step 1: Update App.tsx**

Replace the entire file:

```tsx
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { AuthProvider } from './context/AuthContext';
import Layout from './components/Layout';
import ProtectedRoute from './components/ProtectedRoute';
import Login from './pages/Login';
import Register from './pages/Register';
import Dashboard from './pages/Dashboard';
import History from './pages/History';
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
            <Route path="history" element={<History />} />
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

- [ ] **Step 2: Update Layout.tsx navigation**

Replace the nav links block (inside `<div className="flex items-center space-x-8">`):

```tsx
<Link to="/" className="text-xl font-bold text-gray-900">Poly Paper</Link>
<Link to="/" className="text-gray-600 hover:text-gray-900">Dashboard</Link>
<Link to="/history" className="text-gray-600 hover:text-gray-900">History</Link>
<Link to="/positions" className="text-gray-600 hover:text-gray-900">Positions</Link>
<Link to="/wallet" className="text-gray-600 hover:text-gray-900">Wallet</Link>
<Link to="/api-keys" className="text-gray-600 hover:text-gray-900">API Keys</Link>
```

- [ ] **Step 3: Build dashboard to verify no TypeScript errors**

```bash
make dev-dashboard   # starts Vite; check terminal for TS errors
# Ctrl+C after confirming no errors
```

Expected: Vite starts with no TypeScript compilation errors.

- [ ] **Step 4: Commit**

```bash
git add dashboard/src/App.tsx dashboard/src/components/Layout.tsx
git commit -m "dashboard: update routes and nav to use History page"
```

---

## Final Verification

- [ ] **Start the server and run a smoke test**

```bash
make docker-up
make run
```

Expected in logs:
- `migration applied: 004_simplify_instant_fill` (or "already applied" on re-run)
- No mention of `match worker started`
- Server listening on port

- [ ] **Run all Go tests**

```bash
make test
```

Expected: all tests pass.
