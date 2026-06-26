package controller

import (
	"net/http"

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
	r.Get("/quotes/{symbol}", c.quote)
	r.Post("/alerts", c.createAlert)
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
