package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/tonikpro/poly-paper-api/internal/auth"
	"github.com/tonikpro/poly-paper-api/internal/config"
	"github.com/tonikpro/poly-paper-api/internal/database"
	polymw "github.com/tonikpro/poly-paper-api/internal/middleware"
	polyserver "github.com/tonikpro/poly-paper-api/internal/server"
	polysync "github.com/tonikpro/poly-paper-api/internal/sync"
	"github.com/tonikpro/poly-paper-api/internal/trading"

	clobapi "github.com/tonikpro/poly-paper-api/api/generated/clob"
	dashboardapi "github.com/tonikpro/poly-paper-api/api/generated/dashboard"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := database.Migrate(ctx, pool); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	// Services
	authRepo := auth.NewRepository(pool)
	authSvc := auth.NewService(authRepo, cfg)

	tradingRepo := trading.NewRepository(pool)
	bookClient := trading.NewOrderBookClient(cfg.PolymarketCLOBURL)
	tradingSvc := trading.NewService(tradingRepo, bookClient, cfg.PolymarketGammaURL)

	// Handlers
	dashboardQueries := auth.NewDashboardQueries(pool)
	dashboardHandler := auth.NewDashboardHandler(authSvc, dashboardQueries)
	clobAuthHandler := auth.NewCLOBAuthHandler(authSvc)
	tradingHandler := trading.NewCLOBTradingHandler(tradingSvc)
	proxyHandler := trading.NewProxyHandler(cfg.PolymarketCLOBURL)
	clobServer := polyserver.NewCLOBServer(clobAuthHandler, tradingHandler, proxyHandler)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	rateLimiter := polymw.NewRateLimiter(100, time.Minute) // 100 req/min per IP
	r.Use(rateLimiter.Handler)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"*"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Dashboard routes — JWT auth on /api/* routes, public /auth/* routes
	dashboardapi.HandlerWithOptions(dashboardHandler, dashboardapi.ChiServerOptions{
		BaseRouter: r,
		Middlewares: []dashboardapi.MiddlewareFunc{
			func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if len(r.URL.Path) >= 4 && r.URL.Path[:4] == "/api" {
						auth.JWTMiddleware(authSvc)(next).ServeHTTP(w, r)
						return
					}
					next.ServeHTTP(w, r)
				})
			},
		},
	})

	// Dashboard stats endpoint (JWT-protected)
	r.With(auth.JWTMiddleware(authSvc)).Get("/api/stats", dashboardHandler.GetStats)

	// Dashboard API key management (JWT-protected)
	r.Route("/api/api-keys", func(r chi.Router) {
		r.Use(auth.JWTMiddleware(authSvc))
		r.Get("/", dashboardHandler.ListAPIKeys)
		r.Post("/", dashboardHandler.CreateAPIKeyDashboard)
		r.Delete("/", dashboardHandler.DeleteAPIKeyDashboard)
	})

	// CLOB routes under /clob prefix — auth determined by endpoint
	clobRouter := chi.NewRouter()
	clobapi.HandlerWithOptions(clobServer, clobapi.ChiServerOptions{
		BaseRouter: clobRouter,
		Middlewares: []clobapi.MiddlewareFunc{
			auth.CLOBAuthMiddleware(authSvc),
		},
	})
	r.Mount("/clob", clobRouter)

	// Serve dashboard SPA — any path not matched above falls through to index.html
	dashboardDist := http.Dir("dashboard/dist")
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		f, err := dashboardDist.Open(r.URL.Path)
		if err == nil {
			f.Close()
			http.FileServer(dashboardDist).ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, "dashboard/dist/index.html")
	})

	// Start market sync poller (checks resolutions every 60s)
	resolver := polysync.NewResolver(pool)
	poller := polysync.NewPoller(pool, cfg.PolymarketGammaURL, resolver)
	poller.Start(ctx, 60*time.Second)

	addr := fmt.Sprintf(":%d", cfg.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	go func() {
		slog.Info("server starting", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down server")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown error", "error", err)
	}
}
