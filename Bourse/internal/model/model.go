package model

import (
	"time"

	"github.com/google/uuid"
)

// Entry is a single, immutable leg of a trade in the append-only event stream.
// Every trade produces two entries whose signed values net to zero (double-entry).
// For a stock leg, Quantity is shares and Price is cents/share. For the CASH leg,
// Quantity is the amount in cents and Price is nil.
type Entry struct {
	ID          int64     `json:"id"`
	TradeID     uuid.UUID `json:"trade_id"`
	PortfolioID uuid.UUID `json:"portfolio_id"`
	Instrument  string    `json:"instrument"` // "CASH" or a ticker like "AAPL"
	Direction   int16     `json:"direction"`  // +1 in, -1 out
	Quantity    int64     `json:"quantity"`
	Price       *int64    `json:"price,omitempty"` // cents/share; nil for cash
	CreatedAt   time.Time `json:"created_at"`
	Seq         int64     `json:"seq"`
}

// SignedValue returns the cents value of the entry signed by direction. For a
// cash leg the per-unit price is 1 (quantity already in cents).
func (e Entry) SignedValue() int64 {
	unit := int64(1)
	if e.Price != nil {
		unit = *e.Price
	}
	return int64(e.Direction) * e.Quantity * unit
}

// Order is a user instruction to buy or sell. Its fill is recorded as entries;
// Status is derived from execution.
type Order struct {
	ID             uuid.UUID `json:"id"`
	PortfolioID    uuid.UUID `json:"portfolio_id"`
	IdempotencyKey string    `json:"idempotency_key,omitempty"`
	Side           string    `json:"side"`       // buy | sell
	Instrument     string    `json:"instrument"` // ticker
	Quantity       int64     `json:"quantity"`   // shares
	Type           string    `json:"type"`       // market | limit
	LimitPrice     *int64    `json:"limit_price,omitempty"`
	Status         string    `json:"status"` // pending|filled|rejected|cancelled|settled
	FillPrice      *int64    `json:"fill_price,omitempty"`
	Reason         *string   `json:"reason,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// Job is a unit of background work stored durably in Postgres.
type Job struct {
	ID          uuid.UUID  `json:"id"`
	Type        string     `json:"type"`
	Payload     []byte     `json:"payload"`
	Priority    int        `json:"priority"`
	RunAt       time.Time  `json:"run_at"`
	Status      string     `json:"status"` // queued|inflight|done|dead
	Attempts    int        `json:"attempts"`
	MaxAttempts int        `json:"max_attempts"`
	LeasedUntil *time.Time `json:"leased_until,omitempty"`
	LastError   *string    `json:"last_error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// Alert fires a webhook when a symbol crosses a price threshold.
type Alert struct {
	ID         uuid.UUID `json:"id"`
	Symbol     string    `json:"symbol"`
	Direction  string    `json:"direction"` // above | below
	Threshold  int64     `json:"threshold"` // cents
	WebhookURL string    `json:"webhook_url"`
	Triggered  bool      `json:"triggered"`
	CreatedAt  time.Time `json:"created_at"`
}

// Quote is a (cached) market price for a symbol.
type Quote struct {
	Symbol string `json:"symbol"`
	Price  int64  `json:"price"` // cents
	AsOf   int64  `json:"as_of"` // unix millis
}

// Position is a derived holding of a single instrument.
type Position struct {
	Instrument string `json:"instrument"`
	Quantity   int64  `json:"quantity"`
	Price      int64  `json:"price"`       // latest cents/share
	MarketValue int64 `json:"market_value"` // cents
}

// PortfolioView is the derived, point-in-time state of a portfolio.
type PortfolioView struct {
	PortfolioID uuid.UUID  `json:"portfolio_id"`
	Cash        int64      `json:"cash"`        // cents
	Positions   []Position `json:"positions"`
	MarketValue int64      `json:"market_value"` // positions only, cents
	TotalValue  int64      `json:"total_value"`  // cash + market value, cents
	AsOf        time.Time  `json:"as_of"`
}

// Job type constants.
const (
	JobExecuteOrder = "execute_order"
	JobSettle       = "settle"
	JobPollQuotes   = "poll_quotes"
	JobAlertWebhook = "alert_webhook"
)
