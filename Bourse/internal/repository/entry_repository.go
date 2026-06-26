package repository

import (
	"context"
	"time"

	"bourse/internal/model"
	"bourse/internal/store"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type EntryRepository struct{}

func NewEntryRepository() *EntryRepository { return &EntryRepository{} }

// Insert appends a single immutable entry. INSERT-only; entries are never updated.
func (r *EntryRepository) Insert(ctx context.Context, q store.Querier, e model.Entry) error {
	_, err := q.Exec(ctx,
		`INSERT INTO entries (trade_id, portfolio_id, instrument, direction, quantity, price, seq)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		e.TradeID, e.PortfolioID, e.Instrument, e.Direction, e.Quantity, e.Price, e.Seq)
	return err
}

// Holdings returns net quantity per instrument for a portfolio, derived purely
// from the event stream. If asOf is non-nil, only entries up to that time count
// (this powers point-in-time queries). "CASH" net quantity is the cash balance
// in cents.
func (r *EntryRepository) Holdings(ctx context.Context, q store.Querier, portfolioID uuid.UUID, asOf *time.Time) (map[string]int64, error) {
	var rows pgx.Rows
	var err error
	if asOf == nil {
		rows, err = q.Query(ctx,
			`SELECT instrument, COALESCE(SUM(direction::bigint * quantity), 0)::bigint
			 FROM entries WHERE portfolio_id=$1 GROUP BY instrument`, portfolioID)
	} else {
		rows, err = q.Query(ctx,
			`SELECT instrument, COALESCE(SUM(direction::bigint * quantity), 0)::bigint
			 FROM entries WHERE portfolio_id=$1 AND created_at <= $2 GROUP BY instrument`, portfolioID, *asOf)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]int64)
	for rows.Next() {
		var instrument string
		var qty int64
		if err := rows.Scan(&instrument, &qty); err != nil {
			return nil, err
		}
		out[instrument] = qty
	}
	return out, rows.Err()
}

// List returns the ordered event stream for a portfolio (audit trail).
func (r *EntryRepository) List(ctx context.Context, q store.Querier, portfolioID uuid.UUID) ([]model.Entry, error) {
	rows, err := q.Query(ctx,
		`SELECT id, trade_id, portfolio_id, instrument, direction, quantity, price, created_at, seq
		 FROM entries WHERE portfolio_id=$1 ORDER BY seq ASC`, portfolioID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Entry
	for rows.Next() {
		var e model.Entry
		if err := rows.Scan(&e.ID, &e.TradeID, &e.PortfolioID, &e.Instrument,
			&e.Direction, &e.Quantity, &e.Price, &e.CreatedAt, &e.Seq); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// DistinctSymbols returns the set of non-cash instruments ever traded, used to
// decide which quotes to poll.
func (r *EntryRepository) DistinctSymbols(ctx context.Context, q store.Querier) ([]string, error) {
	rows, err := q.Query(ctx,
		`SELECT DISTINCT instrument FROM entries WHERE instrument <> 'CASH'`)
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
