package sync

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Resolver struct {
	pool *pgxpool.Pool
}

func NewResolver(pool *pgxpool.Pool) *Resolver {
	return &Resolver{pool: pool}
}

// SettleMarket settles all positions for a resolved market.
// Universal payout rule: payout_per_share = winner ? 1.0 : 0.0
func (r *Resolver) SettleMarket(ctx context.Context, marketID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Get all tokens for this market with their winner status
	tokenRows, err := tx.Query(ctx,
		`SELECT token_id, outcome, winner FROM outcome_tokens WHERE market_id = $1`,
		marketID)
	if err != nil {
		return fmt.Errorf("get tokens: %w", err)
	}

	type tokenInfo struct {
		tokenID string
		outcome string
		winner  *bool
	}
	var tokens []tokenInfo
	var tokenIDs []string
	for tokenRows.Next() {
		var t tokenInfo
		if err := tokenRows.Scan(&t.tokenID, &t.outcome, &t.winner); err != nil {
			tokenRows.Close()
			return fmt.Errorf("scan token: %w", err)
		}
		tokens = append(tokens, t)
		tokenIDs = append(tokenIDs, t.tokenID)
	}
	tokenRows.Close()

	if len(tokens) == 0 {
		return nil
	}

	// Build winner lookup
	winnerMap := make(map[string]bool)
	for _, t := range tokens {
		if t.winner != nil {
			winnerMap[t.tokenID] = *t.winner
		}
	}

	// Lock and process all positions for tokens in this market
	posRows, err := tx.Query(ctx,
		`SELECT p.id, p.user_id, p.token_id, p.size, p.avg_price, p.realized_pnl
		 FROM positions p
		 JOIN outcome_tokens ot ON p.token_id = ot.token_id
		 WHERE ot.market_id = $1 AND p.size > 0
		 FOR UPDATE OF p`,
		marketID)
	if err != nil {
		return fmt.Errorf("get positions: %w", err)
	}

	type positionInfo struct {
		id          string
		userID      string
		tokenID     string
		size        float64
		avgPrice    float64
		realizedPnl float64
	}
	var positions []positionInfo
	for posRows.Next() {
		var p positionInfo
		var sizeStr, avgPriceStr, rpnlStr string
		if err := posRows.Scan(&p.id, &p.userID, &p.tokenID, &sizeStr, &avgPriceStr, &rpnlStr); err != nil {
			posRows.Close()
			return fmt.Errorf("scan position: %w", err)
		}
		p.size, _ = strconv.ParseFloat(sizeStr, 64)
		p.avgPrice, _ = strconv.ParseFloat(avgPriceStr, 64)
		p.realizedPnl, _ = strconv.ParseFloat(rpnlStr, 64)
		positions = append(positions, p)
	}
	posRows.Close()

	// Settle each position
	for _, pos := range positions {
		winner, ok := winnerMap[pos.tokenID]
		if !ok {
			slog.Warn("resolver: no winner info for token", "token_id", pos.tokenID)
			continue
		}

		// Universal payout rule
		var payoutPerShare float64
		if winner {
			payoutPerShare = 1.0
		}

		totalPayout := pos.size * payoutPerShare
		realizedPnl := totalPayout - (pos.size * pos.avgPrice)

		slog.Info("resolver: settling position",
			"user_id", pos.userID,
			"token_id", pos.tokenID,
			"size", pos.size,
			"winner", winner,
			"payout", totalPayout,
			"pnl", realizedPnl)

		// Credit COLLATERAL wallet with payout
		if totalPayout > 0 {
			payoutStr := fmt.Sprintf("%.6f", totalPayout)
			_, err := tx.Exec(ctx,
				`INSERT INTO wallets (user_id, asset_type, token_id, balance, allowance)
				 VALUES ($1, 'COLLATERAL', '', $2::numeric, $2::numeric)
				 ON CONFLICT (user_id, asset_type, token_id) DO UPDATE SET
					balance = wallets.balance + $2::numeric,
					allowance = wallets.allowance + $2::numeric,
					updated_at = now()`,
				pos.userID, payoutStr)
			if err != nil {
				return fmt.Errorf("credit wallet: %w", err)
			}
		}

		// Zero out CONDITIONAL wallet for this token
		_, err := tx.Exec(ctx,
			`UPDATE wallets SET balance = 0, allowance = 0, updated_at = now()
			 WHERE user_id = $1 AND asset_type = 'CONDITIONAL' AND token_id = $2`,
			pos.userID, pos.tokenID)
		if err != nil {
			return fmt.Errorf("zero conditional wallet: %w", err)
		}

		// Close position and record PnL
		rpnlStr := fmt.Sprintf("%.6f", pos.realizedPnl+realizedPnl)
		_, err = tx.Exec(ctx,
			`UPDATE positions SET size = 0, realized_pnl = $3, updated_at = now()
			 WHERE id = $1 AND user_id = $2`,
			pos.id, pos.userID, rpnlStr)
		if err != nil {
			return fmt.Errorf("close position: %w", err)
		}
	}

	// Cancel all remaining LIVE orders for tokens in this market
	_, err = tx.Exec(ctx,
		`UPDATE orders SET status = 'CANCELED', updated_at = now()
		 WHERE token_id = ANY($1) AND status = 'LIVE'`,
		tokenIDs)
	if err != nil {
		return fmt.Errorf("cancel orders: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit settlement: %w", err)
	}

	slog.Info("resolver: market settled successfully", "market_id", marketID, "positions_settled", len(positions))
	return nil
}
