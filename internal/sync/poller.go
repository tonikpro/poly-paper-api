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
	ConditionID   string  `json:"conditionId"`
	QuestionID    string  `json:"questionID"`
	Question      string  `json:"question"`
	Slug          string  `json:"slug"`
	Active        bool    `json:"active"`
	Closed        bool    `json:"closed"`
	NegRisk       bool    `json:"negRisk"`
	TickSize      float64 `json:"orderPriceMinTickSize"`
	MinOrderSize  float64 `json:"orderMinSize"`
	ClobTokenIds  string  `json:"clobTokenIds"`
	Outcomes      string  `json:"outcomes"`
	OutcomePrices string  `json:"outcomePrices"`
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
		slog.Info("poller: no active tokens to check")
		return
	}

	slog.Info("poller: checking tokens", "count", len(tokenIDs), "token_ids", tokenIDs)

	for i := 0; i < len(tokenIDs); i += 10 {
		end := i + 10
		if end > len(tokenIDs) {
			end = len(tokenIDs)
		}
		batch := tokenIDs[i:end]

		markets, err := p.fetchMarkets(batch)
		if err != nil {
			slog.Warn("poller: failed to fetch markets from gamma", "batch", batch, "error", err)
			continue
		}

		slog.Info("poller: gamma returned markets", "requested_tokens", len(batch), "markets_returned", len(markets))

		for _, m := range markets {
			marketID := m.ConditionID
			if marketID == "" {
				marketID = m.QuestionID
			}
			slog.Info("poller: market status",
				"market_id", marketID,
				"question", m.Question,
				"active", m.Active,
				"closed", m.Closed,
				"outcome_prices", m.OutcomePrices)

			if err := p.upsertMarket(ctx, &m); err != nil {
				slog.Warn("poller: failed to upsert market", "market_id", marketID, "error", err)
				continue
			}

			if m.Closed {
				slog.Info("poller: market is closed, triggering resolution", "market_id", marketID)
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
	params := make([]string, len(tokenIDs))
	for i, id := range tokenIDs {
		params[i] = "clob_token_ids=" + id
	}
	url := fmt.Sprintf("%s/markets?closed=true&%s", p.gammaURL, strings.Join(params, "&"))
	resp, err := p.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch gamma markets: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gamma API returned %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read gamma response: %w", err)
	}

	var markets []GammaMarket
	if err := json.Unmarshal(body, &markets); err != nil {
		return nil, fmt.Errorf("decode gamma response: %w", err)
	}
	return markets, nil
}

func (p *Poller) upsertMarket(ctx context.Context, m *GammaMarket) error {
	marketID := m.ConditionID
	if marketID == "" {
		marketID = m.QuestionID
	}

	tickSize := fmt.Sprintf("%g", m.TickSize)
	if tickSize == "0" {
		tickSize = "0.01"
	}
	minOrderSize := fmt.Sprintf("%g", m.MinOrderSize)
	if minOrderSize == "0" {
		minOrderSize = "5"
	}

	_, err := p.pool.Exec(ctx,
		`INSERT INTO markets (id, condition_id, question, slug, active, closed, neg_risk, tick_size, min_order_size, synced_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now())
		 ON CONFLICT (id) DO UPDATE SET
			active = EXCLUDED.active,
			closed = EXCLUDED.closed,
			synced_at = now()`,
		marketID, m.ConditionID, m.Question, m.Slug, m.Active, m.Closed, m.NegRisk,
		tickSize, minOrderSize)
	if err != nil {
		return fmt.Errorf("upsert market: %w", err)
	}

	if m.ClobTokenIds == "" {
		return nil
	}

	var clobIDs []string
	if err := json.Unmarshal([]byte(m.ClobTokenIds), &clobIDs); err != nil {
		slog.Warn("poller: failed to parse clobTokenIds", "error", err)
		return nil
	}

	var outcomes []string
	if m.Outcomes != "" {
		_ = json.Unmarshal([]byte(m.Outcomes), &outcomes)
	}

	// outcomePrices is ["1","0"] or ["0","1"] — price "1" = winner
	var outcomePrices []string
	if m.OutcomePrices != "" {
		_ = json.Unmarshal([]byte(m.OutcomePrices), &outcomePrices)
	}

	for i, tokenID := range clobIDs {
		outcome := ""
		if i < len(outcomes) {
			outcome = outcomes[i]
		}

		var winner *bool
		if m.Closed && i < len(outcomePrices) {
			w := outcomePrices[i] == "1"
			winner = &w
			slog.Info("poller: token winner determined",
				"token_id", tokenID, "outcome", outcome, "price", outcomePrices[i], "winner", w)
		}

		_, err := p.pool.Exec(ctx,
			`INSERT INTO outcome_tokens (token_id, market_id, outcome, winner)
			 VALUES ($1, $2, $3, $4)
			 ON CONFLICT (token_id) DO UPDATE SET
				market_id = EXCLUDED.market_id,
				winner = EXCLUDED.winner`,
			tokenID, marketID, outcome, winner)
		if err != nil {
			slog.Warn("poller: failed to upsert outcome token", "token_id", tokenID, "error", err)
		}
	}

	return nil
}

func (p *Poller) handleResolution(ctx context.Context, m *GammaMarket) {
	marketID := m.ConditionID
	if marketID == "" {
		marketID = m.QuestionID
	}

	var hasOpenPositions bool
	err := p.pool.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM positions p
			JOIN outcome_tokens ot ON p.token_id = ot.token_id
			WHERE ot.market_id = $1 AND p.size > 0
		)`, marketID).Scan(&hasOpenPositions)
	if err != nil {
		slog.Warn("poller: failed to check open positions", "market_id", marketID, "error", err)
		return
	}

	slog.Info("poller: resolution check", "market_id", marketID, "has_open_positions", hasOpenPositions)

	if !hasOpenPositions {
		return
	}

	slog.Info("poller: market resolved, settling positions", "market_id", marketID)
	if err := p.resolver.SettleMarket(ctx, marketID); err != nil {
		slog.Error("poller: settlement failed", "market_id", marketID, "error", err)
	}
}
