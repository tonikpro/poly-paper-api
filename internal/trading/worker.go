package trading

import (
	"context"
	"log/slog"
	"time"
)

// StartMatchWorker runs a background goroutine that periodically checks
// live orders against current Polymarket prices.
func StartMatchWorker(ctx context.Context, svc *Service, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		slog.Info("match worker started", "interval", interval)

		for {
			select {
			case <-ctx.Done():
				slog.Info("match worker stopped")
				return
			case <-ticker.C:
				if err := svc.MatchLiveOrders(ctx); err != nil {
					slog.Warn("match worker error", "error", err)
				}
			}
		}
	}()
}
