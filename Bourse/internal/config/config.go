package config

import (
	"os"
	"strconv"
)

// Config holds all runtime configuration, loaded from environment variables.
type Config struct {
	Port              string
	DatabaseURL       string
	RedisURL          string
	MarketDataProvider string // "stub" or "finnhub"
	MarketDataAPIKey  string
	DefaultRate       float64 // default rate-limit tokens/sec for unknown keys
	DefaultBurst      int     // default burst size
	LeaseSeconds      int     // job visibility timeout
	QuoteTTLSeconds   int     // how long a quote stays cached
}

// Load reads configuration from the environment, applying sane defaults so the
// service runs out of the box for local development.
func Load() Config {
	return Config{
		Port:               env("PORT", "8080"),
		DatabaseURL:        env("DATABASE_URL", "postgres://bourse:bourse@localhost:5432/bourse?sslmode=disable"),
		RedisURL:           env("REDIS_URL", "redis://localhost:6379/0"),
		MarketDataProvider: env("MARKETDATA_PROVIDER", "stub"),
		MarketDataAPIKey:   env("MARKETDATA_API_KEY", ""),
		DefaultRate:        envFloat("DEFAULT_RATE", 10),
		DefaultBurst:       envInt("DEFAULT_BURST", 20),
		LeaseSeconds:       envInt("LEASE_SECONDS", 30),
		QuoteTTLSeconds:    envInt("QUOTE_TTL_SECONDS", 10),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			return n
		}
	}
	return def
}
