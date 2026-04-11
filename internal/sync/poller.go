package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// GammaMarket represents a market from Polymarket's Gamma API
type GammaMarket struct {
	ConditionID   string `json:"condition_id"`
	QuestionID    string `json:"question_id"`
	Question      string `json:"question"`
	Slug          string `json:"slug"`
	Active        bool   `json:"active"`
	Closed        bool   `json:"closed"`
	NegRisk       bool   `json:"neg_risk"`
	TickSize      string `json:"minimum_tick_size"`
	MinOrderSize  string `json:"minimum_order_size"`
	Outcome       string `json:"outcome"` // "Yes" / "No" for resolved markets
	ClobTokenIds  string `json:"clob_token_ids"`
	Tokens        []struct {
		TokenID string `json:"token_id"`
		Outcome string `json:"outcome"`
		Winner  bool   `json:"winner"`
	} `json:"tokens"`
}

type Poller struct {
	pool       *pgxpool.Pool
	gammaURL   string
	httpClient *http.Client
	resolver   *Resolver
}

func NewPoller(pool *pgxpool.Pool, gammaURL string, resolver *Resolver) *Poller {
	return &Poller{
		pool:     pool,
		gammaURL: gammaURL,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		resolver: resolver,
	}
}

// Start runs the poller in a background goroutine.
func (p *Poller) Start(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		slog.Info("market sync poller started", "interval", interval)

		// Run once immediately
		p.poll(ctx)

		for {
			select {
			case <-ctx.Done():
				slog.Info("market sync poller stopped")
				return
			case <-ticker.C:
				p.poll(ctx)
			}
		}
	}()
}

func (p *Poller) poll(ctx context.Context) {
	tokenIDs, err := p.getActiveTokenIDs(ctx)
	if err != nil {
		slog.Warn("poller: failed to get active token IDs", "error", err)
		return
	}
	if len(tokenIDs) == 0 {
		return
	}

	slog.Debug("poller: checking tokens", "count", len(tokenIDs))

	// Batch tokens into groups (Gamma API supports comma-separated)
	for i := 0; i < len(tokenIDs); i += 10 {
		end := i + 10
		if end > len(tokenIDs) {
			end = len(tokenIDs)
		}
		batch := tokenIDs[i:end]

		markets, err := p.fetchMarkets(batch)
		if err != nil {
			slog.Warn("poller: failed to fetch markets from gamma", "error", err)
			continue
		}

		for _, m := range markets {
			if err := p.upsertMarket(ctx, &m); err != nil {
				slog.Warn("poller: failed to upsert market", "market_id", m.ConditionID, "error", err)
				continue
			}

			// Check for resolution
			if m.Closed {
				p.handleResolution(ctx, &m)
			}
		}
	}
}

func (p *Poller) getActiveTokenIDs(ctx context.Context) ([]string, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT DISTINCT token_id FROM (
			SELECT token_id FROM orders WHERE status = 'LIVE'
			UNION
			SELECT token_id FROM positions WHERE size > 0
		) t`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (p *Poller) fetchMarkets(tokenIDs []string) ([]GammaMarket, error) {
	url := fmt.Sprintf("%s/markets?clob_token_ids=%s", p.gammaURL, strings.Join(tokenIDs, ","))
	resp, err := p.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch gamma markets: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gamma API returned %d: %s", resp.StatusCode, string(body))
	}

	var markets []GammaMarket
	if err := json.NewDecoder(resp.Body).Decode(&markets); err != nil {
		return nil, fmt.Errorf("decode gamma response: %w", err)
	}
	return markets, nil
}

func (p *Poller) upsertMarket(ctx context.Context, m *GammaMarket) error {
	marketID := m.ConditionID
	if marketID == "" {
		marketID = m.QuestionID
	}

	_, err := p.pool.Exec(ctx,
		`INSERT INTO markets (id, condition_id, question, slug, active, closed, neg_risk, tick_size, min_order_size, synced_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now())
		 ON CONFLICT (id) DO UPDATE SET
			active = EXCLUDED.active,
			closed = EXCLUDED.closed,
			synced_at = now()`,
		marketID, m.ConditionID, m.Question, m.Slug, m.Active, m.Closed, m.NegRisk,
		defaultStr(m.TickSize, "0.01"), defaultStr(m.MinOrderSize, "5"))
	if err != nil {
		return fmt.Errorf("upsert market: %w", err)
	}

	// Upsert outcome tokens
	for _, token := range m.Tokens {
		_, err := p.pool.Exec(ctx,
			`INSERT INTO outcome_tokens (token_id, market_id, outcome, winner)
			 VALUES ($1, $2, $3, $4)
			 ON CONFLICT (token_id) DO UPDATE SET
				winner = EXCLUDED.winner`,
			token.TokenID, marketID, token.Outcome, boolToNullable(token.Winner, m.Closed))
		if err != nil {
			slog.Warn("poller: failed to upsert outcome token", "token_id", token.TokenID, "error", err)
		}
	}

	return nil
}

func (p *Poller) handleResolution(ctx context.Context, m *GammaMarket) {
	marketID := m.ConditionID
	if marketID == "" {
		marketID = m.QuestionID
	}

	// Check if we already settled this market
	var settled bool
	err := p.pool.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM positions p
			JOIN outcome_tokens ot ON p.token_id = ot.token_id
			WHERE ot.market_id = $1 AND p.size > 0
		)`, marketID).Scan(&settled)
	if err != nil || !settled {
		return // no open positions to settle
	}

	slog.Info("poller: market resolved, settling positions", "market_id", marketID)
	if err := p.resolver.SettleMarket(ctx, marketID); err != nil {
		slog.Error("poller: settlement failed", "market_id", marketID, "error", err)
	}
}

func defaultStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func boolToNullable(winner bool, closed bool) *bool {
	if !closed {
		return nil
	}
	return &winner
}
