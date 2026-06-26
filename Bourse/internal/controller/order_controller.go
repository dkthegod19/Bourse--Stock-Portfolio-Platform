package controller

import (
	"net/http"

	"bourse/internal/service"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// OrderController handles order placement, lookup, and cancellation.
type OrderController struct {
	trading *service.TradingService
}

func NewOrderController(t *service.TradingService) *OrderController {
	return &OrderController{trading: t}
}

func (c *OrderController) Routes(r chi.Router) {
	r.Post("/orders", c.place)
	r.Get("/orders/{id}", c.get)
	r.Delete("/orders/{id}", c.cancel)
}

func (c *OrderController) place(w http.ResponseWriter, r *http.Request) {
	var req service.PlaceOrderRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, service.ValidationError{Msg: "invalid JSON body"})
		return
	}
	// Allow the idempotency key to come from a header too.
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = r.Header.Get("Idempotency-Key")
	}
	order, err := c.trading.PlaceOrder(r.Context(), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, order)
}

func (c *OrderController) get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, service.ValidationError{Msg: "invalid order id"})
		return
	}
	order, err := c.trading.GetOrder(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, order)
}

func (c *OrderController) cancel(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, service.ValidationError{Msg: "invalid order id"})
		return
	}
	order, err := c.trading.CancelOrder(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, order)
}
