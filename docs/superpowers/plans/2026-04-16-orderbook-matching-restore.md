# Orderbook Matching Restore Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restore realistic order filling against the live Polymarket orderbook, with LIVE orders retried by a background worker and proper cancellation when markets close.

**Architecture:** `orderbook.go` provides a pure `MatchOrder` function and an HTTP client — both `PlaceOrder` and the worker reuse the same matching logic. `PlaceOrder` tries to fill immediately in a single transaction; on orderbook failure the order stays `LIVE`. The worker ticks every second, groups LIVE orders by token (one HTTP fetch per token), fills or cancels.

**Tech Stack:** Go 1.22, pgx/v5, React/TypeScript, Vite, axios

---

## File Map

| File | Action | Purpose |
|---|---|---|
| `internal/database/migrations/005_restore_live_orders.sql` | Create | Add `fill_key` column + unique constraint to `trades` |
| `internal/trading/orderbook.go` | Create | `OrderBookClient`, `FetchOrderBook`, pure `MatchOrder`, `MatchResult` |
| `internal/trading/orderbook_test.go` | Create | Unit tests for `MatchOrder` |
| `internal/trading/repository.go` | Modify | Add `GetAllLiveOrders`, `GetOrderByIDForUpdate`, `CancelLiveOrdersByTokenID`, `UpdateOrderFill` |
| `internal/trading/service.go` | Modify | Add `bookClient` field; rewrite `PlaceOrder`; add `applyFill`, `executeFill`, `determineInitialFill` |
| `internal/trading/worker.go` | Create | Background matching worker |
| `cmd/server/main.go` | Modify | Construct `OrderBookClient`, pass to `NewService`, start worker |
| `dashboard/src/pages/Orders.tsx` | Create | LIVE orders table with cancel button |
| `dashboard/src/pages/Dashboard.tsx` | Modify | Remove `Open Orders` metric card |
| `dashboard/src/components/Layout.tsx` | Modify | Add "Orders" nav link |
| `dashboard/src/App.tsx` | Modify | Add `/orders` route |

---

## Task 1: DB Migration — Restore `fill_key`

**Files:**
- Create: `internal/database/migrations/005_restore_live_orders.sql`

- [ ] **Step 1: Create the migration file**

```sql
-- 005_restore_live_orders.sql
-- Restore fill_key on trades for worker deduplication.
-- fill_key is NULL for PlaceOrder trades (not needed), non-null for worker fills.
-- PostgreSQL UNIQUE ignores NULLs so multiple NULL values are allowed.

ALTER TABLE trades ADD COLUMN IF NOT EXISTS fill_key TEXT;
ALTER TABLE trades DROP CONSTRAINT IF EXISTS trades_fill_key_unique;
ALTER TABLE trades ADD CONSTRAINT trades_fill_key_unique UNIQUE (fill_key);
```

- [ ] **Step 2: Verify migration runs cleanly**

```bash
make docker-up
make run
# Server should start and print "migrations complete" without errors
# Then Ctrl-C
```

Expected: server starts, no migration error in output.

- [ ] **Step 3: Commit**

```bash
git add internal/database/migrations/005_restore_live_orders.sql
git commit -m "migration: restore fill_key on trades for worker dedup"
```

---

## Task 2: `orderbook.go` — Pure Matching Logic

**Files:**
- Create: `internal/trading/orderbook.go`
- Create: `internal/trading/orderbook_test.go`

- [ ] **Step 1: Write the failing tests first**

Create `internal/trading/orderbook_test.go`:

```go
package trading

import (
	"testing"
)

func makeBook(bids, asks [][]float64) *OrderBookResponse {
	b := &OrderBookResponse{}
	for _, level := range bids {
		b.Bids = append(b.Bids, OrderBookRow{
			Price: fmt.Sprintf("%.4f", level[0]),
			Size:  fmt.Sprintf("%.6f", level[1]),
		})
	}
	for _, level := range asks {
		b.Asks = append(b.Asks, OrderBookRow{
			Price: fmt.Sprintf("%.4f", level[0]),
			Size:  fmt.Sprintf("%.6f", level[1]),
		})
	}
	return b
}

func TestMatchOrder_BuyFullFill(t *testing.T) {
	// BUY limit 0.70, asks at 0.60 (size 50) and 0.65 (size 60) — total 110 available
	book := makeBook(nil, [][]float64{{0.60, 50}, {0.65, 60}})
	result := MatchOrder(book, "BUY", 0.70, 100)

	if result.FillSize < 99.9999 || result.FillSize > 100.0001 {
		t.Errorf("FillSize: got %.6f, want 100", result.FillSize)
	}
	// Weighted avg = (50*0.60 + 50*0.65) / 100 = 0.625
	if result.FillPrice < 0.624 || result.FillPrice > 0.626 {
		t.Errorf("FillPrice: got %.4f, want ~0.625", result.FillPrice)
	}
	if result.Remaining > 0.0001 {
		t.Errorf("Remaining: got %.6f, want 0", result.Remaining)
	}
	if result.Partial {
		t.Error("Partial: got true, want false")
	}
}

func TestMatchOrder_BuyPartialFill(t *testing.T) {
	// BUY limit 0.70, only 40 shares available
	book := makeBook(nil, [][]float64{{0.65, 40}})
	result := MatchOrder(book, "BUY", 0.70, 100)

	if result.FillSize < 39.9999 || result.FillSize > 40.0001 {
		t.Errorf("FillSize: got %.6f, want 40", result.FillSize)
	}
	if result.Remaining < 59.9999 || result.Remaining > 60.0001 {
		t.Errorf("Remaining: got %.6f, want 60", result.Remaining)
	}
	if !result.Partial {
		t.Error("Partial: got false, want true")
	}
}

func TestMatchOrder_BuyPriceTooLow(t *testing.T) {
	// BUY limit 0.50, ask at 0.60 — should not fill
	book := makeBook(nil, [][]float64{{0.60, 100}})
	result := MatchOrder(book, "BUY", 0.50, 100)

	if result.FillSize > 0.0001 {
		t.Errorf("FillSize: got %.6f, want 0", result.FillSize)
	}
	if result.Remaining < 99.9999 {
		t.Errorf("Remaining: got %.6f, want 100", result.Remaining)
	}
}

func TestMatchOrder_SellFullFill(t *testing.T) {
	// SELL limit 0.30, bids at 0.40 (size 60) and 0.35 (size 50) — total 110 available
	book := makeBook([][]float64{{0.40, 60}, {0.35, 50}}, nil)
	result := MatchOrder(book, "SELL", 0.30, 100)

	if result.FillSize < 99.9999 || result.FillSize > 100.0001 {
		t.Errorf("FillSize: got %.6f, want 100", result.FillSize)
	}
	// Weighted avg = (60*0.40 + 40*0.35) / 100 = 0.38
	if result.FillPrice < 0.379 || result.FillPrice > 0.381 {
		t.Errorf("FillPrice: got %.4f, want ~0.38", result.FillPrice)
	}
}

func TestMatchOrder_SellPriceTooHigh(t *testing.T) {
	// SELL limit 0.80, best bid is 0.70 — should not fill
	book := makeBook([][]float64{{0.70, 100}}, nil)
	result := MatchOrder(book, "SELL", 0.80, 100)

	if result.FillSize > 0.0001 {
		t.Errorf("FillSize: got %.6f, want 0", result.FillSize)
	}
}

func TestMatchOrder_EmptyBook(t *testing.T) {
	book := makeBook(nil, nil)
	result := MatchOrder(book, "BUY", 0.70, 100)
	if result.FillSize > 0.0001 {
		t.Errorf("FillSize: got %.6f, want 0 on empty book", result.FillSize)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail (file not created yet)**

```bash
go test ./internal/trading/... -run TestMatchOrder -v 2>&1 | head -20
```

Expected: compilation error — `MatchOrder`, `OrderBookResponse`, `OrderBookRow` undefined.

- [ ] **Step 3: Create `orderbook.go`**

```go
package trading

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"
)

// ErrOrderBookNotFound is returned when Polymarket returns 404 for a token.
// This means the market is resolved or closed — orders for this token should be rejected.
var ErrOrderBookNotFound = errors.New("orderbook not found")

// OrderBookResponse is Polymarket's GET /book response.
type OrderBookResponse struct {
	Market  string         `json:"market"`
	AssetID string         `json:"asset_id"`
	Hash    string         `json:"hash"`
	Bids    []OrderBookRow `json:"bids"`
	Asks    []OrderBookRow `json:"asks"`
}

// OrderBookRow is a single price level in the orderbook.
type OrderBookRow struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

// MatchResult holds the outcome of matching an order against the book.
type MatchResult struct {
	FillSize  float64 // how many tokens were filled
	FillPrice float64 // weighted average fill price
	Remaining float64 // unfilled size
	Partial   bool    // true if only partially filled
}

// OrderBookClient fetches the live Polymarket orderbook via HTTP.
type OrderBookClient struct {
	clobURL    string
	httpClient *http.Client
}

// NewOrderBookClient creates a client that fetches from the given CLOB URL.
func NewOrderBookClient(clobURL string) *OrderBookClient {
	return &OrderBookClient{
		clobURL:    clobURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// FetchOrderBook retrieves the current orderbook for a token from Polymarket.
// Returns ErrOrderBookNotFound if the market is closed/resolved (404).
func (c *OrderBookClient) FetchOrderBook(tokenID string) (*OrderBookResponse, error) {
	url := fmt.Sprintf("%s/book?token_id=%s", c.clobURL, tokenID)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch orderbook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: %s", ErrOrderBookNotFound, string(body))
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("orderbook API returned %d: %s", resp.StatusCode, string(body))
	}

	var book OrderBookResponse
	if err := json.NewDecoder(resp.Body).Decode(&book); err != nil {
		return nil, fmt.Errorf("decode orderbook: %w", err)
	}
	return &book, nil
}

// MatchOrder checks how much of an order can be filled against the given orderbook.
// This is a pure function — it does not make any HTTP calls.
//
// BUY: matches against asks sorted ascending — fills levels where ask_price <= limitPrice.
// SELL: matches against bids sorted descending — fills levels where bid_price >= limitPrice.
// Returns a MatchResult with FillSize=0 if nothing can be filled.
func MatchOrder(book *OrderBookResponse, side string, limitPrice, size float64) *MatchResult {
	result := &MatchResult{Remaining: size}

	if side == "BUY" {
		asks := parseLevels(book.Asks)
		sort.Slice(asks, func(i, j int) bool { return asks[i].price < asks[j].price })

		var totalFilled, weightedPrice float64
		for _, level := range asks {
			if limitPrice < level.price {
				break
			}
			canFill := size - totalFilled
			if canFill <= 0 {
				break
			}
			fillAtLevel := canFill
			if fillAtLevel > level.size {
				fillAtLevel = level.size
			}
			weightedPrice += fillAtLevel * level.price
			totalFilled += fillAtLevel
		}

		if totalFilled > 0 {
			result.FillSize = totalFilled
			result.FillPrice = weightedPrice / totalFilled
			result.Remaining = size - totalFilled
			result.Partial = result.Remaining > 0.000001
		}
	} else {
		bids := parseLevels(book.Bids)
		sort.Slice(bids, func(i, j int) bool { return bids[i].price > bids[j].price })

		var totalFilled, weightedPrice float64
		for _, level := range bids {
			if limitPrice > level.price {
				break
			}
			canFill := size - totalFilled
			if canFill <= 0 {
				break
			}
			fillAtLevel := canFill
			if fillAtLevel > level.size {
				fillAtLevel = level.size
			}
			weightedPrice += fillAtLevel * level.price
			totalFilled += fillAtLevel
		}

		if totalFilled > 0 {
			result.FillSize = totalFilled
			result.FillPrice = weightedPrice / totalFilled
			result.Remaining = size - totalFilled
			result.Partial = result.Remaining > 0.000001
		}
	}

	return result
}

type priceLevel struct {
	price float64
	size  float64
}

func parseLevels(rows []OrderBookRow) []priceLevel {
	levels := make([]priceLevel, 0, len(rows))
	for _, row := range rows {
		p, err := strconv.ParseFloat(row.Price, 64)
		if err != nil {
			continue
		}
		s, err := strconv.ParseFloat(row.Size, 64)
		if err != nil {
			continue
		}
		levels = append(levels, priceLevel{price: p, size: s})
	}
	return levels
}
```

- [ ] **Step 4: Fix the test file import**

The test file uses `fmt.Sprintf` — add the import at the top of `orderbook_test.go`:

```go
package trading

import (
	"fmt"
	"testing"
)
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./internal/trading/... -run TestMatchOrder -v
```

Expected output: 6 tests, all PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/trading/orderbook.go internal/trading/orderbook_test.go
git commit -m "feat: add orderbook client and pure MatchOrder function"
```

---

## Task 3: Repository — Add Missing Methods

**Files:**
- Modify: `internal/trading/repository.go`

- [ ] **Step 1: Add `GetAllLiveOrders`**

Append to `repository.go` after the `GetAllOrdersByUserID` function:

```go
// GetAllLiveOrders returns all LIVE orders across all users, used by the matching worker.
func (r *Repository) GetAllLiveOrders(ctx context.Context) ([]*models.Order, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, user_id, salt, maker, signer, taker, token_id,
			maker_amount, taker_amount, side, expiration, nonce, fee_rate_bps,
			signature_type, signature, price, original_size, size_matched, status,
			order_type, post_only, owner, market, asset_id, outcome,
			created_at, updated_at
		 FROM orders WHERE status = 'LIVE'
		 ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("get all live orders: %w", err)
	}
	defer rows.Close()
	return r.scanOrders(rows)
}
```

- [ ] **Step 2: Add `GetOrderByIDForUpdate`**

Append to `repository.go`:

```go
// GetOrderByIDForUpdate fetches an order with a FOR UPDATE lock inside a transaction.
// Used by the matching worker to prevent double-fills when ticks overlap.
func (r *Repository) GetOrderByIDForUpdate(ctx context.Context, tx pgx.Tx, orderID string) (*models.Order, error) {
	return r.scanOrder(tx.QueryRow(ctx,
		`SELECT id, user_id, salt, maker, signer, taker, token_id,
			maker_amount, taker_amount, side, expiration, nonce, fee_rate_bps,
			signature_type, signature, price, original_size, size_matched, status,
			order_type, post_only, owner, market, asset_id, outcome,
			created_at, updated_at
		 FROM orders WHERE id = $1 FOR UPDATE`, orderID))
}
```

- [ ] **Step 3: Add `UpdateOrderFill`**

Append to `repository.go`:

```go
// UpdateOrderFill updates an order's size_matched and status after a worker fill.
func (r *Repository) UpdateOrderFill(ctx context.Context, tx pgx.Tx, orderID, sizeMatched, status string) error {
	ct, err := tx.Exec(ctx,
		`UPDATE orders SET size_matched = $2::numeric, status = $3, updated_at = now()
		 WHERE id = $1`,
		orderID, sizeMatched, status)
	if err != nil {
		return fmt.Errorf("update order fill: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("order %s not found", orderID)
	}
	return nil
}
```

- [ ] **Step 4: Add `CancelLiveOrdersByTokenID`**

Append to `repository.go`:

```go
// CancelLiveOrdersByTokenID cancels all LIVE orders for a given token and refunds their reserved funds.
// Called by the worker when a 404 from the orderbook indicates the market is closed.
func (r *Repository) CancelLiveOrdersByTokenID(ctx context.Context, tokenID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin cancel tx: %w", err)
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx,
		`UPDATE orders SET status = 'CANCELED', updated_at = now()
		 WHERE token_id = $1 AND status = 'LIVE'
		 RETURNING id, user_id, side, original_size::float8, size_matched::float8, price::float8, token_id`,
		tokenID)
	if err != nil {
		return fmt.Errorf("cancel orders by token: %w", err)
	}

	type row struct {
		id, userID, side, tokenID    string
		origSize, matched, price float64
	}
	var rows2 []row
	for rows.Next() {
		var rr row
		if err := rows.Scan(&rr.id, &rr.userID, &rr.side, &rr.origSize, &rr.matched, &rr.price, &rr.tokenID); err != nil {
			rows.Close()
			return fmt.Errorf("scan cancel row: %w", err)
		}
		rows2 = append(rows2, rr)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate cancel rows: %w", err)
	}

	for _, rr := range rows2 {
		remaining := rr.origSize - rr.matched
		if err := refundOrderReservation(ctx, tx, rr.userID, rr.side, rr.tokenID, remaining, rr.price); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}
```

- [ ] **Step 5: Update `CreateTrade` to support optional `fill_key`**

Find `CreateTrade` in `repository.go`. Replace the INSERT query to include `fill_key`:

Old:
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
```

New (adds fill_key as nullable):
```go
func (r *Repository) CreateTrade(ctx context.Context, tx pgx.Tx, t *models.Trade) error {
	var matchTime, lastUpdate time.Time
	var fillKey interface{}
	if t.FillKey != "" {
		fillKey = t.FillKey
	}
	err := tx.QueryRow(ctx,
		`INSERT INTO trades (taker_order_id, user_id, market, asset_id, side, size,
			fee_rate_bps, price, status, outcome, owner, maker_address, fill_key)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		 RETURNING id, match_time, last_update`,
		t.TakerOrderID, t.UserID, t.Market, t.AssetID, t.Side, t.Size,
		t.FeeRateBps, t.Price, t.Status, t.Outcome, t.Owner, t.MakerAddress, fillKey,
	).Scan(&t.ID, &matchTime, &lastUpdate)
```

- [ ] **Step 6: Add `FillKey` field to `models.Trade`**

In `internal/models/models.go`, find the `Trade` struct and add the field after `MakerOrders`:

```go
FillKey         string `json:"-"` // worker dedup key, not exposed via API
```

- [ ] **Step 7: Verify compilation**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 8: Commit**

```bash
git add internal/trading/repository.go internal/models/models.go
git commit -m "feat: add repository methods for live order matching and fill_key support"
```

---

## Task 4: Service — Rewrite `PlaceOrder` with Orderbook Matching

**Files:**
- Modify: `internal/trading/service.go`

- [ ] **Step 1: Update `Service` struct and constructor**

Replace the current `Service` struct and `NewService`:

```go
type Service struct {
	repo        *Repository
	bookClient  *OrderBookClient
	gammaURL    string
	httpClient  *http.Client
}

func NewService(repo *Repository, bookClient *OrderBookClient, gammaURL string) *Service {
	return &Service{
		repo:        repo,
		bookClient:  bookClient,
		gammaURL:    gammaURL,
		httpClient:  &http.Client{Timeout: 15 * time.Second},
	}
}
```

- [ ] **Step 2: Add `determineInitialFill` helper**

Add this function above `PlaceOrder` in `service.go`:

```go
// determineInitialFill returns the order's initial status, the size to fill now, and the fill price.
// matchResult is nil when the orderbook was unavailable (transient error).
func determineInitialFill(orderType string, size float64, matchResult *MatchResult) (status string, fillSize float64, fillPrice float64) {
	if matchResult == nil || matchResult.FillSize < 0.000001 {
		switch orderType {
		case "FOK", "FAK":
			return "CANCELED", 0, 0
		default:
			return "LIVE", 0, 0
		}
	}

	if matchResult.FillSize >= size-0.000001 {
		// Fully filled
		return "MATCHED", matchResult.FillSize, matchResult.FillPrice
	}

	// Partially filled
	switch orderType {
	case "FOK":
		return "CANCELED", 0, 0
	case "FAK":
		return "CANCELED", matchResult.FillSize, matchResult.FillPrice
	default: // GTC, GTD
		return "LIVE", matchResult.FillSize, matchResult.FillPrice
	}
}
```

- [ ] **Step 3: Rewrite `PlaceOrder`**

Replace the current `PlaceOrder` function entirely:

```go
// PlaceOrder validates the order, checks the live Polymarket orderbook for an immediate fill,
// and persists everything in a single transaction.
// If the orderbook is unavailable, the order is created as LIVE for the worker to retry.
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

	// Fetch live orderbook; a 404 means the market is closed — reject immediately.
	// Any other error is transient — fall back to LIVE so the worker can retry.
	var matchResult *MatchResult
	book, bookErr := s.bookClient.FetchOrderBook(signed.TokenID)
	if bookErr != nil {
		if errors.Is(bookErr, ErrOrderBookNotFound) {
			return nil, fmt.Errorf("market is closed or does not exist")
		}
		slog.Warn("orderbook unavailable, order will stay LIVE", "token_id", signed.TokenID, "error", bookErr)
	} else {
		matchResult = MatchOrder(book, signed.Side, price, size)
	}

	initialStatus, fillSize, fillPrice := determineInitialFill(req.OrderType, size, matchResult)

	// FOK: never create the order if it can't fully fill
	if req.OrderType == "FOK" && initialStatus == "CANCELED" {
		return &models.OrderResponse{
			Success:  false,
			ErrorMsg: "FOK order could not be fully filled",
			Status:   "CANCELED",
		}, nil
	}

	priceStr := fmt.Sprintf("%.4f", price)
	sizeStr := fmt.Sprintf("%.6f", size)
	sizeMatchedStr := "0"
	if fillSize > 0 {
		sizeMatchedStr = fmt.Sprintf("%.6f", fillSize)
	}

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
		SizeMatched:   sizeMatchedStr,
		Status:        initialStatus,
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

	// 1. Reserve funds at limit price
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

	// 2. Create the order
	if err := s.repo.CreateOrderTx(ctx, tx, order); err != nil {
		return nil, fmt.Errorf("create order: %w", err)
	}

	// 3. Apply fill if any (creates trade, credits assets, updates position)
	if fillSize > 0 {
		if err := s.applyFill(ctx, tx, order, fillSize, fillPrice); err != nil {
			return nil, fmt.Errorf("apply fill: %w", err)
		}
	}

	// 4. FAK: refund the unfilled portion (fill happened or not)
	if req.OrderType == "FAK" && initialStatus == "CANCELED" {
		remaining := size - fillSize
		if err := refundOrderReservation(ctx, tx, userID, order.Side, signed.TokenID, remaining, price); err != nil {
			return nil, fmt.Errorf("refund FAK remainder: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return &models.OrderResponse{
		Success:            true,
		OrderID:            order.ID,
		TransactionsHashes: []string{},
		Status:             initialStatus,
		TakingAmount:       order.TakerAmount,
		MakingAmount:       order.MakerAmount,
	}, nil
}
```

- [ ] **Step 4: Add `applyFill` helper**

Add this method after `PlaceOrder`:

```go
// applyFill creates a trade record and updates wallets and position for a fill.
// Used by PlaceOrder for immediate fills. The order must already have been created.
func (s *Service) applyFill(ctx context.Context, tx pgx.Tx, order *models.Order, fillSize, fillPrice float64) error {
	limitPrice, _ := strconv.ParseFloat(order.Price, 64)
	fillSizeStr := fmt.Sprintf("%.6f", fillSize)
	fillPriceStr := fmt.Sprintf("%.4f", fillPrice)

	trade := &models.Trade{
		TakerOrderID: order.ID,
		UserID:       order.UserID,
		Market:       order.Market,
		AssetID:      order.AssetID,
		Side:         order.Side,
		Size:         fillSizeStr,
		FeeRateBps:   order.FeeRateBps,
		Price:        fillPriceStr,
		Status:       "MATCHED",
		Outcome:      order.Outcome,
		Owner:        order.Owner,
		MakerAddress: order.Maker,
	}
	if err := s.repo.CreateTrade(ctx, tx, trade); err != nil {
		return fmt.Errorf("create trade: %w", err)
	}

	if order.Side == "BUY" {
		excess := (limitPrice - fillPrice) * fillSize
		if excess > 0.000001 {
			if err := s.repo.CreditWallet(ctx, tx, order.UserID, "COLLATERAL", "", fmt.Sprintf("%.6f", excess)); err != nil {
				return fmt.Errorf("refund excess collateral: %w", err)
			}
		}
		if err := s.repo.CreditWallet(ctx, tx, order.UserID, "CONDITIONAL", order.TokenID, fillSizeStr); err != nil {
			return fmt.Errorf("credit conditional: %w", err)
		}
	} else {
		costStr := fmt.Sprintf("%.6f", fillSize*fillPrice)
		if err := s.repo.CreditWallet(ctx, tx, order.UserID, "COLLATERAL", "", costStr); err != nil {
			return fmt.Errorf("credit collateral: %w", err)
		}
	}

	return s.updatePosition(ctx, tx, order.UserID, order.TokenID, order.Market, order.Outcome, order.Side, fillSize, fillPrice)
}

// updatePosition adjusts the user's position after a fill.
func (s *Service) updatePosition(ctx context.Context, tx pgx.Tx, userID, tokenID, market, outcome, side string, fillSize, fillPrice float64) error {
	pos, err := s.repo.GetPositionForUpdate(ctx, tx, userID, tokenID)
	if err != nil {
		return fmt.Errorf("get position: %w", err)
	}
	if side == "BUY" {
		var newSize, newAvg float64
		if pos != nil {
			existingSize, _ := strconv.ParseFloat(pos.Size, 64)
			existingAvg, _ := strconv.ParseFloat(pos.AvgPrice, 64)
			newSize = existingSize + fillSize
			newAvg = (existingSize*existingAvg + fillSize*fillPrice) / newSize
		} else {
			newSize = fillSize
			newAvg = fillPrice
		}
		return s.repo.UpsertPosition(ctx, tx, userID, tokenID, market, outcome,
			fmt.Sprintf("%.6f", newSize), fmt.Sprintf("%.4f", newAvg))
	}
	if pos != nil {
		existingSize, _ := strconv.ParseFloat(pos.Size, 64)
		newSize := existingSize - fillSize
		if newSize < 0.000001 {
			newSize = 0
		}
		return s.repo.UpsertPosition(ctx, tx, userID, tokenID, market, outcome,
			fmt.Sprintf("%.6f", newSize), pos.AvgPrice)
	}
	return nil
}
```

- [ ] **Step 5: Add `errors` import to `service.go`**

In the imports block of `service.go`, add `"errors"`.

- [ ] **Step 6: Add `executeFill` method (used by worker)**

Add this method after `applyFill`:

```go
// executeFill is called by the matching worker to fill a LIVE order.
// It locks the order row, verifies it is still LIVE, then applies the fill atomically.
func (s *Service) executeFill(ctx context.Context, order *models.Order, result *MatchResult) error {
	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Lock the order to prevent double-fill from overlapping worker ticks
	locked, err := s.repo.GetOrderByIDForUpdate(ctx, tx, order.ID)
	if err != nil {
		return fmt.Errorf("lock order: %w", err)
	}
	if locked == nil || locked.Status != "LIVE" {
		return nil // already filled or canceled, nothing to do
	}

	currentMatched, _ := strconv.ParseFloat(locked.SizeMatched, 64)
	originalSize, _ := strconv.ParseFloat(locked.OriginalSize, 64)
	remaining := originalSize - currentMatched

	fillSize := result.FillSize
	if fillSize > remaining {
		fillSize = remaining
	}
	if fillSize < 0.000001 {
		return nil
	}

	fillPrice := result.FillPrice
	newMatched := currentMatched + fillSize
	newStatus := "LIVE"
	if newMatched >= originalSize-0.000001 {
		newStatus = "MATCHED"
	}

	fillSizeStr := fmt.Sprintf("%.6f", fillSize)
	fillPriceStr := fmt.Sprintf("%.4f", fillPrice)
	fillKey := fmt.Sprintf("%s-%.6f-%.4f", order.ID, fillSize, fillPrice)

	trade := &models.Trade{
		TakerOrderID: order.ID,
		UserID:       order.UserID,
		Market:       order.Market,
		AssetID:      order.AssetID,
		Side:         order.Side,
		Size:         fillSizeStr,
		FeeRateBps:   order.FeeRateBps,
		Price:        fillPriceStr,
		Status:       "MATCHED",
		Outcome:      order.Outcome,
		Owner:        order.Owner,
		MakerAddress: order.Maker,
		FillKey:      fillKey,
	}
	if err := s.repo.CreateTrade(ctx, tx, trade); err != nil {
		return fmt.Errorf("create worker trade: %w", err)
	}

	if err := s.repo.UpdateOrderFill(ctx, tx, order.ID, fmt.Sprintf("%.6f", newMatched), newStatus); err != nil {
		return fmt.Errorf("update order fill: %w", err)
	}

	limitPrice, _ := strconv.ParseFloat(locked.Price, 64)
	if locked.Side == "BUY" {
		excess := (limitPrice - fillPrice) * fillSize
		if excess > 0.000001 {
			if err := s.repo.CreditWallet(ctx, tx, locked.UserID, "COLLATERAL", "", fmt.Sprintf("%.6f", excess)); err != nil {
				return fmt.Errorf("refund excess: %w", err)
			}
		}
		if err := s.repo.CreditWallet(ctx, tx, locked.UserID, "CONDITIONAL", locked.TokenID, fillSizeStr); err != nil {
			return fmt.Errorf("credit conditional: %w", err)
		}
	} else {
		costStr := fmt.Sprintf("%.6f", fillSize*fillPrice)
		if err := s.repo.CreditWallet(ctx, tx, locked.UserID, "COLLATERAL", "", costStr); err != nil {
			return fmt.Errorf("credit collateral: %w", err)
		}
	}

	if err := s.updatePosition(ctx, tx, locked.UserID, locked.TokenID, locked.Market, locked.Outcome, locked.Side, fillSize, fillPrice); err != nil {
		return fmt.Errorf("update position: %w", err)
	}

	return tx.Commit(ctx)
}
```

- [ ] **Step 7: Verify compilation**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 8: Run existing tests**

```bash
go test ./internal/trading/... -v
```

Expected: `TestDeriveOrderPriceAndSize_*` and `TestMatchOrder_*` all PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/trading/service.go
git commit -m "feat: rewrite PlaceOrder to match against live orderbook in single transaction"
```

---

## Task 5: Worker — Background Matching Loop

**Files:**
- Create: `internal/trading/worker.go`

- [ ] **Step 1: Create `worker.go`**

```go
package trading

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// Worker periodically tries to fill all LIVE orders against the live Polymarket orderbook.
// It ticks every second, groups LIVE orders by token_id to minimise HTTP calls
// (one FetchOrderBook per unique token, not per order), and cancels orders when
// their market is no longer found (404 = resolved or closed).
type Worker struct {
	repo        *Repository
	bookClient  *OrderBookClient
}

// NewWorker creates a matching worker.
func NewWorker(repo *Repository, bookClient *OrderBookClient) *Worker {
	return &Worker{repo: repo, bookClient: bookClient}
}

// Start launches the worker loop. It stops when ctx is cancelled.
func (w *Worker) Start(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	slog.Info("matching worker started", "interval", interval)
	for {
		select {
		case <-ctx.Done():
			slog.Info("matching worker stopped")
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	orders, err := w.repo.GetAllLiveOrders(ctx)
	if err != nil {
		slog.Error("worker: get live orders failed", "error", err)
		return
	}
	if len(orders) == 0 {
		return
	}

	// Group by token_id — one FetchOrderBook call per unique token
	byToken := make(map[string][]*Order)
	for _, o := range orders {
		byToken[o.TokenID] = append(byToken[o.TokenID], o)
	}

	for tokenID, tokenOrders := range byToken {
		book, err := w.bookClient.FetchOrderBook(tokenID)
		if err != nil {
			if errors.Is(err, ErrOrderBookNotFound) {
				// Market is closed — cancel all LIVE orders for this token
				slog.Info("worker: market closed, canceling LIVE orders", "token_id", tokenID)
				if cancelErr := w.repo.CancelLiveOrdersByTokenID(ctx, tokenID); cancelErr != nil {
					slog.Error("worker: cancel by token failed", "token_id", tokenID, "error", cancelErr)
				}
			} else {
				slog.Warn("worker: orderbook fetch failed, will retry next tick", "token_id", tokenID, "error", err)
			}
			continue
		}

		for _, order := range tokenOrders {
			limitPrice, _ := strconv.ParseFloat(order.Price, 64)
			originalSize, _ := strconv.ParseFloat(order.OriginalSize, 64)
			currentMatched, _ := strconv.ParseFloat(order.SizeMatched, 64)
			remaining := originalSize - currentMatched

			if remaining < 0.000001 {
				continue
			}

			result := MatchOrder(book, order.Side, limitPrice, remaining)
			if result.FillSize < 0.000001 {
				continue // nothing to fill at current prices
			}

			svc := &Service{repo: w.repo, bookClient: w.bookClient}
			if err := svc.executeFill(ctx, order, result); err != nil {
				slog.Error("worker: executeFill failed", "order_id", order.ID, "error", err)
			} else {
				slog.Info("worker: filled order", "order_id", order.ID,
					"fill_size", result.FillSize, "fill_price", result.FillPrice)
			}
		}
	}
}
```

- [ ] **Step 2: Fix the import — add `strconv`**

At the top of `worker.go`, use this import block:

```go
import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"
)
```

- [ ] **Step 3: Fix the `Order` type reference in worker**

The worker references `*Order` but the type is `*models.Order`. Update the `byToken` map and the function signatures:

Replace:
```go
byToken := make(map[string][]*Order)
for _, o := range orders {
    byToken[o.TokenID] = append(byToken[o.TokenID], o)
}

for tokenID, tokenOrders := range byToken {
    ...
    for _, order := range tokenOrders {
```

The `orders` variable is `[]*models.Order` from `GetAllLiveOrders`. Add the import and use:

```go
import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/tonikpro/poly-paper-api/internal/models"
)
```

And change `[]*Order` to `[]*models.Order`.

- [ ] **Step 4: Remove the inline `&Service{}` construction — pass service instead**

The worker calling `svc.executeFill` by constructing a Service inline is wrong. Instead, give the worker a reference to the Service. Update `Worker`:

```go
type Worker struct {
	svc        *Service
	bookClient *OrderBookClient
}

func NewWorker(svc *Service, bookClient *OrderBookClient) *Worker {
	return &Worker{svc: svc, bookClient: bookClient}
}
```

And in `tick`, replace `svc := &Service{...}; svc.executeFill(...)` with `w.svc.executeFill(...)`.

Also update the `repo` reference in `tick` to use `w.svc.repo`:

```go
orders, err := w.svc.repo.GetAllLiveOrders(ctx)
...
if cancelErr := w.svc.repo.CancelLiveOrdersByTokenID(ctx, tokenID); cancelErr != nil {
```

Final correct `worker.go`:

```go
package trading

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/tonikpro/poly-paper-api/internal/models"
)

// Worker periodically tries to fill all LIVE orders against the live Polymarket orderbook.
type Worker struct {
	svc        *Service
	bookClient *OrderBookClient
}

// NewWorker creates a matching worker.
func NewWorker(svc *Service, bookClient *OrderBookClient) *Worker {
	return &Worker{svc: svc, bookClient: bookClient}
}

// Start launches the worker loop. It stops when ctx is cancelled.
func (w *Worker) Start(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	slog.Info("matching worker started", "interval", interval)
	for {
		select {
		case <-ctx.Done():
			slog.Info("matching worker stopped")
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	orders, err := w.svc.repo.GetAllLiveOrders(ctx)
	if err != nil {
		slog.Error("worker: get live orders failed", "error", err)
		return
	}
	if len(orders) == 0 {
		return
	}

	// Group by token_id — one FetchOrderBook per unique token
	byToken := make(map[string][]*models.Order)
	for _, o := range orders {
		byToken[o.TokenID] = append(byToken[o.TokenID], o)
	}

	for tokenID, tokenOrders := range byToken {
		book, err := w.bookClient.FetchOrderBook(tokenID)
		if err != nil {
			if errors.Is(err, ErrOrderBookNotFound) {
				slog.Info("worker: market closed, canceling LIVE orders", "token_id", tokenID)
				if cancelErr := w.svc.repo.CancelLiveOrdersByTokenID(ctx, tokenID); cancelErr != nil {
					slog.Error("worker: cancel by token failed", "token_id", tokenID, "error", cancelErr)
				}
			} else {
				slog.Warn("worker: orderbook fetch failed, retrying next tick", "token_id", tokenID, "error", err)
			}
			continue
		}

		for _, order := range tokenOrders {
			limitPrice, _ := strconv.ParseFloat(order.Price, 64)
			originalSize, _ := strconv.ParseFloat(order.OriginalSize, 64)
			currentMatched, _ := strconv.ParseFloat(order.SizeMatched, 64)
			remaining := originalSize - currentMatched
			if remaining < 0.000001 {
				continue
			}

			result := MatchOrder(book, order.Side, limitPrice, remaining)
			if result.FillSize < 0.000001 {
				continue
			}

			if err := w.svc.executeFill(ctx, order, result); err != nil {
				slog.Error("worker: executeFill failed", "order_id", order.ID, "error", err)
			} else {
				slog.Info("worker: filled order", "order_id", order.ID,
					"fill_size", result.FillSize, "fill_price", result.FillPrice)
			}
		}
	}
}
```

- [ ] **Step 5: Verify compilation**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add internal/trading/worker.go
git commit -m "feat: add background matching worker, ticks every 1s"
```

---

## Task 6: Wire Everything in `main.go`

**Files:**
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Update service construction and start worker**

In `cmd/server/main.go`, find this block:

```go
tradingRepo := trading.NewRepository(pool)
tradingSvc := trading.NewService(tradingRepo, cfg.PolymarketGammaURL)
```

Replace with:

```go
tradingRepo := trading.NewRepository(pool)
bookClient := trading.NewOrderBookClient(cfg.PolymarketCLOBURL)
tradingSvc := trading.NewService(tradingRepo, bookClient, cfg.PolymarketGammaURL)
matchingWorker := trading.NewWorker(tradingSvc, bookClient)
```

- [ ] **Step 2: Start the worker alongside the sync poller**

Find:

```go
poller.Start(ctx, 60*time.Second)
```

Add the worker start immediately after:

```go
poller.Start(ctx, 60*time.Second)
go matchingWorker.Start(ctx, 1*time.Second)
```

- [ ] **Step 3: Build and run**

```bash
go build ./... && make run
```

Expected: server starts, logs show "matching worker started interval=1s".

- [ ] **Step 4: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat: wire OrderBookClient and matching worker into server"
```

---

## Task 7: Dashboard — `Orders.tsx` (LIVE Orders Page)

**Files:**
- Create: `dashboard/src/pages/Orders.tsx`

- [ ] **Step 1: Add `cancelOrder` to the API client**

In `dashboard/src/api/client.ts`, append:

```ts
export const cancelOrder = (orderId: string) =>
  api.delete(`/clob/order`, { data: { orderID: orderId } });
```

- [ ] **Step 2: Create `Orders.tsx`**

```tsx
import { useEffect, useState } from 'react';
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
}

export default function Orders() {
  const [orders, setOrders] = useState<Order[]>([]);
  const [loading, setLoading] = useState(true);
  const [canceling, setCanceling] = useState<string | null>(null);

  const load = () => {
    setLoading(true);
    getOrders({ status: 'LIVE', limit: 100 })
      .then(r => setOrders(r.data.orders || []))
      .catch(() => {})
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    load();
    const id = setInterval(load, 3000);
    return () => clearInterval(id);
  }, []);

  const handleCancel = async (orderId: string) => {
    setCanceling(orderId);
    try {
      await cancelOrder(orderId);
      setOrders(prev => prev.filter(o => o.id !== orderId));
    } catch {
      // ignore
    } finally {
      setCanceling(null);
    }
  };

  if (loading && orders.length === 0) return <div className="text-center py-8">Loading...</div>;

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Open Orders</h1>
      {orders.length === 0 ? (
        <div className="bg-white rounded-lg shadow p-8 text-center text-gray-500">
          No open orders. Place orders using your bot.
        </div>
      ) : (
        <div className="bg-white rounded-lg shadow overflow-x-auto">
          <table className="min-w-full divide-y divide-gray-200">
            <thead className="bg-gray-50">
              <tr>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Side</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Outcome</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Price</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Filled / Total</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Type</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Created</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Action</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-200">
              {orders.map(o => (
                <tr key={o.id} className="hover:bg-gray-50">
                  <td className={`px-4 py-3 font-medium ${o.side === 'BUY' ? 'text-green-600' : 'text-red-600'}`}>
                    {o.side}
                  </td>
                  <td className="px-4 py-3 text-sm">{o.outcome || '—'}</td>
                  <td className="px-4 py-3">{o.price}</td>
                  <td className="px-4 py-3 text-sm">
                    {parseFloat(o.size_matched).toFixed(2)} / {parseFloat(o.original_size).toFixed(2)}
                  </td>
                  <td className="px-4 py-3 text-sm text-gray-500">{o.order_type || 'GTC'}</td>
                  <td className="px-4 py-3 text-xs text-gray-500">
                    {new Date(o.created_at).toLocaleString()}
                  </td>
                  <td className="px-4 py-3">
                    <button
                      onClick={() => handleCancel(o.id)}
                      disabled={canceling === o.id}
                      className="text-xs text-red-600 hover:text-red-800 disabled:opacity-40"
                    >
                      {canceling === o.id ? 'Canceling…' : 'Cancel'}
                    </button>
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
```

- [ ] **Step 3: Commit**

```bash
git add dashboard/src/pages/Orders.tsx dashboard/src/api/client.ts
git commit -m "feat: add Orders page showing LIVE orders with cancel button"
```

---

## Task 8: Dashboard — Wire Routes and Remove Stale Metric

**Files:**
- Modify: `dashboard/src/App.tsx`
- Modify: `dashboard/src/components/Layout.tsx`
- Modify: `dashboard/src/pages/Dashboard.tsx`
- Modify: `internal/auth/dashboard_queries.go`

- [ ] **Step 1: Add `outcome` to `GetOrders` query in `dashboard_queries.go`**

In `internal/auth/dashboard_queries.go`, find `GetOrders` and update the SELECT:

Old:
```go
dataQuery := `SELECT id, token_id, side, price::text, original_size::text, size_matched::text, status, order_type, created_at
     FROM orders WHERE user_id = $1`
```

New:
```go
dataQuery := `SELECT id, token_id, side, price::text, original_size::text, size_matched::text, status, order_type, outcome, created_at
     FROM orders WHERE user_id = $1`
```

And update the scan in the rows loop from:
```go
if err := rows.Scan(&id, &tokenID, &side, &price, &origSize, &sizeMatched, &st, &orderType, &createdAt); err != nil {
```
to:
```go
var outcome string
if err := rows.Scan(&id, &tokenID, &side, &price, &origSize, &sizeMatched, &st, &orderType, &outcome, &createdAt); err != nil {
```

And add `"outcome": outcome` to the map:
```go
orders = append(orders, map[string]any{
    "id": id, "token_id": tokenID, "side": side, "price": price,
    "original_size": origSize, "size_matched": sizeMatched,
    "status": st, "order_type": orderType, "outcome": outcome, "created_at": createdAt,
})
```

- [ ] **Step 3: Add route in `App.tsx`**

In `dashboard/src/App.tsx`, add the import:

```tsx
import Orders from './pages/Orders';
```

Add the route inside the protected layout routes (after the `history` route):

```tsx
<Route path="orders" element={<Orders />} />
```

- [ ] **Step 4: Add nav link in `Layout.tsx`**

In `dashboard/src/components/Layout.tsx`, add after the History link:

```tsx
<Link to="/orders" className="text-gray-600 hover:text-gray-900">Orders</Link>
```

- [ ] **Step 5: Remove `Open Orders` card from `Dashboard.tsx`**

In `dashboard/src/pages/Dashboard.tsx`:

Remove the state variable:
```tsx
const [orderCount, setOrderCount] = useState(0);
```

Remove the fetch call:
```tsx
getOrders({ limit: 1 }).then(r => setOrderCount(r.data.total || 0)).catch(() => {});
```

Remove the card:
```tsx
<Card title="Open Orders" value={String(orderCount)} />
```

- [ ] **Step 6: Build the dashboard**

```bash
make dev-dashboard
# Open http://localhost:5173 in browser
# Verify: Dashboard has no "Open Orders" card
# Verify: "Orders" link appears in nav
# Verify: /orders page loads (empty if no LIVE orders)
# Ctrl-C
```

- [ ] **Step 7: Commit**

```bash
git add dashboard/src/App.tsx dashboard/src/components/Layout.tsx dashboard/src/pages/Dashboard.tsx internal/auth/dashboard_queries.go
git commit -m "feat: wire Orders route, add nav link, remove stale Open Orders metric, add outcome to orders API"
```

---

## Task 9: Full Integration Verification

- [ ] **Step 1: Run all Go tests**

```bash
make test
```

Expected: all tests PASS including `TestMatchOrder_*` and `TestDeriveOrderPriceAndSize_*`.

- [ ] **Step 2: Start the full server**

```bash
make docker-up && make run
```

Expected: logs show:
- `migrations complete`
- `matching worker started interval=1s`
- `server starting addr=:8080`

- [ ] **Step 3: Verify POST /order creates LIVE order when orderbook unavailable**

Temporarily point `POLYMARKET_CLOB_URL` to a broken endpoint to confirm fallback:

```bash
POLYMARKET_CLOB_URL=http://127.0.0.1:9999 make run
```

Place an order via the bot — it should succeed with `status: "LIVE"` in the response.

- [ ] **Step 4: Final commit**

```bash
git add -A
git commit -m "feat: orderbook matching restore — live fills, LIVE fallback, worker, dashboard Orders page"
```
