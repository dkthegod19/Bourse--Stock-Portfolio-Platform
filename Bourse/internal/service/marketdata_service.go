package service

import (
	"context"
	"time"

	"bourse/internal/cache"
	"bourse/internal/marketdata"
	"bourse/internal/model"
)

// MarketDataService is a read-through cache over a market-data Provider.
type MarketDataService struct {
	provider marketdata.Provider
	cache    *cache.Cache
}

func NewMarketDataService(p marketdata.Provider, c *cache.Cache) *MarketDataService {
	return &MarketDataService{provider: p, cache: c}
}

// Quote returns the price for a symbol in cents, serving from cache when fresh
// and falling back to the upstream provider on a miss.
func (s *MarketDataService) Quote(ctx context.Context, symbol string) (model.Quote, error) {
	if q, err := s.cache.GetQuote(ctx, symbol); err == nil {
		return q, nil
	}
	price, err := s.provider.Quote(ctx, symbol)
	if err != nil {
		return model.Quote{}, err
	}
	q := model.Quote{Symbol: symbol, Price: price, AsOf: time.Now().UnixMilli()}
	_ = s.cache.SetQuote(ctx, q) // best-effort cache fill
	return q, nil
}

// Refresh forces an upstream fetch and updates the cache (used by the poller).
func (s *MarketDataService) Refresh(ctx context.Context, symbol string) (model.Quote, error) {
	price, err := s.provider.Quote(ctx, symbol)
	if err != nil {
		return model.Quote{}, err
	}
	q := model.Quote{Symbol: symbol, Price: price, AsOf: time.Now().UnixMilli()}
	if err := s.cache.SetQuote(ctx, q); err != nil {
		return q, err
	}
	return q, nil
}
