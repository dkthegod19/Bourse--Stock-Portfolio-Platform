package controller

import (
	"net/http"
	"time"

	"bourse/internal/service"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// PortfolioController handles portfolio creation and views.
type PortfolioController struct {
	trading *service.TradingService
}

func NewPortfolioController(t *service.TradingService) *PortfolioController {
	return &PortfolioController{trading: t}
}

func (c *PortfolioController) Routes(r chi.Router) {
	r.Post("/portfolios", c.create)
	r.Get("/portfolios/{id}", c.get)
	r.Get("/portfolios/{id}/history", c.history)
}

type createPortfolioReq struct {
	Name      string `json:"name"`
	SeedCents int64  `json:"seed_cents"`
}

func (c *PortfolioController) create(w http.ResponseWriter, r *http.Request) {
	var req createPortfolioReq
	if err := decode(r, &req); err != nil {
		writeErr(w, service.ValidationError{Msg: "invalid JSON body"})
		return
	}
	if req.Name == "" {
		req.Name = "default"
	}
	id, err := c.trading.CreatePortfolio(r.Context(), req.Name, req.SeedCents)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "name": req.Name, "seed_cents": req.SeedCents})
}

func (c *PortfolioController) get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, service.ValidationError{Msg: "invalid portfolio id"})
		return
	}
	var asOf *time.Time
	if raw := r.URL.Query().Get("as_of"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeErr(w, service.ValidationError{Msg: "as_of must be RFC3339"})
			return
		}
		asOf = &t
	}
	view, err := c.trading.Portfolio(r.Context(), id, asOf)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (c *PortfolioController) history(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, service.ValidationError{Msg: "invalid portfolio id"})
		return
	}
	entries, orders, err := c.trading.History(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries, "orders": orders})
}
