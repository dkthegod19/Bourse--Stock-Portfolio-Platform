package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bourse/internal/controller"
	mw "bourse/internal/middleware"
	"bourse/internal/ratelimit"
	"bourse/internal/service"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Deps bundles everything the HTTP layer needs.
type Deps struct {
	Trading   *service.TradingService
	MarketData *service.MarketDataService
	Queue     *service.QueueService
	Alerts    *service.AlertService
	Limiter   *ratelimit.Limiter
	UIDir     string
}

// NewRouter builds the full HTTP router.
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(corsAllowAll)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// API surface, guarded by the rate limiter.
	r.Route("/v1", func(api chi.Router) {
		api.Use(mw.RateLimit(d.Limiter))

		controller.NewPortfolioController(d.Trading).Routes(api)
		controller.NewOrderController(d.Trading).Routes(api)
		controller.NewQuoteController(d.MarketData, d.Alerts).Routes(api)
		controller.NewAdminController(d.Queue, d.Limiter).Routes(api)
	})

	// Static single-page app (the built Vite/React frontend). Serve real files
	// when they exist and fall back to index.html so the SPA loads at any path.
	uiDir := d.UIDir
	if uiDir == "" {
		uiDir = "frontend/dist"
	}
	if _, err := os.Stat(filepath.Join(uiDir, "index.html")); err == nil {
		r.Handle("/*", spaHandler(uiDir))
	}

	return r
}

// spaHandler serves static assets from dir and returns index.html for any path
// that doesn't map to a file (so client-side routing / deep links work).
func spaHandler(dir string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	index := filepath.Join(dir, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := filepath.Clean(r.URL.Path)
		// Never let the SPA shadow the API or health surfaces.
		if strings.HasPrefix(clean, "/v1") || clean == "/healthz" {
			http.NotFound(w, r)
			return
		}
		if clean != "/" {
			if _, err := os.Stat(filepath.Join(dir, clean)); err == nil {
				fs.ServeHTTP(w, r)
				return
			}
		}
		http.ServeFile(w, r, index)
	})
}

// corsAllowAll lets the static dashboard call the API from a browser during
// local development.
func corsAllowAll(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type,X-API-Key,Idempotency-Key")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
