package middleware

import (
	"encoding/json"
	"net/http"
	"strconv"

	"bourse/internal/ratelimit"
)

// RateLimit returns middleware that enforces a per-API-key limit and sets the
// standard rate-limit headers on every response. The client key comes from the
// X-API-Key header; unkeyed requests fall back to the remote address.
func RateLimit(limiter *ratelimit.Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("X-API-Key")
			if key == "" {
				key = "anon:" + clientIP(r)
			}

			res, err := limiter.Allow(r.Context(), key)
			if err != nil {
				// Fail open on limiter errors so a Redis blip doesn't take down
				// the whole API; log-and-allow is the safer default here.
				next.ServeHTTP(w, r)
				return
			}

			h := w.Header()
			h.Set("X-RateLimit-Limit", strconv.Itoa(res.Limit))
			h.Set("X-RateLimit-Remaining", strconv.Itoa(res.Remaining))
			h.Set("X-RateLimit-Reset", strconv.Itoa(res.ResetSec))

			if !res.Allowed {
				h.Set("Retry-After", strconv.Itoa(res.ResetSec))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	return r.RemoteAddr
}
