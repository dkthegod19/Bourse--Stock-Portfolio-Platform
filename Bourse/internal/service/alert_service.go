package service

import (
	"context"

	"bourse/internal/model"
	"bourse/internal/repository"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AlertService manages price alerts.
type AlertService struct {
	pool   *pgxpool.Pool
	alerts *repository.AlertRepository
}

func NewAlertService(pool *pgxpool.Pool, alerts *repository.AlertRepository) *AlertService {
	return &AlertService{pool: pool, alerts: alerts}
}

type CreateAlertRequest struct {
	Symbol     string `json:"symbol"`
	Direction  string `json:"direction"` // above | below
	Threshold  int64  `json:"threshold"` // cents
	WebhookURL string `json:"webhook_url"`
}

func (s *AlertService) Create(ctx context.Context, req CreateAlertRequest) (uuid.UUID, error) {
	if req.Symbol == "" {
		return uuid.Nil, ValidationError{"symbol is required"}
	}
	if req.Direction != "above" && req.Direction != "below" {
		return uuid.Nil, ValidationError{"direction must be 'above' or 'below'"}
	}
	if req.Threshold <= 0 {
		return uuid.Nil, ValidationError{"threshold must be positive (cents)"}
	}
	if req.WebhookURL == "" {
		return uuid.Nil, ValidationError{"webhook_url is required"}
	}
	return s.alerts.Create(ctx, s.pool, model.Alert{
		Symbol:     req.Symbol,
		Direction:  req.Direction,
		Threshold:  req.Threshold,
		WebhookURL: req.WebhookURL,
	})
}
