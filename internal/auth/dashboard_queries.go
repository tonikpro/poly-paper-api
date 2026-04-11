package auth

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DashboardQueries handles DB queries for dashboard-specific endpoints.
type DashboardQueries struct {
	pool *pgxpool.Pool
}

func NewDashboardQueries(pool *pgxpool.Pool) *DashboardQueries {
	return &DashboardQueries{pool: pool}
}

type WalletInfo struct {
	Balance string `json:"balance"`
}

func (q *DashboardQueries) GetWallet(ctx context.Context, userID string) (*WalletInfo, error) {
	var balance string
	err := q.pool.QueryRow(ctx,
		`SELECT COALESCE(balance::text, '0') FROM wallets
		 WHERE user_id = $1 AND asset_type = 'COLLATERAL' AND token_id = ''`,
		userID,
	).Scan(&balance)
	if err != nil {
		if err == pgx.ErrNoRows {
			return &WalletInfo{Balance: "0"}, nil
		}
		return nil, err
	}
	return &WalletInfo{Balance: balance}, nil
}

func (q *DashboardQueries) Deposit(ctx context.Context, userID, amount string) error {
	_, err := q.pool.Exec(ctx,
		`INSERT INTO wallets (user_id, asset_type, token_id, balance, allowance)
		 VALUES ($1, 'COLLATERAL', '', $2::numeric, $2::numeric)
		 ON CONFLICT (user_id, asset_type, token_id) DO UPDATE SET
			balance = wallets.balance + $2::numeric,
			allowance = wallets.allowance + $2::numeric,
			updated_at = now()`,
		userID, amount)
	return err
}

func (q *DashboardQueries) Withdraw(ctx context.Context, userID, amount string) error {
	ct, err := q.pool.Exec(ctx,
		`UPDATE wallets SET balance = balance - $2::numeric, allowance = allowance - $2::numeric, updated_at = now()
		 WHERE user_id = $1 AND asset_type = 'COLLATERAL' AND token_id = '' AND balance >= $2::numeric`,
		userID, amount)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("insufficient balance")
	}
	return nil
}

func (q *DashboardQueries) GetOrders(ctx context.Context, userID string, status *string, limit, offset int) ([]map[string]any, int, error) {
	countQuery := `SELECT COUNT(*) FROM orders WHERE user_id = $1`
	dataQuery := `SELECT id, token_id, side, price::text, original_size::text, size_matched::text, status, order_type, created_at
		 FROM orders WHERE user_id = $1`
	args := []any{userID}
	argIdx := 2

	if status != nil && *status != "" {
		countQuery += fmt.Sprintf(" AND status = $%d", argIdx)
		dataQuery += fmt.Sprintf(" AND status = $%d", argIdx)
		args = append(args, *status)
		argIdx++
	}

	var total int
	if err := q.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	dataQuery += " ORDER BY created_at DESC"
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
		var id, tokenID, side, price, origSize, sizeMatched, st, orderType string
		var createdAt any
		if err := rows.Scan(&id, &tokenID, &side, &price, &origSize, &sizeMatched, &st, &orderType, &createdAt); err != nil {
			return nil, 0, err
		}
		orders = append(orders, map[string]any{
			"id": id, "token_id": tokenID, "side": side, "price": price,
			"original_size": origSize, "size_matched": sizeMatched,
			"status": st, "order_type": orderType, "created_at": createdAt,
		})
	}
	if orders == nil {
		orders = []map[string]any{}
	}
	return orders, total, nil
}

func (q *DashboardQueries) GetPositions(ctx context.Context, userID string) ([]map[string]any, error) {
	rows, err := q.pool.Query(ctx,
		`SELECT id, token_id, outcome, size::text, avg_price::text, realized_pnl::text
		 FROM positions WHERE user_id = $1 AND size > 0
		 ORDER BY updated_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var positions []map[string]any
	for rows.Next() {
		var id, tokenID, outcome, size, avgPrice, rpnl string
		if err := rows.Scan(&id, &tokenID, &outcome, &size, &avgPrice, &rpnl); err != nil {
			return nil, err
		}
		positions = append(positions, map[string]any{
			"id": id, "token_id": tokenID, "outcome": outcome,
			"size": size, "avg_price": avgPrice, "realized_pnl": rpnl,
		})
	}
	if positions == nil {
		positions = []map[string]any{}
	}
	return positions, nil
}

func (q *DashboardQueries) GetTrades(ctx context.Context, userID string, limit, offset int) ([]map[string]any, int, error) {
	var total int
	if err := q.pool.QueryRow(ctx, `SELECT COUNT(*) FROM trades WHERE user_id = $1`, userID).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `SELECT id, asset_id, side, price, size, status, match_time
		 FROM trades WHERE user_id = $1 ORDER BY match_time DESC`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	if offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", offset)
	}

	rows, err := q.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var trades []map[string]any
	for rows.Next() {
		var id, assetID, side, price, size, status string
		var matchTime any
		if err := rows.Scan(&id, &assetID, &side, &price, &size, &status, &matchTime); err != nil {
			return nil, 0, err
		}
		trades = append(trades, map[string]any{
			"id": id, "asset_id": assetID, "side": side, "price": price,
			"size": size, "status": status, "match_time": matchTime,
		})
	}
	if trades == nil {
		trades = []map[string]any{}
	}
	return trades, total, nil
}

// Unused import guard
var _ = json.Marshal
