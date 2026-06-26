package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"bourse/internal/cache"
	"bourse/internal/model"
	"bourse/internal/repository"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TradingService holds the event-sourced trading domain logic.
type TradingService struct {
	pool       *pgxpool.Pool
	portfolios *repository.PortfolioRepository
	entries    *repository.EntryRepository
	orders     *repository.OrderRepository
	jobs       *repository.JobRepository
	md         *MarketDataService
	cache      *cache.Cache
}

func NewTradingService(pool *pgxpool.Pool, p *repository.PortfolioRepository, e *repository.EntryRepository,
	o *repository.OrderRepository, j *repository.JobRepository, md *MarketDataService, c *cache.Cache) *TradingService {
	return &TradingService{pool: pool, portfolios: p, entries: e, orders: o, jobs: j, md: md, cache: c}
}

// runTx runs fn inside a serializable-enough transaction with rollback on error.
func (s *TradingService) runTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) // no-op if committed
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CreatePortfolio creates a paper-trading account seeded with cash (in paise).
func (s *TradingService) CreatePortfolio(ctx context.Context, name string, seedPaise int64) (uuid.UUID, error) {
	if seedPaise < 0 {
		return uuid.Nil, ValidationError{"seed cash cannot be negative"}
	}
	var id uuid.UUID
	err := s.runTx(ctx, func(tx pgx.Tx) error {
		var err error
		id, err = s.portfolios.Create(ctx, tx, name)
		if err != nil {
			return err
		}
		if seedPaise > 0 {
			if _, err := s.portfolios.LockForUpdate(ctx, tx, id); err != nil {
				return err
			}
			entry := model.Entry{
				TradeID:     uuid.New(),
				PortfolioID: id,
				Instrument:  "CASH",
				Direction:   1,
				Quantity:    seedPaise,
				Seq:         1,
			}
			if err := s.entries.Insert(ctx, tx, entry); err != nil {
				return err
			}
			if err := s.portfolios.BumpSeq(ctx, tx, id, 1); err != nil {
				return err
			}
		}
		return nil
	})
	return id, err
}

// PlaceOrderRequest is the input to PlaceOrder.
type PlaceOrderRequest struct {
	PortfolioID    uuid.UUID `json:"portfolio_id"`
	Side           string    `json:"side"`
	Instrument     string    `json:"instrument"`
	Quantity       int64     `json:"quantity"`
	Type           string    `json:"type"`
	LimitPrice     *int64    `json:"limit_price,omitempty"`
	IdempotencyKey string    `json:"idempotency_key,omitempty"`
}

// PlaceOrder validates and accepts an order, then enqueues its execution in the
// SAME transaction (transactional outbox): the order and its execution job can
// never drift apart. Execution itself happens asynchronously in a worker.
func (s *TradingService) PlaceOrder(ctx context.Context, req PlaceOrderRequest) (model.Order, error) {
	if req.Side != "buy" && req.Side != "sell" {
		return model.Order{}, ValidationError{"side must be 'buy' or 'sell'"}
	}
	if req.Type == "" {
		req.Type = "market"
	}
	if req.Type != "market" && req.Type != "limit" {
		return model.Order{}, ValidationError{"type must be 'market' or 'limit'"}
	}
	if req.Type == "limit" && (req.LimitPrice == nil || *req.LimitPrice <= 0) {
		return model.Order{}, ValidationError{"limit orders require a positive limit_price"}
	}
	if req.Quantity <= 0 {
		return model.Order{}, ValidationError{"quantity must be positive"}
	}
	if req.Instrument == "" || req.Instrument == "CASH" {
		return model.Order{}, ValidationError{"invalid instrument"}
	}

	// Idempotency: a repeated submit with the same key returns the original order.
	if req.IdempotencyKey != "" {
		if existing, err := s.orders.GetByIdempotencyKey(ctx, s.pool, req.IdempotencyKey); err == nil {
			return existing, nil
		} else if !errors.Is(err, repository.ErrNotFound) {
			return model.Order{}, err
		}
	}

	exists, err := s.portfolios.Exists(ctx, s.pool, req.PortfolioID)
	if err != nil {
		return model.Order{}, err
	}
	if !exists {
		return model.Order{}, ValidationError{"portfolio not found"}
	}

	order := model.Order{
		ID:             uuid.New(),
		PortfolioID:    req.PortfolioID,
		IdempotencyKey: req.IdempotencyKey,
		Side:           req.Side,
		Instrument:     req.Instrument,
		Quantity:       req.Quantity,
		Type:           req.Type,
		LimitPrice:     req.LimitPrice,
		Status:         "pending",
	}

	err = s.runTx(ctx, func(tx pgx.Tx) error {
		if err := s.orders.Create(ctx, tx, order); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]string{"order_id": order.ID.String()})
		_, err := s.jobs.Enqueue(ctx, tx, model.JobExecuteOrder, payload, 10, time.Now(), 5)
		return err
	})
	if err != nil {
		return model.Order{}, err
	}
	return order, nil
}

// ExecuteOrder is invoked by a worker. It is idempotent: if the order is no
// longer pending it returns without side effects. The fill is written as a
// balanced double-entry trade inside a single transaction that locks the
// portfolio row, so concurrent executions cannot corrupt balances.
func (s *TradingService) ExecuteOrder(ctx context.Context, orderID uuid.UUID) error {
	order, err := s.orders.Get(ctx, s.pool, orderID)
	if err != nil {
		return err
	}
	if order.Status != "pending" {
		return nil // already executed: idempotent no-op
	}

	quote, err := s.md.Quote(ctx, order.Instrument)
	if err != nil {
		return fmt.Errorf("price lookup failed: %w", err) // retryable
	}

	// Determine fill price and marketability for limit orders.
	fillPrice := quote.Price
	if order.Type == "limit" {
		switch order.Side {
		case "buy":
			if quote.Price > *order.LimitPrice {
				return s.reject(ctx, order.ID, "limit buy not marketable")
			}
		case "sell":
			if quote.Price < *order.LimitPrice {
				return s.reject(ctx, order.ID, "limit sell not marketable")
			}
		}
		fillPrice = *order.LimitPrice
	}

	return s.runTx(ctx, func(tx pgx.Tx) error {
		seq, err := s.portfolios.LockForUpdate(ctx, tx, order.PortfolioID)
		if err != nil {
			return err
		}
		holdings, err := s.entries.Holdings(ctx, tx, order.PortfolioID, nil)
		if err != nil {
			return err
		}
		cost := order.Quantity * fillPrice

		if order.Side == "buy" {
			if holdings["CASH"] < cost {
				return s.orders.SetStatus(ctx, tx, order.ID, "rejected",
					strptr(fmt.Sprintf("insufficient cash: need %d have %d", cost, holdings["CASH"])))
			}
		} else { // sell
			if holdings[order.Instrument] < order.Quantity {
				return s.orders.SetStatus(ctx, tx, order.ID, "rejected",
					strptr(fmt.Sprintf("insufficient shares: need %d have %d", order.Quantity, holdings[order.Instrument])))
			}
		}

		tradeID := uuid.New()
		var stockDir, cashDir int16
		if order.Side == "buy" {
			stockDir, cashDir = 1, -1
		} else {
			stockDir, cashDir = -1, 1
		}
		price := fillPrice
		stockLeg := model.Entry{TradeID: tradeID, PortfolioID: order.PortfolioID,
			Instrument: order.Instrument, Direction: stockDir, Quantity: order.Quantity, Price: &price, Seq: seq + 1}
		cashLeg := model.Entry{TradeID: tradeID, PortfolioID: order.PortfolioID,
			Instrument: "CASH", Direction: cashDir, Quantity: cost, Seq: seq + 2}

		// Double-entry invariant: signed values must net to zero.
		if stockLeg.SignedValue()+cashLeg.SignedValue() != 0 {
			return fmt.Errorf("double-entry invariant violated")
		}
		if err := s.entries.Insert(ctx, tx, stockLeg); err != nil {
			return err
		}
		if err := s.entries.Insert(ctx, tx, cashLeg); err != nil {
			return err
		}
		if err := s.portfolios.BumpSeq(ctx, tx, order.PortfolioID, 2); err != nil {
			return err
		}
		if err := s.orders.SetFilled(ctx, tx, order.ID, fillPrice); err != nil {
			return err
		}
		// Schedule T+1 settlement (delayed job) as part of the same tx.
		payload, _ := json.Marshal(map[string]string{"order_id": order.ID.String()})
		if _, err := s.jobs.Enqueue(ctx, tx, model.JobSettle, payload, 0, time.Now().Add(24*time.Hour), 3); err != nil {
			return err
		}
		// Invalidate the cached view; done after the row writes, before commit
		// returns is fine because a miss simply recomputes.
		_ = s.cache.InvalidateSnapshot(ctx, order.PortfolioID)
		return nil
	})
}

func (s *TradingService) reject(ctx context.Context, id uuid.UUID, reason string) error {
	return s.orders.SetStatus(ctx, s.pool, id, "rejected", &reason)
}

// CancelOrder cancels an order that has not yet executed.
func (s *TradingService) CancelOrder(ctx context.Context, id uuid.UUID) (model.Order, error) {
	order, err := s.orders.Get(ctx, s.pool, id)
	if err != nil {
		return model.Order{}, err
	}
	if order.Status != "pending" {
		return order, InvariantError{"only pending orders can be cancelled"}
	}
	reason := "cancelled by user"
	if err := s.orders.SetStatus(ctx, s.pool, id, "cancelled", &reason); err != nil {
		return model.Order{}, err
	}
	order.Status = "cancelled"
	return order, nil
}

func (s *TradingService) GetOrder(ctx context.Context, id uuid.UUID) (model.Order, error) {
	return s.orders.Get(ctx, s.pool, id)
}

// Portfolio computes the derived view. With asOf nil it serves cache-first; with
// asOf set it always recomputes from the event stream (point-in-time query).
func (s *TradingService) Portfolio(ctx context.Context, id uuid.UUID, asOf *time.Time) (model.PortfolioView, error) {
	if asOf == nil {
		if v, err := s.cache.GetSnapshot(ctx, id); err == nil {
			return v, nil
		}
	}
	exists, err := s.portfolios.Exists(ctx, s.pool, id)
	if err != nil {
		return model.PortfolioView{}, err
	}
	if !exists {
		return model.PortfolioView{}, repository.ErrNotFound
	}

	holdings, err := s.entries.Holdings(ctx, s.pool, id, asOf)
	if err != nil {
		return model.PortfolioView{}, err
	}

	view := model.PortfolioView{PortfolioID: id, AsOf: time.Now().UTC()}
	if asOf != nil {
		view.AsOf = *asOf
	}
	view.Cash = holdings["CASH"]

	for instrument, qty := range holdings {
		if instrument == "CASH" || qty == 0 {
			continue
		}
		q, err := s.md.Quote(ctx, instrument)
		price := int64(0)
		if err == nil {
			price = q.Price
		}
		mv := qty * price
		view.Positions = append(view.Positions, model.Position{
			Instrument: instrument, Quantity: qty, Price: price, MarketValue: mv,
		})
		view.MarketValue += mv
	}
	sort.Slice(view.Positions, func(i, j int) bool {
		return view.Positions[i].Instrument < view.Positions[j].Instrument
	})
	view.TotalValue = view.Cash + view.MarketValue

	if asOf == nil {
		_ = s.cache.SetSnapshot(ctx, view)
	}
	return view, nil
}

// History returns the full audit trail (ordered event stream) plus recent orders.
func (s *TradingService) History(ctx context.Context, id uuid.UUID) ([]model.Entry, []model.Order, error) {
	entries, err := s.entries.List(ctx, s.pool, id)
	if err != nil {
		return nil, nil, err
	}
	orders, err := s.orders.ListByPortfolio(ctx, s.pool, id, 100)
	if err != nil {
		return nil, nil, err
	}
	return entries, orders, nil
}

func strptr(s string) *string { return &s }
