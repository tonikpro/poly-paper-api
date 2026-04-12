package trading

import (
	"context"
	"fmt"

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
	// Pass NULL instead of empty string for outcome to satisfy CHECK constraint
	var outcome interface{} = o.Outcome
	if o.Outcome == "" {
		outcome = nil
	}

	err := r.pool.QueryRow(ctx,
		`INSERT INTO orders (user_id, salt, maker, signer, taker, token_id,
			maker_amount, taker_amount, side, expiration, nonce, fee_rate_bps,
			signature_type, signature, price, original_size, size_matched, status,
			order_type, post_only, owner, market, asset_id, outcome, associate_trades)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25)
		 RETURNING id, created_at, updated_at`,
		o.UserID, o.Salt, o.Maker, o.Signer, o.Taker, o.TokenID,
		o.MakerAmount, o.TakerAmount, o.Side, o.Expiration, o.Nonce, o.FeeRateBps,
		o.SignatureType, o.Signature, o.Price, o.OriginalSize, o.SizeMatched, o.Status,
		o.OrderType, o.PostOnly, o.Owner, o.Market, o.AssetID, outcome, "[]",
	).Scan(&o.ID, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create order: %w", err)
	}
	return nil
}

func (r *Repository) GetOrderByID(ctx context.Context, orderID string) (*models.Order, error) {
	return r.scanOrder(r.pool.QueryRow(ctx,
		`SELECT id, user_id, salt, maker, signer, taker, token_id,
			maker_amount, taker_amount, side, expiration, nonce, fee_rate_bps,
			signature_type, signature, price, original_size, size_matched, status,
			order_type, post_only, owner, market, asset_id, outcome, associate_trades,
			created_at, updated_at
		 FROM orders WHERE id = $1`, orderID))
}

func (r *Repository) GetOrderByIDForUpdate(ctx context.Context, tx pgx.Tx, orderID string) (*models.Order, error) {
	return r.scanOrder(tx.QueryRow(ctx,
		`SELECT id, user_id, salt, maker, signer, taker, token_id,
			maker_amount, taker_amount, side, expiration, nonce, fee_rate_bps,
			signature_type, signature, price, original_size, size_matched, status,
			order_type, post_only, owner, market, asset_id, outcome, associate_trades,
			created_at, updated_at
		 FROM orders WHERE id = $1 FOR UPDATE`, orderID))
}

func (r *Repository) GetLiveOrdersForUpdate(ctx context.Context, tx pgx.Tx) ([]*models.Order, error) {
	rows, err := tx.Query(ctx,
		`SELECT id, user_id, salt, maker, signer, taker, token_id,
			maker_amount, taker_amount, side, expiration, nonce, fee_rate_bps,
			signature_type, signature, price, original_size, size_matched, status,
			order_type, post_only, owner, market, asset_id, outcome, associate_trades,
			created_at, updated_at
		 FROM orders WHERE status = 'LIVE' FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return nil, fmt.Errorf("get live orders: %w", err)
	}
	defer rows.Close()
	return r.scanOrders(rows)
}

func (r *Repository) GetOrdersByUserID(ctx context.Context, userID string, market, assetID, cursor *string) ([]*models.Order, string, error) {
	query := `SELECT id, user_id, salt, maker, signer, taker, token_id,
			maker_amount, taker_amount, side, expiration, nonce, fee_rate_bps,
			signature_type, signature, price, original_size, size_matched, status,
			order_type, post_only, owner, market, asset_id, outcome, associate_trades,
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
		argIdx++
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
			order_type, post_only, owner, market, asset_id, outcome, associate_trades,
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
	var canceledID string
	err := r.pool.QueryRow(ctx,
		`UPDATE orders
		 SET status = 'CANCELED', updated_at = now()
		 WHERE id = $1 AND user_id = $2 AND status = 'LIVE'
		 RETURNING id`,
		orderID, userID,
	).Scan(&canceledID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("cancel order: %w", err)
	}
	return true, nil
}

func (r *Repository) CancelOrdersByFilter(ctx context.Context, userID string, market, assetID *string) ([]string, error) {
	query := `UPDATE orders
		SET status = 'CANCELED', updated_at = now()
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
	query += " RETURNING id"

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("cancel filtered orders: %w", err)
	}
	defer rows.Close()

	var canceled []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan canceled order id: %w", err)
		}
		canceled = append(canceled, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate canceled order ids: %w", err)
	}
	return canceled, nil
}

func (r *Repository) UpdateOrderFill(ctx context.Context, tx pgx.Tx, orderID string, sizeMatched string, status string, tradeID string) error {
	_, err := tx.Exec(ctx,
		`UPDATE orders SET size_matched = $2, status = $3, updated_at = now(),
			associate_trades = associate_trades || to_jsonb($4::text)
		 WHERE id = $1`, orderID, sizeMatched, status, tradeID)
	return err
}

// --- Trades ---

func (r *Repository) CreateTrade(ctx context.Context, tx pgx.Tx, t *models.Trade) error {
	err := tx.QueryRow(ctx,
		`INSERT INTO trades (taker_order_id, user_id, market, asset_id, side, size,
			fee_rate_bps, price, status, outcome, owner, maker_address, trader_side, fill_key)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		 ON CONFLICT (fill_key) DO NOTHING
		 RETURNING id, match_time, last_update`,
		t.TakerOrderID, t.UserID, t.Market, t.AssetID, t.Side, t.Size,
		t.FeeRateBps, t.Price, t.Status, t.Outcome, t.Owner, t.MakerAddress,
		t.TraderSide, t.FillKey,
	).Scan(&t.ID, &t.MatchTime, &t.LastUpdate)
	if err != nil {
		return fmt.Errorf("create trade: %w", err)
	}
	return nil
}

func (r *Repository) GetTradesByUserID(ctx context.Context, userID string, market, assetID, cursor *string) ([]models.Trade, string, error) {
	query := `SELECT id, taker_order_id, user_id, market, asset_id, side, size,
			fee_rate_bps, price, status, match_time, last_update, outcome,
			owner, maker_address, bucket_index, transaction_hash, trader_side, maker_orders
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
		argIdx++
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
		if err := rows.Scan(
			&t.ID, &t.TakerOrderID, &t.UserID, &t.Market, &t.AssetID, &t.Side,
			&t.Size, &t.FeeRateBps, &t.Price, &t.Status, &t.MatchTime, &t.LastUpdate,
			&t.Outcome, &t.Owner, &t.MakerAddress, &t.BucketIndex, &t.TransactionHash,
			&t.TraderSide, &t.MakerOrders,
		); err != nil {
			return nil, "", fmt.Errorf("scan trade: %w", err)
		}
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

func (r *Repository) UpsertPosition(ctx context.Context, tx pgx.Tx, userID, tokenID, marketID, outcome, size, avgPrice string) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO positions (user_id, token_id, market_id, outcome, size, avg_price)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (user_id, token_id) DO UPDATE SET
			size = EXCLUDED.size,
			avg_price = EXCLUDED.avg_price,
			updated_at = now()`,
		userID, tokenID, marketID, outcome, size, avgPrice)
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
	var associateTrades []byte
	err := row.Scan(
		&o.ID, &o.UserID, &o.Salt, &o.Maker, &o.Signer, &o.Taker, &o.TokenID,
		&o.MakerAmount, &o.TakerAmount, &o.Side, &o.Expiration, &o.Nonce, &o.FeeRateBps,
		&o.SignatureType, &o.Signature, &o.Price, &o.OriginalSize, &o.SizeMatched, &o.Status,
		&o.OrderType, &o.PostOnly, &o.Owner, &o.Market, &o.AssetID, &o.Outcome,
		&associateTrades, &o.CreatedAt, &o.UpdatedAt,
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
		var associateTrades []byte
		if err := rows.Scan(
			&o.ID, &o.UserID, &o.Salt, &o.Maker, &o.Signer, &o.Taker, &o.TokenID,
			&o.MakerAmount, &o.TakerAmount, &o.Side, &o.Expiration, &o.Nonce, &o.FeeRateBps,
			&o.SignatureType, &o.Signature, &o.Price, &o.OriginalSize, &o.SizeMatched, &o.Status,
			&o.OrderType, &o.PostOnly, &o.Owner, &o.Market, &o.AssetID, &o.Outcome,
			&associateTrades, &o.CreatedAt, &o.UpdatedAt,
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
