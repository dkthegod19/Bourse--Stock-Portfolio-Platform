package controller

import (
	"net/http"

	"bourse/internal/ratelimit"
	"bourse/internal/service"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// AdminController exposes queue inspection/control and rate-limit configuration.
type AdminController struct {
	queue   *service.QueueService
	limiter *ratelimit.Limiter
}

func NewAdminController(q *service.QueueService, l *ratelimit.Limiter) *AdminController {
	return &AdminController{queue: q, limiter: l}
}

func (c *AdminController) Routes(r chi.Router) {
	r.Get("/admin/queue/stats", c.queueStats)
	r.Get("/admin/queue/dead", c.deadLetters)
	r.Post("/admin/queue/dead/{id}/replay", c.replay)
	r.Get("/admin/limits/{key}", c.getLimit)
	r.Put("/admin/limits/{key}", c.setLimit)
}

func (c *AdminController) queueStats(w http.ResponseWriter, r *http.Request) {
	stats, err := c.queue.Stats(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (c *AdminController) deadLetters(w http.ResponseWriter, r *http.Request) {
	dead, err := c.queue.ListDead(r.Context(), 50)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"dead_letters": dead})
}

func (c *AdminController) replay(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, service.ValidationError{Msg: "invalid job id"})
		return
	}
	if err := c.queue.Replay(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"replayed": id})
}

func (c *AdminController) getLimit(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	cfg, err := c.limiter.GetConfig(r.Context(), key)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (c *AdminController) setLimit(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	var cfg ratelimit.Config
	if err := decode(r, &cfg); err != nil {
		writeErr(w, service.ValidationError{Msg: "invalid JSON body"})
		return
	}
	if err := c.limiter.SetConfig(r.Context(), key, cfg); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}
