package api

import (
	"net/http"
	"os"
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

	// Static dashboards.
	uiDir := d.UIDir
	if uiDir == "" {
		uiDir = "ui"
	}
	if _, err := os.Stat(uiDir); err == nil {
		fs := http.FileServer(http.Dir(uiDir))
		r.Handle("/ui/*", http.StripPrefix("/ui/", fs))
		r.Get("/", func(w http.ResponseWriter, req *http.Request) {
			http.Redirect(w, req, "/ui/", http.StatusFound)
		})
	}

	return r
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
