package repository

import (
	"context"
	"errors"

	"bourse/internal/model"
	"bourse/internal/store"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type OrderRepository struct{}

func NewOrderRepository() *OrderRepository { return &OrderRepository{} }

func (r *OrderRepository) Create(ctx context.Context, q store.Querier, o model.Order) error {
	var idem *string
	if o.IdempotencyKey != "" {
		idem = &o.IdempotencyKey
	}
	_, err := q.Exec(ctx,
		`INSERT INTO orders (id, portfolio_id, idempotency_key, side, instrument, quantity, type, limit_price, status)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		o.ID, o.PortfolioID, idem, o.Side, o.Instrument, o.Quantity, o.Type, o.LimitPrice, o.Status)
	return err
}

func (r *OrderRepository) Get(ctx context.Context, q store.Querier, id uuid.UUID) (model.Order, error) {
	var o model.Order
	var idem *string
	err := q.QueryRow(ctx,
		`SELECT id, portfolio_id, idempotency_key, side, instrument, quantity, type, limit_price, status, fill_price, reason, created_at
		 FROM orders WHERE id=$1`, id).
		Scan(&o.ID, &o.PortfolioID, &idem, &o.Side, &o.Instrument, &o.Quantity, &o.Type, &o.LimitPrice, &o.Status, &o.FillPrice, &o.Reason, &o.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, ErrNotFound
	}
	if idem != nil {
		o.IdempotencyKey = *idem
	}
	return o, err
}

// GetByIdempotencyKey returns an existing order for the given key, or ErrNotFound.
func (r *OrderRepository) GetByIdempotencyKey(ctx context.Context, q store.Querier, key string) (model.Order, error) {
	var o model.Order
	var idem *string
	err := q.QueryRow(ctx,
		`SELECT id, portfolio_id, idempotency_key, side, instrument, quantity, type, limit_price, status, fill_price, reason, created_at
		 FROM orders WHERE idempotency_key=$1`, key).
		Scan(&o.ID, &o.PortfolioID, &idem, &o.Side, &o.Instrument, &o.Quantity, &o.Type, &o.LimitPrice, &o.Status, &o.FillPrice, &o.Reason, &o.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, ErrNotFound
	}
	if idem != nil {
		o.IdempotencyKey = *idem
	}
	return o, err
}

// SetFilled marks an order filled at a price.
func (r *OrderRepository) SetFilled(ctx context.Context, q store.Querier, id uuid.UUID, fillPrice int64) error {
	_, err := q.Exec(ctx,
		`UPDATE orders SET status='filled', fill_price=$2, reason=NULL WHERE id=$1`, id, fillPrice)
	return err
}

// SetStatus updates an order's status with an optional human-readable reason.
func (r *OrderRepository) SetStatus(ctx context.Context, q store.Querier, id uuid.UUID, status string, reason *string) error {
	_, err := q.Exec(ctx,
		`UPDATE orders SET status=$2, reason=$3 WHERE id=$1`, id, status, reason)
	return err
}

func (r *OrderRepository) ListByPortfolio(ctx context.Context, q store.Querier, portfolioID uuid.UUID, limit int) ([]model.Order, error) {
	rows, err := q.Query(ctx,
		`SELECT id, portfolio_id, idempotency_key, side, instrument, quantity, type, limit_price, status, fill_price, reason, created_at
		 FROM orders WHERE portfolio_id=$1 ORDER BY created_at DESC LIMIT $2`, portfolioID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Order
	for rows.Next() {
		var o model.Order
		var idem *string
		if err := rows.Scan(&o.ID, &o.PortfolioID, &idem, &o.Side, &o.Instrument, &o.Quantity, &o.Type, &o.LimitPrice, &o.Status, &o.FillPrice, &o.Reason, &o.CreatedAt); err != nil {
			return nil, err
		}
		if idem != nil {
			o.IdempotencyKey = *idem
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
