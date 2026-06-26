package repository

import (
	"context"
	"errors"

	"bourse/internal/store"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

type PortfolioRepository struct{}

func NewPortfolioRepository() *PortfolioRepository { return &PortfolioRepository{} }

func (r *PortfolioRepository) Create(ctx context.Context, q store.Querier, name string) (uuid.UUID, error) {
	id := uuid.New()
	_, err := q.Exec(ctx,
		`INSERT INTO portfolios (id, name) VALUES ($1, $2)`, id, name)
	return id, err
}

// Exists reports whether a portfolio row is present.
func (r *PortfolioRepository) Exists(ctx context.Context, q store.Querier, id uuid.UUID) (bool, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT 1 FROM portfolios WHERE id=$1`, id).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// LockForUpdate takes a row lock on the portfolio and returns the current
// sequence counter. Called inside a transaction to serialize concurrent trades
// on the same portfolio (prevents lost updates).
func (r *PortfolioRepository) LockForUpdate(ctx context.Context, q store.Querier, id uuid.UUID) (int64, error) {
	var seq int64
	err := q.QueryRow(ctx,
		`SELECT seq_counter FROM portfolios WHERE id=$1 FOR UPDATE`, id).Scan(&seq)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return seq, err
}

// BumpSeq advances the portfolio's sequence counter by n.
func (r *PortfolioRepository) BumpSeq(ctx context.Context, q store.Querier, id uuid.UUID, n int64) error {
	_, err := q.Exec(ctx,
		`UPDATE portfolios SET seq_counter = seq_counter + $2 WHERE id=$1`, id, n)
	return err
}
