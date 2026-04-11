package trading

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"strconv"
	"strings"

	"github.com/tonikpro/poly-paper-api/internal/models"
)

type Service struct {
	repo    *Repository
	matcher *Matcher
}

func NewService(repo *Repository, matcher *Matcher) *Service {
	return &Service{repo: repo, matcher: matcher}
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

	var marketID, outcome string
	if token != nil {
		marketID = token.MarketID
		outcome = token.Outcome
	}

	order := &models.Order{
		UserID:        userID,
		Salt:          signed.Salt,
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

	if err := s.repo.CreateOrder(ctx, order); err != nil {
		return nil, fmt.Errorf("create order: %w", err)
	}

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
		_ = s.repo.CancelOrder(ctx, order.ID, userID)
		return &models.OrderResponse{
			Success:  false,
			ErrorMsg: "FOK order could not be fully filled",
			OrderID:  order.ID,
			Status:   "CANCELED",
		}, nil
	}

	// FAK: fill what we can, cancel the rest
	if req.OrderType == "FAK" && matchResult != nil && matchResult.Remaining > 0.000001 {
		_ = s.repo.CancelOrder(ctx, order.ID, userID)
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
	result, err := s.matcher.MatchOrder(order.TokenID, order.Side, price, size)
	if err != nil {
		return nil, err
	}

	if !result.Filled || result.FillSize <= 0 {
		return result, nil
	}

	if err := s.executeFill(ctx, order, result); err != nil {
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

	// Update wallet and position
	cost := result.FillSize * result.FillPrice

	if order.Side == "BUY" {
		// Debit collateral, credit conditional token position
		costStr := fmt.Sprintf("%.6f", cost)
		if err := s.repo.DebitWallet(ctx, tx, order.UserID, "COLLATERAL", "", costStr); err != nil {
			return fmt.Errorf("debit wallet: %w", err)
		}
		// Credit conditional token wallet
		if err := s.repo.CreditWallet(ctx, tx, order.UserID, "CONDITIONAL", order.TokenID, fillSizeStr); err != nil {
			return fmt.Errorf("credit conditional: %w", err)
		}
	} else {
		// SELL: debit conditional token, credit collateral
		if err := s.repo.DebitWallet(ctx, tx, order.UserID, "CONDITIONAL", order.TokenID, fillSizeStr); err != nil {
			return fmt.Errorf("debit conditional: %w", err)
		}
		costStr := fmt.Sprintf("%.6f", cost)
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
func (s *Service) CancelOrder(ctx context.Context, userID, orderID string) error {
	return s.repo.CancelOrder(ctx, orderID, userID)
}

// CancelOrders cancels multiple orders.
func (s *Service) CancelOrders(ctx context.Context, userID string, orderIDs []string) error {
	for _, id := range orderIDs {
		if err := s.repo.CancelOrder(ctx, id, userID); err != nil {
			slog.Warn("failed to cancel order", "order_id", id, "error", err)
		}
	}
	return nil
}

// CancelAll cancels all live orders for a user.
func (s *Service) CancelAll(ctx context.Context, userID string) (int64, error) {
	return s.repo.CancelAllOrders(ctx, userID)
}

// CancelMarketOrders cancels all live orders for a user in a specific market.
func (s *Service) CancelMarketOrders(ctx context.Context, userID, market string) (int64, error) {
	return s.repo.CancelMarketOrders(ctx, userID, market)
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
			slog.Warn("failed to fetch book for background match", "token_id", tokenID, "error", err)
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

			result := matchAgainstBook(book, order.Side, price, remaining)
			if result.Filled {
				if err := s.executeFill(ctx, order, result); err != nil {
					slog.Warn("background fill failed", "order_id", order.ID, "error", err)
				}
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
