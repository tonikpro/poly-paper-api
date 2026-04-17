package trading

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tonikpro/poly-paper-api/internal/models"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// --- Orders ---

func (r *Repository) CreateOrder(ctx context.Context, o *models.Order) error {
	return r.createOrderRow(r.pool.QueryRow(ctx, insertOrderSQL(o), insertOrderArgs(o)...), o)
}

func (r *Repository) CreateOrderTx(ctx context.Context, tx pgx.Tx, o *models.Order) error {
	return r.createOrderRow(tx.QueryRow(ctx, insertOrderSQL(o), insertOrderArgs(o)...), o)
}

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

func (r *Repository) createOrderRow(row pgx.Row, o *models.Order) error {
	if err := row.Scan(&o.ID, &o.CreatedAt, &o.UpdatedAt); err != nil {
		return fmt.Errorf("create order: %w", err)
	}
	return nil
}

func (r *Repository) GetOrderByID(ctx context.Context, orderID string) (*models.Order, error) {
	return r.scanOrder(r.pool.QueryRow(ctx,
		`SELECT id, user_id, salt, maker, signer, taker, token_id,
			maker_amount, taker_amount, side, expiration, nonce, fee_rate_bps,
			signature_type, signature, price, original_size, size_matched, status,
			order_type, post_only, owner, market, asset_id, outcome,
			created_at, updated_at
		 FROM orders WHERE id = $1`, orderID))
}

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

func (r *Repository) CancelOrder(ctx context.Context, orderID, userID string) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin cancel tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var side, tokenID string
	var origSize, sizeMatched, price float64
	err = tx.QueryRow(ctx,
		`UPDATE orders SET status = 'CANCELED', updated_at = now()
		 WHERE id = $1 AND user_id = $2 AND status = 'LIVE'
		 RETURNING side, original_size::float8, size_matched::float8, price::float8, token_id`,
		orderID, userID,
	).Scan(&side, &origSize, &sizeMatched, &price, &tokenID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("cancel order: %w", err)
	}

	if err := refundOrderReservation(ctx, tx, userID, side, tokenID, origSize-sizeMatched, price); err != nil {
		return false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit cancel: %w", err)
	}
	return true, nil
}

func (r *Repository) CancelOrdersByFilter(ctx context.Context, userID string, market, assetID *string) ([]string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin cancel tx: %w", err)
	}
	defer tx.Rollback(ctx)

	query := `UPDATE orders SET status = 'CANCELED', updated_at = now()
		WHERE user_id = $1 AND status = 'LIVE'`
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
	query += " RETURNING id, side, original_size::float8, size_matched::float8, price::float8, token_id"

	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("cancel filtered orders: %w", err)
	}

	type cancelInfo struct {
		id, side, tokenID    string
		origSize, matched, price float64
	}
	var infos []cancelInfo
	var canceled []string
	for rows.Next() {
		var ci cancelInfo
		if err := rows.Scan(&ci.id, &ci.side, &ci.origSize, &ci.matched, &ci.price, &ci.tokenID); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan canceled order: %w", err)
		}
		canceled = append(canceled, ci.id)
		infos = append(infos, ci)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate canceled orders: %w", err)
	}

	for _, ci := range infos {
		if err := refundOrderReservation(ctx, tx, userID, ci.side, ci.tokenID, ci.origSize-ci.matched, ci.price); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit cancel: %w", err)
	}
	return canceled, nil
}

// refundOrderReservation credits back the reserved funds for the unfilled portion of a canceled order.
func refundOrderReservation(ctx context.Context, tx pgx.Tx, userID, side, tokenID string, remaining, price float64) error {
	if remaining < 0.000001 {
		return nil
	}
	if side == "BUY" {
		refund := fmt.Sprintf("%.6f", remaining*price)
		_, err := tx.Exec(ctx,
			`UPDATE wallets SET balance = balance + $2::numeric, allowance = allowance + $2::numeric, updated_at = now()
			 WHERE user_id = $1 AND asset_type = 'COLLATERAL' AND token_id = ''`,
			userID, refund)
		if err != nil {
			return fmt.Errorf("refund collateral: %w", err)
		}
	} else {
		refund := fmt.Sprintf("%.6f", remaining)
		_, err := tx.Exec(ctx,
			`INSERT INTO wallets (user_id, asset_type, token_id, balance, allowance)
			 VALUES ($1, 'CONDITIONAL', $2, $3::numeric, $3::numeric)
			 ON CONFLICT (user_id, asset_type, token_id) DO UPDATE SET
				balance = wallets.balance + $3::numeric,
				allowance = wallets.allowance + $3::numeric,
				updated_at = now()`,
			userID, tokenID, refund)
		if err != nil {
			return fmt.Errorf("refund conditional: %w", err)
		}
	}
	return nil
}

// --- Trades ---

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

// --- Positions ---

func (r *Repository) UpsertPosition(ctx context.Context, tx pgx.Tx, userID, tokenID, marketID, outcome, size, avgPrice, realizedPnlDelta string) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO positions (user_id, token_id, market_id, outcome, size, avg_price, realized_pnl)
		 VALUES ($1, $2, $3, $4, $5, $6, $7::numeric)
		 ON CONFLICT (user_id, token_id) DO UPDATE SET
			size = EXCLUDED.size,
			avg_price = EXCLUDED.avg_price,
			realized_pnl = positions.realized_pnl + $7::numeric,
			updated_at = now()`,
		userID, tokenID, marketID, outcome, size, avgPrice, realizedPnlDelta)
	return err
}

func (r *Repository) GetPosition(ctx context.Context, userID, tokenID string) (*models.Position, error) {
	p := &models.Position{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, user_id, token_id, market_id, outcome, size, avg_price, realized_pnl, created_at, updated_at
		 FROM positions WHERE user_id = $1 AND token_id = $2`,
		userID, tokenID,
	).Scan(&p.ID, &p.UserID, &p.TokenID, &p.MarketID, &p.Outcome, &p.Size, &p.AvgPrice, &p.RealizedPnl, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get position: %w", err)
	}
	return p, nil
}

func (r *Repository) GetPositionForUpdate(ctx context.Context, tx pgx.Tx, userID, tokenID string) (*models.Position, error) {
	p := &models.Position{}
	err := tx.QueryRow(ctx,
		`SELECT id, user_id, token_id, market_id, outcome, size, avg_price, realized_pnl, created_at, updated_at
		 FROM positions WHERE user_id = $1 AND token_id = $2 FOR UPDATE`,
		userID, tokenID,
	).Scan(&p.ID, &p.UserID, &p.TokenID, &p.MarketID, &p.Outcome, &p.Size, &p.AvgPrice, &p.RealizedPnl, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get position for update: %w", err)
	}
	return p, nil
}

// --- Wallets ---

func (r *Repository) GetWallet(ctx context.Context, userID, assetType, tokenID string) (*models.Wallet, error) {
	w := &models.Wallet{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, user_id, asset_type, token_id, balance, allowance, created_at, updated_at
		 FROM wallets WHERE user_id = $1 AND asset_type = $2 AND token_id = $3`,
		userID, assetType, tokenID,
	).Scan(&w.ID, &w.UserID, &w.AssetType, &w.TokenID, &w.Balance, &w.Allowance, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get wallet: %w", err)
	}
	return w, nil
}

func (r *Repository) DebitWallet(ctx context.Context, tx pgx.Tx, userID, assetType, tokenID, amount string) error {
	ct, err := tx.Exec(ctx,
		`UPDATE wallets SET balance = balance - $4::numeric, allowance = allowance - $4::numeric, updated_at = now()
		 WHERE user_id = $1 AND asset_type = $2 AND token_id = $3 AND balance >= $4::numeric`,
		userID, assetType, tokenID, amount)
	if err != nil {
		return fmt.Errorf("debit wallet: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("insufficient balance")
	}
	return nil
}

func (r *Repository) CreditWallet(ctx context.Context, tx pgx.Tx, userID, assetType, tokenID, amount string) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO wallets (user_id, asset_type, token_id, balance, allowance)
		 VALUES ($1, $2, $3, $4::numeric, $4::numeric)
		 ON CONFLICT (user_id, asset_type, token_id) DO UPDATE SET
			balance = wallets.balance + $4::numeric,
			allowance = wallets.allowance + $4::numeric,
			updated_at = now()`,
		userID, assetType, tokenID, amount)
	return err
}

// --- Helpers ---

func (r *Repository) BeginTx(ctx context.Context) (pgx.Tx, error) {
	return r.pool.Begin(ctx)
}

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

// UpsertMarketAndTokens inserts or updates a market and its outcome tokens.
func (r *Repository) UpsertMarketAndTokens(ctx context.Context, market *models.Market, tokens []models.OutcomeToken) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO markets (id, condition_id, question, slug, active, closed, neg_risk, tick_size, min_order_size, synced_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now())
		 ON CONFLICT (id) DO UPDATE SET
			active = EXCLUDED.active,
			closed = EXCLUDED.closed,
			synced_at = now()`,
		market.ID, market.ConditionID, market.Question, market.Slug, market.Active, market.Closed, market.NegRisk,
		market.TickSize, market.MinOrderSize)
	if err != nil {
		return fmt.Errorf("upsert market: %w", err)
	}

	for _, t := range tokens {
		_, err := r.pool.Exec(ctx,
			`INSERT INTO outcome_tokens (token_id, market_id, outcome, winner)
			 VALUES ($1, $2, $3, $4)
			 ON CONFLICT (token_id) DO UPDATE SET
				winner = EXCLUDED.winner`,
			t.TokenID, t.MarketID, t.Outcome, t.Winner)
		if err != nil {
			return fmt.Errorf("upsert outcome token: %w", err)
		}
	}

	return nil
}

// GetOutcomeToken looks up a token to find its market and outcome
func (r *Repository) GetOutcomeToken(ctx context.Context, tokenID string) (*models.OutcomeToken, error) {
	t := &models.OutcomeToken{}
	err := r.pool.QueryRow(ctx,
		`SELECT token_id, market_id, outcome, winner FROM outcome_tokens WHERE token_id = $1`,
		tokenID,
	).Scan(&t.TokenID, &t.MarketID, &t.Outcome, &t.Winner)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get outcome token: %w", err)
	}
	return t, nil
}

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
		id, userID, side, tokenID string
		origSize, matched, price  float64
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
