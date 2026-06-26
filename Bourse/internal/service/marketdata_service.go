package service

import (
	"context"
	"sort"
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

// Quote returns the price for a symbol in paise, serving from cache when fresh
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

// UniverseSymbols returns every tradable symbol, used by the poller to keep the
// whole browse/trending list fresh even before anything has been traded.
func (s *MarketDataService) UniverseSymbols() []string {
	u := s.provider.Universe()
	out := make([]string, 0, len(u))
	for _, st := range u {
		out = append(out, st.Symbol)
	}
	return out
}

// Stocks returns the full tradable universe enriched with each stock's current
// (cached) price and day-change vs. its reference close. Powers the browse list.
func (s *MarketDataService) Stocks(ctx context.Context) ([]model.StockQuote, error) {
	universe := s.provider.Universe()
	out := make([]model.StockQuote, 0, len(universe))
	for _, st := range universe {
		q, err := s.Quote(ctx, st.Symbol)
		if err != nil {
			continue // skip a transient miss rather than failing the whole list
		}
		prev, ok := s.provider.PrevClose(st.Symbol)
		if !ok || prev == 0 {
			prev = q.Price
		}
		change := q.Price - prev
		var pct float64
		if prev != 0 {
			pct = float64(change) / float64(prev) * 100
		}
		out = append(out, model.StockQuote{
			Symbol:    st.Symbol,
			Name:      st.Name,
			Sector:    st.Sector,
			Exchange:  st.Exchange,
			Price:     q.Price,
			PrevClose: prev,
			Change:    change,
			ChangePct: pct,
		})
	}
	return out, nil
}

// Trending returns the top movers (largest absolute day-change %), capped at
// limit. Ties broken by symbol for stable ordering.
func (s *MarketDataService) Trending(ctx context.Context, limit int) ([]model.StockQuote, error) {
	all, err := s.Stocks(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(all, func(i, j int) bool {
		ai, aj := abs(all[i].ChangePct), abs(all[j].ChangePct)
		if ai != aj {
			return ai > aj
		}
		return all[i].Symbol < all[j].Symbol
	})
	if limit > 0 && limit < len(all) {
		all = all[:limit]
	}
	return all, nil
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
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
