package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"time"

	"bourse/internal/model"
	"bourse/internal/repository"
	"bourse/internal/service"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Worker is the background job engine. It claims jobs from the durable queue,
// dispatches them by type, and applies retry/backoff and dead-lettering.
type Worker struct {
	pool       *pgxpool.Pool
	jobs       *repository.JobRepository
	orders     *repository.OrderRepository
	entries    *repository.EntryRepository
	alerts     *repository.AlertRepository
	trading    *service.TradingService
	md         *service.MarketDataService
	leaseSecs  int
	concurrency int
	pollEvery  time.Duration
	httpClient *http.Client
	log        *slog.Logger
}

func New(pool *pgxpool.Pool, jobs *repository.JobRepository, orders *repository.OrderRepository,
	entries *repository.EntryRepository, alerts *repository.AlertRepository,
	trading *service.TradingService, md *service.MarketDataService, leaseSecs int, log *slog.Logger) *Worker {
	return &Worker{
		pool: pool, jobs: jobs, orders: orders, entries: entries, alerts: alerts,
		trading: trading, md: md, leaseSecs: leaseSecs,
		concurrency: 4,
		pollEvery:   15 * time.Second,
		httpClient:  &http.Client{Timeout: 8 * time.Second},
		log:         log,
	}
}

// Run starts the worker pool, the lease reaper, and the recurring quote poller.
// It blocks until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	w.seedPoller(ctx)

	go w.reaper(ctx)

	for i := 0; i < w.concurrency; i++ {
		go w.loop(ctx, i)
	}
	w.log.Info("worker started", "concurrency", w.concurrency, "lease_seconds", w.leaseSecs)
	<-ctx.Done()
	w.log.Info("worker stopping")
}

// seedPoller ensures exactly one recurring poll_quotes job exists.
func (w *Worker) seedPoller(ctx context.Context) {
	active, err := w.jobs.HasActive(ctx, w.pool, model.JobPollQuotes)
	if err != nil {
		w.log.Error("seed poller check failed", "err", err)
		return
	}
	if !active {
		if _, err := w.jobs.Enqueue(ctx, w.pool, model.JobPollQuotes, []byte(`{}`), 0, time.Now(), 1000000); err != nil {
			w.log.Error("seed poller enqueue failed", "err", err)
		}
	}
}

func (w *Worker) loop(ctx context.Context, id int) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		job, err := w.jobs.Claim(ctx, w.pool, w.leaseSecs)
		if err != nil {
			w.log.Error("claim failed", "err", err)
			time.Sleep(time.Second)
			continue
		}
		if job == nil {
			time.Sleep(250 * time.Millisecond) // idle backoff
			continue
		}
		w.process(ctx, job)
	}
}

func (w *Worker) process(ctx context.Context, job *model.Job) {
	err := w.dispatch(ctx, job)
	if err == nil {
		if cErr := w.jobs.Complete(ctx, w.pool, job.ID); cErr != nil {
			w.log.Error("complete failed", "job", job.ID, "err", cErr)
		}
		return
	}

	w.log.Warn("job failed", "job", job.ID, "type", job.Type, "attempt", job.Attempts, "err", err)
	if job.Attempts >= job.MaxAttempts {
		if dErr := w.jobs.MoveToDead(ctx, w.pool, job, err.Error()); dErr != nil {
			w.log.Error("dead-letter failed", "job", job.ID, "err", dErr)
		}
		return
	}
	// Exponential backoff with a cap.
	backoff := time.Duration(math.Min(math.Pow(2, float64(job.Attempts)), 300)) * time.Second
	if rErr := w.jobs.Retry(ctx, w.pool, job.ID, time.Now().Add(backoff), err.Error()); rErr != nil {
		w.log.Error("retry failed", "job", job.ID, "err", rErr)
	}
}

func (w *Worker) dispatch(ctx context.Context, job *model.Job) error {
	switch job.Type {
	case model.JobExecuteOrder:
		return w.handleExecuteOrder(ctx, job)
	case model.JobSettle:
		return w.handleSettle(ctx, job)
	case model.JobPollQuotes:
		return w.handlePollQuotes(ctx, job)
	case model.JobAlertWebhook:
		return w.handleAlertWebhook(ctx, job)
	default:
		return fmt.Errorf("unknown job type %q", job.Type)
	}
}

func (w *Worker) handleExecuteOrder(ctx context.Context, job *model.Job) error {
	var p struct {
		OrderID string `json:"order_id"`
	}
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return err
	}
	id, err := uuid.Parse(p.OrderID)
	if err != nil {
		return err
	}
	return w.trading.ExecuteOrder(ctx, id)
}

func (w *Worker) handleSettle(ctx context.Context, job *model.Job) error {
	var p struct {
		OrderID string `json:"order_id"`
	}
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return err
	}
	id, err := uuid.Parse(p.OrderID)
	if err != nil {
		return err
	}
	order, err := w.orders.Get(ctx, w.pool, id)
	if err != nil {
		return err
	}
	if order.Status == "filled" {
		settled := "settled (T+1)"
		return w.orders.SetStatus(ctx, w.pool, id, "settled", &settled)
	}
	return nil
}

func (w *Worker) handlePollQuotes(ctx context.Context, job *model.Job) error {
	// Symbols to refresh: everything ever traded + symbols with active alerts.
	symSet := map[string]struct{}{}
	traded, err := w.entries.DistinctSymbols(ctx, w.pool)
	if err != nil {
		return err
	}
	for _, s := range traded {
		symSet[s] = struct{}{}
	}
	alertSyms, err := w.alerts.Symbols(ctx, w.pool)
	if err != nil {
		return err
	}
	for _, s := range alertSyms {
		symSet[s] = struct{}{}
	}

	prices := map[string]int64{}
	for sym := range symSet {
		q, err := w.md.Refresh(ctx, sym)
		if err != nil {
			w.log.Warn("quote refresh failed", "symbol", sym, "err", err)
			continue
		}
		prices[sym] = q.Price
	}

	// Fire any alerts that have crossed their threshold.
	active, err := w.alerts.ListActive(ctx, w.pool)
	if err != nil {
		return err
	}
	for _, a := range active {
		price, ok := prices[a.Symbol]
		if !ok {
			continue
		}
		crossed := (a.Direction == "above" && price >= a.Threshold) ||
			(a.Direction == "below" && price <= a.Threshold)
		if !crossed {
			continue
		}
		payload, _ := json.Marshal(map[string]any{
			"alert_id": a.ID.String(), "symbol": a.Symbol, "price": price,
			"threshold": a.Threshold, "direction": a.Direction, "webhook_url": a.WebhookURL,
		})
		if _, err := w.jobs.Enqueue(ctx, w.pool, model.JobAlertWebhook, payload, 5, time.Now(), 5); err != nil {
			w.log.Error("enqueue alert webhook failed", "err", err)
			continue
		}
		_ = w.alerts.MarkTriggered(ctx, w.pool, a.ID)
	}

	// Re-arm the recurring poll.
	if _, err := w.jobs.Enqueue(ctx, w.pool, model.JobPollQuotes, []byte(`{}`), 0, time.Now().Add(w.pollEvery), 1000000); err != nil {
		return err
	}
	return nil
}

func (w *Worker) handleAlertWebhook(ctx context.Context, job *model.Job) error {
	var p struct {
		WebhookURL string `json:"webhook_url"`
	}
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.WebhookURL, bytes.NewReader(job.Payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.httpClient.Do(req)
	if err != nil {
		return err // network failure: retry, eventually dead-letter
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}

// reaper periodically requeues jobs whose lease expired (crashed workers) and
// dead-letters those that have exhausted their attempts.
func (w *Worker) reaper(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rq, dead, err := w.jobs.ReapExpired(ctx, w.pool)
			if err != nil {
				w.log.Error("reaper failed", "err", err)
				continue
			}
			if rq > 0 || dead > 0 {
				w.log.Info("reaped expired leases", "requeued", rq, "dead", dead)
			}
		}
	}
}
