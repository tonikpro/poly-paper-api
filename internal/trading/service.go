package trading

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

// CancelOrder cancels a single order.
func (s *Service) CancelOrder(ctx context.Context, userID, orderID string) (*models.CancelOrdersResponse, error) {
	canceled, err := s.repo.CancelOrder(ctx, orderID, userID)
	if err != nil {
		return nil, err
	}

	resp := &models.CancelOrdersResponse{
		Canceled:    []string{},
		NotCanceled: map[string]string{},
	}
	if canceled {
		resp.Canceled = append(resp.Canceled, orderID)
	} else {
		resp.NotCanceled[orderID] = "order not found or already canceled"
	}
	return resp, nil
}

// CancelOrders cancels multiple orders.
func (s *Service) CancelOrders(ctx context.Context, userID string, orderIDs []string) (*models.CancelOrdersResponse, error) {
	resp := &models.CancelOrdersResponse{
		Canceled:    []string{},
		NotCanceled: map[string]string{},
	}
	for _, id := range orderIDs {
		canceled, err := s.repo.CancelOrder(ctx, id, userID)
		if err != nil {
			slog.Warn("failed to cancel order", "order_id", id, "error", err)
			resp.NotCanceled[id] = err.Error()
			continue
		}
		if canceled {
			resp.Canceled = append(resp.Canceled, id)
			continue
		}
		resp.NotCanceled[id] = "order not found or already canceled"
	}
	return resp, nil
}

// CancelAll cancels all live orders for a user.
func (s *Service) CancelAll(ctx context.Context, userID string) (*models.CancelOrdersResponse, error) {
	canceled, err := s.repo.CancelOrdersByFilter(ctx, userID, nil, nil)
	if err != nil {
		return nil, err
	}
	return &models.CancelOrdersResponse{
		Canceled:    canceled,
		NotCanceled: map[string]string{},
	}, nil
}

// CancelMarketOrders cancels all live orders for a user in a specific market.
func (s *Service) CancelMarketOrders(ctx context.Context, userID string, market, assetID *string) (*models.CancelOrdersResponse, error) {
	canceled, err := s.repo.CancelOrdersByFilter(ctx, userID, market, assetID)
	if err != nil {
		return nil, err
	}
	return &models.CancelOrdersResponse{
		Canceled:    canceled,
		NotCanceled: map[string]string{},
	}, nil
}

// GetOrder returns a single order.
func (s *Service) GetOrder(ctx context.Context, orderID string) (*models.Order, error) {
	return s.repo.GetOrderByID(ctx, orderID)
}

// GetOrders returns paginated orders for a user.
func (s *Service) GetOrders(ctx context.Context, userID string, market, assetID, cursor *string) ([]*models.Order, string, error) {
	return s.repo.GetOrdersByUserID(ctx, userID, market, assetID, cursor)
}

// GetTrades returns paginated trades for a user.
func (s *Service) GetTrades(ctx context.Context, userID string, market, assetID, cursor *string) ([]models.Trade, string, error) {
	return s.repo.GetTradesByUserID(ctx, userID, market, assetID, cursor)
}

// GetBalanceAllowance returns balance and allowance for a specific asset.
func (s *Service) GetBalanceAllowance(ctx context.Context, userID, assetType, tokenID string) (*models.BalanceAllowanceResponse, error) {
	wallet, err := s.repo.GetWallet(ctx, userID, assetType, tokenID)
	if err != nil {
		return nil, err
	}
	if wallet == nil {
		return &models.BalanceAllowanceResponse{
			Balance:   "0",
			Allowance: "0",
		}, nil
	}
	return &models.BalanceAllowanceResponse{
		Balance:   wallet.Balance,
		Allowance: wallet.Allowance,
	}, nil
}

// deriveOrderPriceAndSize computes price and size from makerAmount/takerAmount.
// For BUY: price = makerAmount/takerAmount (USDC per token), size = takerAmount
// For SELL: price = takerAmount/makerAmount (USDC per token), size = makerAmount
// Amounts are in raw units (6 decimals for USDC, variable for tokens).
func deriveOrderPriceAndSize(side, makerAmountStr, takerAmountStr string) (float64, float64, error) {
	makerAmount := new(big.Float)
	takerAmount := new(big.Float)

	if _, ok := makerAmount.SetString(makerAmountStr); !ok {
		return 0, 0, fmt.Errorf("invalid makerAmount: %s", makerAmountStr)
	}
	if _, ok := takerAmount.SetString(takerAmountStr); !ok {
		return 0, 0, fmt.Errorf("invalid takerAmount: %s", takerAmountStr)
	}

	// Both amounts are in raw units (e.g., 1e6 for $1 USDC)
	// Price = collateral / tokens
	// For BUY: maker gives collateral (makerAmount), receives tokens (takerAmount)
	// For SELL: maker gives tokens (makerAmount), receives collateral (takerAmount)

	var price, size float64
	if strings.EqualFold(side, "BUY") {
		// price = makerAmount / takerAmount
		ratio := new(big.Float).Quo(makerAmount, takerAmount)
		price, _ = ratio.Float64()
		// size = takerAmount (number of tokens) — convert from raw
		size, _ = takerAmount.Float64()
	} else {
		// price = takerAmount / makerAmount
		ratio := new(big.Float).Quo(takerAmount, makerAmount)
		price, _ = ratio.Float64()
		// size = makerAmount (number of tokens)
		size, _ = makerAmount.Float64()
	}

	// Convert from raw units (assuming 1e6 decimals like USDC)
	size = size / 1e6

	return price, size, nil
}

// fetchAndStoreToken fetches a token's market from the Gamma API, stores it in the DB,
// and returns the outcome token.
func (s *Service) fetchAndStoreToken(ctx context.Context, tokenID string) (*models.OutcomeToken, error) {
	url := fmt.Sprintf("%s/markets?clob_token_ids=%s", s.gammaURL, tokenID)
	resp, err := s.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch gamma market: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gamma API returned %d", resp.StatusCode)
	}

	var markets []struct {
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
	if err := json.NewDecoder(resp.Body).Decode(&markets); err != nil {
		return nil, fmt.Errorf("decode gamma response: %w", err)
	}

	if len(markets) == 0 {
		return nil, fmt.Errorf("no market found for token %s", tokenID)
	}

	m := markets[0]
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

	market := &models.Market{
		ID:           marketID,
		ConditionID:  m.ConditionID,
		Question:     m.Question,
		Slug:         m.Slug,
		Active:       m.Active,
		Closed:       m.Closed,
		NegRisk:      m.NegRisk,
		TickSize:     tickSize,
		MinOrderSize: minOrderSize,
	}

	// Parse clobTokenIds, outcomes and outcomePrices to build outcome tokens.
	// outcomePrices["1","0"] means index 0 won; ["0","1"] means index 1 won.
	var outcomeTokens []models.OutcomeToken
	if m.ClobTokenIds != "" {
		var clobIDs []string
		if err := json.Unmarshal([]byte(m.ClobTokenIds), &clobIDs); err != nil {
			return nil, fmt.Errorf("parse clobTokenIds: %w", err)
		}
		var outcomes []string
		if m.Outcomes != "" {
			_ = json.Unmarshal([]byte(m.Outcomes), &outcomes)
		}
		var outcomePrices []string
		if m.OutcomePrices != "" {
			_ = json.Unmarshal([]byte(m.OutcomePrices), &outcomePrices)
		}
		for i, id := range clobIDs {
			outcome := ""
			if i < len(outcomes) {
				outcome = outcomes[i]
			}
			var winner *bool
			if m.Closed && i < len(outcomePrices) {
				w := outcomePrices[i] == "1"
				winner = &w
			}
			outcomeTokens = append(outcomeTokens, models.OutcomeToken{
				TokenID:  id,
				MarketID: marketID,
				Outcome:  outcome,
				Winner:   winner,
			})
		}
	}

	if err := s.repo.UpsertMarketAndTokens(ctx, market, outcomeTokens); err != nil {
		return nil, err
	}

	// Return the requested token
	for i := range outcomeTokens {
		if outcomeTokens[i].TokenID == tokenID {
			return &outcomeTokens[i], nil
		}
	}

	return nil, fmt.Errorf("token %s not found in market response", tokenID)
}
