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
	// Show all positions: open (size > 0) and settled (realized_pnl != 0).
	// Join outcome_tokens for winner status and markets for the question text.
	rows, err := q.pool.Query(ctx,
		`SELECT p.id, p.token_id, p.outcome, p.size::text, p.avg_price::text, p.realized_pnl::text,
		        ot.winner, COALESCE(m.question, '') AS question,
		        p.size > 0 AS is_open
		 FROM positions p
		 LEFT JOIN outcome_tokens ot ON p.token_id = ot.token_id
		 LEFT JOIN markets m ON p.market_id = m.id
		 WHERE p.user_id = $1
		   AND (p.size > 0 OR ABS(p.realized_pnl) > 0.000001)
		 ORDER BY p.updated_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var positions []map[string]any
	for rows.Next() {
		var id, tokenID, outcome, size, avgPrice, rpnl, question string
		var winner *bool
		var isOpen bool
		if err := rows.Scan(&id, &tokenID, &outcome, &size, &avgPrice, &rpnl, &winner, &question, &isOpen); err != nil {
			return nil, err
		}
		positions = append(positions, map[string]any{
			"id": id, "token_id": tokenID, "outcome": outcome,
			"size": size, "avg_price": avgPrice, "realized_pnl": rpnl,
			"winner": winner, "question": question, "is_open": isOpen,
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

	// Join with outcome_tokens to show whether the bet won or lost.
	query := `SELECT t.id, t.asset_id, t.side, t.price, t.size, t.status, t.match_time,
		         t.outcome, ot.winner,
		         CASE WHEN ot.winner IS NOT NULL AND t.side = 'BUY' THEN
		             CASE WHEN ot.winner THEN
		                 t.size::numeric * (1 - t.price::numeric)
		             ELSE
		                 -(t.size::numeric * t.price::numeric)
		             END
		         END AS profit_loss
		  FROM trades t
		  LEFT JOIN outcome_tokens ot ON t.asset_id = ot.token_id
		  WHERE t.user_id = $1 ORDER BY t.match_time DESC`
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
		var outcome string
		var matchTime any
		var winner *bool
		var profitLoss *float64
		if err := rows.Scan(&id, &assetID, &side, &price, &size, &status, &matchTime,
			&outcome, &winner, &profitLoss); err != nil {
			return nil, 0, err
		}
		trades = append(trades, map[string]any{
			"id": id, "asset_id": assetID, "side": side, "price": price,
			"size": size, "status": status, "match_time": matchTime,
			"outcome": outcome, "winner": winner, "profit_loss": profitLoss,
		})
	}
	if trades == nil {
		trades = []map[string]any{}
	}
	return trades, total, nil
}

// GetStats returns P&L and bet statistics for a user across time periods.
func (q *DashboardQueries) GetStats(ctx context.Context, userID string) (map[string]any, error) {
	var totalPnl, todayPnl, monthPnl float64
	err := q.pool.QueryRow(ctx,
		`SELECT
		     COALESCE(SUM(realized_pnl), 0),
		     COALESCE(SUM(CASE WHEN DATE(updated_at AT TIME ZONE 'UTC') = CURRENT_DATE THEN realized_pnl ELSE 0 END), 0),
		     COALESCE(SUM(CASE WHEN DATE_TRUNC('month', updated_at) = DATE_TRUNC('month', NOW()) THEN realized_pnl ELSE 0 END), 0)
		 FROM positions
		 WHERE user_id = $1 AND ABS(realized_pnl) > 0.000001`,
		userID,
	).Scan(&totalPnl, &todayPnl, &monthPnl)
	if err != nil {
		return nil, fmt.Errorf("get pnl stats: %w", err)
	}

	var totalBets, wonBets, lostBets int
	err = q.pool.QueryRow(ctx,
		`SELECT
		     COUNT(*) FILTER (WHERE t.side = 'BUY'),
		     COUNT(*) FILTER (WHERE t.side = 'BUY' AND ot.winner = true),
		     COUNT(*) FILTER (WHERE t.side = 'BUY' AND ot.winner = false)
		 FROM trades t
		 LEFT JOIN outcome_tokens ot ON t.asset_id = ot.token_id
		 WHERE t.user_id = $1`,
		userID,
	).Scan(&totalBets, &wonBets, &lostBets)
	if err != nil {
		return nil, fmt.Errorf("get bet stats: %w", err)
	}

	var currentBalance string
	if err := q.pool.QueryRow(ctx,
		`SELECT COALESCE(balance::text, '0') FROM wallets
		 WHERE user_id = $1 AND asset_type = 'COLLATERAL' AND token_id = ''`,
		userID,
	).Scan(&currentBalance); err != nil {
		currentBalance = "0"
	}

	return map[string]any{
		"total_pnl":       totalPnl,
		"today_pnl":       todayPnl,
		"month_pnl":       monthPnl,
		"total_bets":      totalBets,
		"won_bets":        wonBets,
		"lost_bets":       lostBets,
		"current_balance": currentBalance,
	}, nil
}

// Unused import guard
var _ = json.Marshal
