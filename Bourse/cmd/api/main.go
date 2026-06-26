package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bourse/internal/api"
	"bourse/internal/cache"
	"bourse/internal/config"
	"bourse/internal/marketdata"
	"bourse/internal/ratelimit"
	"bourse/internal/repository"
	"bourse/internal/service"
	"bourse/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg := config.Load()
	ctx := context.Background()

	pool, err := store.NewPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("postgres connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	rdb, err := store.NewRedis(ctx, cfg.RedisURL)
	if err != nil {
		logger.Error("redis connect failed", "err", err)
		os.Exit(1)
	}
	defer rdb.Close()

	// Wiring: repositories -> services -> controllers (via router).
	portfolioRepo := repository.NewPortfolioRepository()
	entryRepo := repository.NewEntryRepository()
	orderRepo := repository.NewOrderRepository()
	jobRepo := repository.NewJobRepository()
	alertRepo := repository.NewAlertRepository()

	c := cache.New(rdb, cfg.QuoteTTLSeconds)
	provider := marketdata.New(cfg.MarketDataProvider, cfg.MarketDataAPIKey)
	limiter := ratelimit.New(rdb, cfg.DefaultRate, cfg.DefaultBurst)

	mdSvc := service.NewMarketDataService(provider, c)
	tradingSvc := service.NewTradingService(pool, portfolioRepo, entryRepo, orderRepo, jobRepo, mdSvc, c)
	queueSvc := service.NewQueueService(pool, jobRepo)
	alertSvc := service.NewAlertService(pool, alertRepo)

	handler := api.NewRouter(api.Deps{
		Trading:    tradingSvc,
		MarketData: mdSvc,
		Queue:      queueSvc,
		Alerts:     alertSvc,
		Limiter:    limiter,
		UIDir:      os.Getenv("UI_DIR"),
	})

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	go func() {
		logger.Info("api listening", "port", cfg.Port, "provider", cfg.MarketDataProvider)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
