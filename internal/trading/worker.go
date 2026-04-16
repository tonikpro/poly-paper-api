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
			tickCtx, tickCancel := context.WithTimeout(ctx, 30*time.Second)
			w.tick(tickCtx)
			tickCancel()
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	orders, err := w.svc.repo.GetAllLiveOrders(ctx)
	if err != nil {
		slog.Error("worker: get live orders", "error", err)
		return
	}
	if len(orders) == 0 {
		return
	}

	byToken := make(map[string][]*models.Order)
	for _, o := range orders {
		byToken[o.TokenID] = append(byToken[o.TokenID], o)
	}

	slog.Info("worker tick", "live_orders", len(orders), "tokens", len(byToken))

	var fills, noMatch, errs int

	for tokenID, tokenOrders := range byToken {
		book, err := w.bookClient.FetchOrderBook(tokenID)
		if err != nil {
			if errors.Is(err, ErrOrderBookNotFound) {
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
				slog.Info("worker: no match", "order_id", order.ID, "side", order.Side,
					"limit_price", limitPrice, "remaining", remaining)
				noMatch++
				continue
			}

			slog.Info("worker: filling", "order_id", order.ID, "side", order.Side,
				"fill_size", result.FillSize, "fill_price", result.FillPrice)

			if err := w.svc.executeFill(ctx, order, result); err != nil {
				slog.Error("worker: execute fill", "order_id", order.ID, "error", err)
				errs++
			} else {
				fills++
			}
		}
	}

	slog.Info("worker tick done", "fills", fills, "no_match", noMatch, "errors", errs)
}
