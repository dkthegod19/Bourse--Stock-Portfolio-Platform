package controller

import (
	"net/http"
	"strconv"

	"bourse/internal/service"

	"github.com/go-chi/chi/v5"
)

// QuoteController exposes market quotes and price-alert creation.
type QuoteController struct {
	md     *service.MarketDataService
	alerts *service.AlertService
}

func NewQuoteController(md *service.MarketDataService, alerts *service.AlertService) *QuoteController {
	return &QuoteController{md: md, alerts: alerts}
}

func (c *QuoteController) Routes(r chi.Router) {
	r.Get("/stocks", c.stocks)
	r.Get("/stocks/trending", c.trending)
	r.Get("/quotes/{symbol}", c.quote)
	r.Post("/alerts", c.createAlert)
}

// stocks lists the full tradable NSE universe with live price + day-change.
func (c *QuoteController) stocks(w http.ResponseWriter, r *http.Request) {
	list, err := c.md.Stocks(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stocks": list})
}

// trending lists the top movers; ?limit=N (default 6).
func (c *QuoteController) trending(w http.ResponseWriter, r *http.Request) {
	limit := 6
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	list, err := c.md.Trending(r.Context(), limit)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stocks": list})
}

func (c *QuoteController) quote(w http.ResponseWriter, r *http.Request) {
	symbol := chi.URLParam(r, "symbol")
	if symbol == "" {
		writeErr(w, service.ValidationError{Msg: "symbol required"})
		return
	}
	q, err := c.md.Quote(r.Context(), symbol)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, q)
}

func (c *QuoteController) createAlert(w http.ResponseWriter, r *http.Request) {
	var req service.CreateAlertRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, service.ValidationError{Msg: "invalid JSON body"})
		return
	}
	id, err := c.alerts.Create(r.Context(), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}
