package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"bourse/internal/cache"
	"bourse/internal/config"
	"bourse/internal/marketdata"
	"bourse/internal/repository"
	"bourse/internal/service"
	"bourse/internal/store"
	"bourse/internal/worker"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg := config.Load()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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

	portfolioRepo := repository.NewPortfolioRepository()
	entryRepo := repository.NewEntryRepository()
	orderRepo := repository.NewOrderRepository()
	jobRepo := repository.NewJobRepository()
	alertRepo := repository.NewAlertRepository()

	c := cache.New(rdb, cfg.QuoteTTLSeconds)
	provider := marketdata.New(cfg.MarketDataProvider, cfg.MarketDataAPIKey)
	mdSvc := service.NewMarketDataService(provider, c)
	tradingSvc := service.NewTradingService(pool, portfolioRepo, entryRepo, orderRepo, jobRepo, mdSvc, c)

	w := worker.New(pool, jobRepo, orderRepo, entryRepo, alertRepo, tradingSvc, mdSvc, cfg.LeaseSeconds, logger)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		cancel()
	}()

	w.Run(ctx)
}
