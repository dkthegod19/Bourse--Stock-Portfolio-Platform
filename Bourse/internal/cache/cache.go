package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"bourse/internal/model"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Cache wraps Redis for two read-heavy concerns: live quotes (TTL-bounded) and
// derived portfolio snapshots (invalidated on write).
type Cache struct {
	rdb      *redis.Client
	quoteTTL time.Duration
}

func New(rdb *redis.Client, quoteTTLSeconds int) *Cache {
	return &Cache{rdb: rdb, quoteTTL: time.Duration(quoteTTLSeconds) * time.Second}
}

// ErrMiss indicates the key was not present in the cache.
var ErrMiss = errors.New("cache miss")

func quoteKey(symbol string) string { return "quote:" + symbol }
func snapKey(id uuid.UUID) string    { return "snap:" + id.String() }

// GetQuote returns a cached quote or ErrMiss.
func (c *Cache) GetQuote(ctx context.Context, symbol string) (model.Quote, error) {
	val, err := c.rdb.Get(ctx, quoteKey(symbol)).Bytes()
	if errors.Is(err, redis.Nil) {
		return model.Quote{}, ErrMiss
	}
	if err != nil {
		return model.Quote{}, err
	}
	var q model.Quote
	if err := json.Unmarshal(val, &q); err != nil {
		return model.Quote{}, err
	}
	return q, nil
}

// SetQuote caches a quote with the configured TTL.
func (c *Cache) SetQuote(ctx context.Context, q model.Quote) error {
	b, err := json.Marshal(q)
	if err != nil {
		return err
	}
	return c.rdb.Set(ctx, quoteKey(q.Symbol), b, c.quoteTTL).Err()
}

// GetSnapshot returns a cached portfolio view or ErrMiss.
func (c *Cache) GetSnapshot(ctx context.Context, id uuid.UUID) (model.PortfolioView, error) {
	val, err := c.rdb.Get(ctx, snapKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return model.PortfolioView{}, ErrMiss
	}
	if err != nil {
		return model.PortfolioView{}, err
	}
	var v model.PortfolioView
	if err := json.Unmarshal(val, &v); err != nil {
		return model.PortfolioView{}, err
	}
	return v, nil
}

// SetSnapshot caches a derived portfolio view briefly. It is invalidated
// explicitly whenever a trade lands.
func (c *Cache) SetSnapshot(ctx context.Context, v model.PortfolioView) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	// Short TTL so live prices stay fresh; writes also invalidate explicitly.
	return c.rdb.Set(ctx, snapKey(v.PortfolioID), b, 5*time.Second).Err()
}

// InvalidateSnapshot drops the cached view for a portfolio (call after a trade).
func (c *Cache) InvalidateSnapshot(ctx context.Context, id uuid.UUID) error {
	return c.rdb.Del(ctx, snapKey(id)).Err()
}

func (c *Cache) String() string { return fmt.Sprintf("cache(quoteTTL=%s)", c.quoteTTL) }
