package auth

import (
	"net/http"
	"strings"
)

// CLOBAuthMiddleware routes requests to the correct auth middleware based on path.
// - /auth/api-key (POST), /auth/derive-api-key (GET) → L1 auth
// - /auth/api-key (DELETE), /auth/api-keys (GET) → L2 auth
// - Trading endpoints (order, orders, cancel-*, balance-*, data/*, notifications) → L2 auth
// - Public endpoints (book, books, midpoint, price, spread, markets, etc.) → no auth
func CLOBAuthMiddleware(svc *Service) func(http.Handler) http.Handler {
	l1 := L1Middleware(svc)
	l2 := L2Middleware(svc)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Strip any /clob prefix for path matching
			path := strings.TrimPrefix(r.URL.Path, "/clob")

			switch {
			// L1 auth endpoints
			case path == "/auth/api-key" && r.Method == http.MethodPost:
				l1(next).ServeHTTP(w, r)
			case path == "/auth/derive-api-key" && r.Method == http.MethodGet:
				l1(next).ServeHTTP(w, r)

			// L2 auth endpoints
			case path == "/auth/api-key" && r.Method == http.MethodDelete:
				l2(next).ServeHTTP(w, r)
			case path == "/auth/api-keys" && r.Method == http.MethodGet:
				l2(next).ServeHTTP(w, r)

			// L2 trading endpoints
			case path == "/order" || path == "/orders":
				l2(next).ServeHTTP(w, r)
			case path == "/cancel-all" || path == "/cancel-market-orders":
				l2(next).ServeHTTP(w, r)
			case path == "/balance-allowance" || path == "/balance-allowance/update":
				l2(next).ServeHTTP(w, r)
			case strings.HasPrefix(path, "/data/"):
				l2(next).ServeHTTP(w, r)
			case path == "/notifications":
				l2(next).ServeHTTP(w, r)
			case path == "/order-scoring" || path == "/orders-scoring":
				l2(next).ServeHTTP(w, r)
			case strings.HasPrefix(path, "/v1/heartbeats"):
				l2(next).ServeHTTP(w, r)

			// Public endpoints — no auth
			default:
				next.ServeHTTP(w, r)
			}
		})
	}
}
