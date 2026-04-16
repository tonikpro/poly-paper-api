package trading

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/tonikpro/poly-paper-api/internal/models"
)

type Worker struct {
	svc        *Service
	bookClient *OrderBookClient
}

func NewWorker(svc *Service, bookClient *OrderBookClient) *Worker {
	return &Worker{svc: svc, bookClient: bookClient}
}

// Start runs the matching worker, calling tick every interval until ctx is done.
func (w *Worker) Start(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

// tick fetches all LIVE orders, groups by token_id, fetches each book once,
// and either fills or cancels orders.
func (w *Worker) tick(ctx context.Context) {
	orders, err := w.svc.repo.GetAllLiveOrders(ctx)
	if err != nil {
		slog.Error("worker: get live orders", "error", err)
		return
	}
	if len(orders) == 0 {
		return
	}

	// Group orders by token_id
	byToken := make(map[string][]*models.Order)
	for _, o := range orders {
		byToken[o.TokenID] = append(byToken[o.TokenID], o)
	}

	for tokenID, tokenOrders := range byToken {
		book, err := w.bookClient.FetchOrderBook(tokenID)
		if err != nil {
			if errors.Is(err, ErrOrderBookNotFound) {
				// Market is closed — cancel all LIVE orders for this token
				if cancelErr := w.svc.repo.CancelLiveOrdersByTokenID(ctx, tokenID); cancelErr != nil {
					slog.Error("worker: cancel live orders on 404", "token_id", tokenID, "error", cancelErr)
				}
				continue
			}
			slog.Warn("worker: fetch orderbook failed, skipping token", "token_id", tokenID, "error", err)
			continue
		}

		for _, order := range tokenOrders {
			limitPrice, err := strconv.ParseFloat(order.Price, 64)
			if err != nil {
				slog.Warn("worker: invalid order price", "order_id", order.ID, "price", order.Price)
				continue
			}
			currentMatched, _ := strconv.ParseFloat(order.SizeMatched, 64)
			originalSize, _ := strconv.ParseFloat(order.OriginalSize, 64)
			remaining := originalSize - currentMatched
			if remaining < 0.000001 {
				continue
			}

			result := MatchOrder(book, order.Side, limitPrice, remaining)
			if result == nil || result.FillSize < 0.000001 {
				continue
			}

			if err := w.svc.executeFill(ctx, order, result); err != nil {
				slog.Error("worker: execute fill", "order_id", order.ID, "error", err)
			}
		}
	}
}
