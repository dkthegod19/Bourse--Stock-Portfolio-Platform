package ratelimit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Config is a per-client rate-limit configuration.
type Config struct {
	Mode     string  `json:"mode"`      // "token" or "sliding"
	Rate     float64 `json:"rate"`      // token mode: tokens/sec; sliding: requests/window
	Burst    int     `json:"burst"`     // token mode bucket capacity
	WindowMS int     `json:"window_ms"` // sliding mode window size
}

// Result is the outcome of a single limit check.
type Result struct {
	Allowed   bool
	Limit     int
	Remaining int
	ResetSec  int // seconds until the limit replenishes
}

// tokenBucket refills then attempts to consume one token, atomically.
var tokenBucket = redis.NewScript(`
local key = KEYS[1]
local rate = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local data = redis.call('HMGET', key, 'tokens', 'ts')
local tokens = tonumber(data[1])
local ts = tonumber(data[2])
if tokens == nil then tokens = burst; ts = now end
local elapsed = math.max(0, now - ts) / 1000.0
tokens = math.min(burst, tokens + elapsed * rate)
local allowed = 0
if tokens >= 1 then
  allowed = 1
  tokens = tokens - 1
end
redis.call('HSET', key, 'tokens', tokens, 'ts', now)
local ttl = 1000
if rate > 0 then ttl = math.ceil(burst / rate * 1000) + 1000 end
redis.call('PEXPIRE', key, ttl)
local reset = 0
if rate > 0 then reset = math.ceil((burst - tokens) / rate) end
return {allowed, math.floor(tokens), reset}
`)

// slidingWindow counts requests in a rolling window using a sorted set.
var slidingWindow = redis.NewScript(`
local key = KEYS[1]
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local member = ARGV[4]
redis.call('ZREMRANGEBYSCORE', key, 0, now - window)
local count = redis.call('ZCARD', key)
local allowed = 0
if count < limit then
  allowed = 1
  redis.call('ZADD', key, now, member)
  count = count + 1
end
redis.call('PEXPIRE', key, window)
local remaining = limit - count
if remaining < 0 then remaining = 0 end
return {allowed, remaining, math.ceil(window/1000)}
`)

// Limiter enforces per-key limits backed by Redis. State survives restarts
// because it lives in Redis, and the refill+consume step is a single atomic
// Lua script, so concurrent requests for the same key cannot double-spend.
type Limiter struct {
	rdb        *redis.Client
	defaultCfg Config
	counter    uint64
}

func New(rdb *redis.Client, defaultRate float64, defaultBurst int) *Limiter {
	return &Limiter{
		rdb: rdb,
		defaultCfg: Config{
			Mode:     "token",
			Rate:     defaultRate,
			Burst:    defaultBurst,
			WindowMS: 1000,
		},
	}
}

func cfgKey(key string) string    { return "rlcfg:" + key }
func bucketKey(key string) string { return "rlbucket:" + key }

// GetConfig returns the configured limit for a key, or the default.
func (l *Limiter) GetConfig(ctx context.Context, key string) (Config, error) {
	val, err := l.rdb.Get(ctx, cfgKey(key)).Bytes()
	if err == redis.Nil {
		return l.defaultCfg, nil
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(val, &c); err != nil {
		return Config{}, err
	}
	return c, nil
}

// SetConfig persists a per-key limit (admin endpoint).
func (l *Limiter) SetConfig(ctx context.Context, key string, c Config) error {
	if c.Mode == "" {
		c.Mode = "token"
	}
	if c.WindowMS == 0 {
		c.WindowMS = 1000
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return l.rdb.Set(ctx, cfgKey(key), b, 0).Err()
}

// Allow performs a single rate-limit check for a key.
func (l *Limiter) Allow(ctx context.Context, key string) (Result, error) {
	cfg, err := l.GetConfig(ctx, key)
	if err != nil {
		return Result{}, err
	}
	nowMS := time.Now().UnixMilli()

	if cfg.Mode == "sliding" {
		l.counter++
		member := fmt.Sprintf("%d-%d", nowMS, l.counter)
		res, err := slidingWindow.Run(ctx, l.rdb, []string{bucketKey(key)},
			nowMS, cfg.WindowMS, int(cfg.Rate), member).Slice()
		if err != nil {
			return Result{}, err
		}
		return Result{
			Allowed:   toInt(res[0]) == 1,
			Limit:     int(cfg.Rate),
			Remaining: toInt(res[1]),
			ResetSec:  toInt(res[2]),
		}, nil
	}

	res, err := tokenBucket.Run(ctx, l.rdb, []string{bucketKey(key)},
		cfg.Rate, cfg.Burst, nowMS).Slice()
	if err != nil {
		return Result{}, err
	}
	return Result{
		Allowed:   toInt(res[0]) == 1,
		Limit:     cfg.Burst,
		Remaining: toInt(res[1]),
		ResetSec:  toInt(res[2]),
	}, nil
}

func toInt(v any) int {
	switch n := v.(type) {
	case int64:
		return int(n)
	case int:
		return n
	case float64:
		return int(n)
	default:
		return 0
	}
}
