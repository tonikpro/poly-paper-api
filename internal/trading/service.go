package trading

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tonikpro/poly-paper-api/internal/models"
)

type Service struct {
	repo       *Repository
	matcher    *Matcher
	gammaURL   string
	httpClient *http.Client
}

func NewService(repo *Repository, matcher *Matcher, gammaURL string) *Service {
	return &Service{
		repo:       repo,
		matcher:    matcher,
		gammaURL:   gammaURL,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// PlaceOrder validates and creates an order, attempts immediate matching.
func (s *Service) PlaceOrder(ctx context.Context, userID string, req *models.PostOrderRequest) (*models.OrderResponse, error) {
	signed := req.Order

	// Derive price and size from maker/taker amounts
	price, size, err := deriveOrderPriceAndSize(signed.Side, signed.MakerAmount, signed.TakerAmount)
	if err != nil {
		return nil, fmt.Errorf("invalid order amounts: %w", err)
	}

	// Look up the outcome token for market/outcome info
	token, err := s.repo.GetOutcomeToken(ctx, signed.TokenID)
	if err != nil {
		return nil, fmt.Errorf("lookup token: %w", err)
	}

	// If token not in DB, fetch from Polymarket Gamma API and store it
	if token == nil {
		token, err = s.fetchAndStoreToken(ctx, signed.TokenID)
		if err != nil {
			return nil, fmt.Errorf("resolve token: %w", err)
		}
	}

	marketID := token.MarketID
	outcome := token.Outcome

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
		Price:         fmt.Sprintf("%.4f", price),
		OriginalSize:  fmt.Sprintf("%.6f", size),
		SizeMatched:   "0",
		Status:        "LIVE",
		OrderType:     req.OrderType,
		PostOnly:      req.PostOnly,
		Owner:         req.Owner,
		Market:        marketID,
		AssetID:       signed.TokenID,
		Outcome:       outcome,
	}

	// Reserve funds and create order atomically:
	// BUY  → lock (size * price) collateral
	// SELL → lock size conditional tokens
	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin order tx: %w", err)
	}
	txDone := false
	defer func() {
		if !txDone {
			tx.Rollback(ctx)
		}
	}()

	if order.Side == "BUY" {
		reserveStr := fmt.Sprintf("%.6f", size*price)
		if err := s.repo.DebitWallet(ctx, tx, userID, "COLLATERAL", "", reserveStr); err != nil {
			return nil, fmt.Errorf("insufficient balance: %w", err)
		}
	} else {
		sizeStr := fmt.Sprintf("%.6f", size)
		if err := s.repo.DebitWallet(ctx, tx, userID, "CONDITIONAL", signed.TokenID, sizeStr); err != nil {
			return nil, fmt.Errorf("insufficient conditional balance: %w", err)
		}
	}

	if err := s.repo.CreateOrderTx(ctx, tx, order); err != nil {
		return nil, fmt.Errorf("create order: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit order: %w", err)
	}
	txDone = true

	// Try immediate matching
	matchResult, matchErr := s.tryMatch(ctx, order, price, size)
	if matchErr != nil {
		slog.Warn("match attempt failed, order stays LIVE", "order_id", order.ID, "error", matchErr)
	}

	status := order.Status
	if matchResult != nil && matchResult.Filled {
		if matchResult.Remaining <= 0.000001 {
			status = "MATCHED"
		} else {
			status = "LIVE"
		}
	}

	// FOK: must fill entirely or cancel
	if req.OrderType == "FOK" && status != "MATCHED" {
		_, _ = s.repo.CancelOrder(ctx, order.ID, userID)
		return &models.OrderResponse{
			Success:  false,
			ErrorMsg: "FOK order could not be fully filled",
			OrderID:  order.ID,
			Status:   "CANCELED",
		}, nil
	}

	// FAK: fill what we can, cancel the rest
	if req.OrderType == "FAK" && matchResult != nil && matchResult.Remaining > 0.000001 {
		_, _ = s.repo.CancelOrder(ctx, order.ID, userID)
		status = "CANCELED"
	}

	return &models.OrderResponse{
		Success:            true,
		ErrorMsg:           "",
		OrderID:            order.ID,
		TransactionsHashes: []string{},
		Status:             status,
		TakingAmount:       order.TakerAmount,
		MakingAmount:       order.MakerAmount,
	}, nil
}

// tryMatch attempts to fill an order against the Polymarket orderbook.
func (s *Service) tryMatch(ctx context.Context, order *models.Order, price, size float64) (*MatchResult, error) {
	slog.Info("tryMatch: attempting match",
		"order_id", order.ID, "token_id", order.TokenID,
		"side", order.Side, "price", price, "size", size)

	result, err := s.matcher.MatchOrder(order.TokenID, order.Side, price, size)
	if err != nil {
		slog.Warn("tryMatch: MatchOrder failed", "order_id", order.ID, "error", err)
		return nil, err
	}

	slog.Info("tryMatch: match result",
		"order_id", order.ID, "filled", result.Filled,
		"fill_price", result.FillPrice, "fill_size", result.FillSize,
		"remaining", result.Remaining)

	if !result.Filled || result.FillSize <= 0 {
		return result, nil
	}

	if err := s.executeFill(ctx, order, result); err != nil {
		slog.Error("tryMatch: executeFill failed", "order_id", order.ID, "error", err)
		return nil, fmt.Errorf("execute fill: %w", err)
	}

	return result, nil
}

// executeFill runs the fill inside a transaction with proper locking.
func (s *Service) executeFill(ctx context.Context, order *models.Order, result *MatchResult) error {
	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Lock the order
	locked, err := s.repo.GetOrderByIDForUpdate(ctx, tx, order.ID)
	if err != nil || locked == nil {
		return fmt.Errorf("lock order: %w", err)
	}
	if locked.Status != "LIVE" {
		return fmt.Errorf("order no longer live")
	}

	fillSizeStr := fmt.Sprintf("%.6f", result.FillSize)
	fillPriceStr := fmt.Sprintf("%.4f", result.FillPrice)
	fillKey := fmt.Sprintf("%s-%s-%.6f", order.ID, fillPriceStr, result.FillSize)

	// Determine new status
	currentMatched, _ := strconv.ParseFloat(locked.SizeMatched, 64)
	newMatched := currentMatched + result.FillSize
	originalSize, _ := strconv.ParseFloat(locked.OriginalSize, 64)
	newStatus := "LIVE"
	if newMatched >= originalSize-0.000001 {
		newStatus = "MATCHED"
	}
	newMatchedStr := fmt.Sprintf("%.6f", newMatched)

	// Create trade
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
		TraderSide:   "TAKER",
		FillKey:      fillKey,
	}

	if err := s.repo.CreateTrade(ctx, tx, trade); err != nil {
		return fmt.Errorf("create trade: %w", err)
	}

	// Update order
	if err := s.repo.UpdateOrderFill(ctx, tx, order.ID, newMatchedStr, newStatus, trade.ID); err != nil {
		return fmt.Errorf("update order: %w", err)
	}

	// Update wallet and position.
	// Funds were already reserved when the order was placed, so we don't debit again.
	// For BUY: reserved (size * limit_price). If fill_price < limit_price, refund the excess.
	// For SELL: reserved the conditional tokens. Just credit the received collateral.
	limitPrice, _ := strconv.ParseFloat(order.Price, 64)

	if order.Side == "BUY" {
		excess := (limitPrice - result.FillPrice) * result.FillSize
		if excess > 0.000001 {
			excessStr := fmt.Sprintf("%.6f", excess)
			if err := s.repo.CreditWallet(ctx, tx, order.UserID, "COLLATERAL", "", excessStr); err != nil {
				return fmt.Errorf("refund excess collateral: %w", err)
			}
		}
		if err := s.repo.CreditWallet(ctx, tx, order.UserID, "CONDITIONAL", order.TokenID, fillSizeStr); err != nil {
			return fmt.Errorf("credit conditional: %w", err)
		}
	} else {
		// SELL: conditional tokens already reserved; credit received collateral
		costStr := fmt.Sprintf("%.6f", result.FillSize*result.FillPrice)
		if err := s.repo.CreditWallet(ctx, tx, order.UserID, "COLLATERAL", "", costStr); err != nil {
			return fmt.Errorf("credit collateral: %w", err)
		}
	}

	// Update position
	pos, err := s.repo.GetPositionForUpdate(ctx, tx, order.UserID, order.TokenID)
	if err != nil {
		return fmt.Errorf("get position: %w", err)
	}

	if order.Side == "BUY" {
		var newSize, newAvg float64
		if pos != nil {
			existingSize, _ := strconv.ParseFloat(pos.Size, 64)
			existingAvg, _ := strconv.ParseFloat(pos.AvgPrice, 64)
			newSize = existingSize + result.FillSize
			newAvg = (existingSize*existingAvg + result.FillSize*result.FillPrice) / newSize
		} else {
			newSize = result.FillSize
			newAvg = result.FillPrice
		}
		if err := s.repo.UpsertPosition(ctx, tx, order.UserID, order.TokenID, order.Market, order.Outcome,
			fmt.Sprintf("%.6f", newSize), fmt.Sprintf("%.4f", newAvg)); err != nil {
			return fmt.Errorf("upsert position: %w", err)
		}
	} else {
		// SELL: reduce position
		if pos != nil {
			existingSize, _ := strconv.ParseFloat(pos.Size, 64)
			newSize := existingSize - result.FillSize
			if newSize < 0.000001 {
				newSize = 0
			}
			if err := s.repo.UpsertPosition(ctx, tx, order.UserID, order.TokenID, order.Market, order.Outcome,
				fmt.Sprintf("%.6f", newSize), pos.AvgPrice); err != nil {
				return fmt.Errorf("upsert position: %w", err)
			}
		}
	}

	return tx.Commit(ctx)
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

// MatchLiveOrders is called by the background worker to check all live orders.
func (s *Service) MatchLiveOrders(ctx context.Context) error {
	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	orders, err := s.repo.GetLiveOrdersForUpdate(ctx, tx)
	if err != nil {
		return err
	}

	// We need to commit the lock transaction before attempting matches,
	// because matching opens new transactions per order.
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	// Group by token to batch API calls
	byToken := make(map[string][]*models.Order)
	for _, o := range orders {
		byToken[o.TokenID] = append(byToken[o.TokenID], o)
	}

	for tokenID, tokenOrders := range byToken {
		book, err := s.matcher.FetchOrderBook(tokenID)
		if err != nil {
			if errors.Is(err, ErrOrderBookNotFound) {
				n, cancelErr := s.repo.CancelLiveOrdersByTokenID(ctx, tokenID)
				if cancelErr != nil {
					slog.Warn("failed to cancel orders for resolved market", "token_id", tokenID, "error", cancelErr)
				} else {
					slog.Info("canceled orders for resolved market", "token_id", tokenID, "count", n, "orders", len(tokenOrders))
				}
			} else {
				slog.Warn("failed to fetch book for background match", "token_id", tokenID, "error", err)
			}
			continue
		}

		for _, order := range tokenOrders {
			// Check GTD expiration
			if order.OrderType == "GTD" && order.Expiration != "0" {
				exp, _ := strconv.ParseInt(order.Expiration, 10, 64)
				if exp > 0 {
					now := strconv.FormatInt(exp, 10) // just check if expired
					_ = now
					// TODO: compare with current time
				}
			}

			price, _ := strconv.ParseFloat(order.Price, 64)
			origSize, _ := strconv.ParseFloat(order.OriginalSize, 64)
			matched, _ := strconv.ParseFloat(order.SizeMatched, 64)
			remaining := origSize - matched

			slog.Info("background match: checking order",
				"order_id", order.ID, "side", order.Side,
				"price", price, "remaining", remaining,
				"num_bids", len(book.Bids), "num_asks", len(book.Asks))

			result := matchAgainstBook(book, order.Side, price, remaining)
			if result.Filled {
				slog.Info("background match: fill found",
					"order_id", order.ID, "fill_price", result.FillPrice,
					"fill_size", result.FillSize, "remaining", result.Remaining)
				if err := s.executeFill(ctx, order, result); err != nil {
					slog.Warn("background fill failed", "order_id", order.ID, "error", err)
				} else {
					slog.Info("background match: fill executed", "order_id", order.ID)
				}
			} else {
				slog.Info("background match: no fill",
					"order_id", order.ID, "side", order.Side, "price", price)
			}
		}
	}

	return nil
}

// matchAgainstBook matches against a pre-fetched orderbook (no API call).
func matchAgainstBook(book *OrderBookResponse, side string, orderPrice, orderSize float64) *MatchResult {
	result := &MatchResult{Remaining: orderSize}

	if side == "BUY" {
		asks := parseLevels(book.Asks)
		sort.Slice(asks, func(i, j int) bool { return asks[i].price < asks[j].price })
		var totalFilled, weightedPrice float64
		for _, level := range asks {
			if orderPrice < level.price {
				break
			}
			canFill := orderSize - totalFilled
			if canFill <= 0 {
				break
			}
			fillAtLevel := min(canFill, level.size)
			weightedPrice += fillAtLevel * level.price
			totalFilled += fillAtLevel
		}
		if totalFilled > 0 {
			result.Filled = true
			result.FillPrice = weightedPrice / totalFilled
			result.FillSize = totalFilled
			result.Remaining = orderSize - totalFilled
			result.Partial = result.Remaining > 0
		}
	} else {
		bids := parseLevels(book.Bids)
		sort.Slice(bids, func(i, j int) bool { return bids[i].price > bids[j].price })
		var totalFilled, weightedPrice float64
		for _, level := range bids {
			if orderPrice > level.price {
				break
			}
			canFill := orderSize - totalFilled
			if canFill <= 0 {
				break
			}
			fillAtLevel := min(canFill, level.size)
			weightedPrice += fillAtLevel * level.price
			totalFilled += fillAtLevel
		}
		if totalFilled > 0 {
			result.Filled = true
			result.FillPrice = weightedPrice / totalFilled
			result.FillSize = totalFilled
			result.Remaining = orderSize - totalFilled
			result.Partial = result.Remaining > 0
		}
	}

	return result
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
