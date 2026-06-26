package repository

import (
	"context"

	"bourse/internal/model"
	"bourse/internal/store"

	"github.com/google/uuid"
)

type AlertRepository struct{}

func NewAlertRepository() *AlertRepository { return &AlertRepository{} }

func (r *AlertRepository) Create(ctx context.Context, q store.Querier, a model.Alert) (uuid.UUID, error) {
	id := uuid.New()
	_, err := q.Exec(ctx,
		`INSERT INTO alerts (id, symbol, direction, threshold, webhook_url)
		 VALUES ($1,$2,$3,$4,$5)`,
		id, a.Symbol, a.Direction, a.Threshold, a.WebhookURL)
	return id, err
}

// ListActive returns alerts that have not yet fired.
func (r *AlertRepository) ListActive(ctx context.Context, q store.Querier) ([]model.Alert, error) {
	rows, err := q.Query(ctx,
		`SELECT id, symbol, direction, threshold, webhook_url, triggered, created_at
		 FROM alerts WHERE triggered=false`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Alert
	for rows.Next() {
		var a model.Alert
		if err := rows.Scan(&a.ID, &a.Symbol, &a.Direction, &a.Threshold, &a.WebhookURL, &a.Triggered, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *AlertRepository) MarkTriggered(ctx context.Context, q store.Querier, id uuid.UUID) error {
	_, err := q.Exec(ctx, `UPDATE alerts SET triggered=true WHERE id=$1`, id)
	return err
}

// Symbols returns the distinct symbols referenced by active alerts.
func (r *AlertRepository) Symbols(ctx context.Context, q store.Querier) ([]string, error) {
	rows, err := q.Query(ctx, `SELECT DISTINCT symbol FROM alerts WHERE triggered=false`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
