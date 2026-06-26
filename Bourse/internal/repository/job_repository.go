package repository

import (
	"context"
	"errors"
	"time"

	"bourse/internal/model"
	"bourse/internal/store"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type JobRepository struct{}

func NewJobRepository() *JobRepository { return &JobRepository{} }

// Enqueue inserts a job. Accepts a Querier so it can run inside the same
// transaction as a domain write (transactional outbox pattern).
func (r *JobRepository) Enqueue(ctx context.Context, q store.Querier, jobType string, payload []byte, priority int, runAt time.Time, maxAttempts int) (uuid.UUID, error) {
	id := uuid.New()
	// payload is passed as a string + cast to jsonb so pgx does not encode it as
	// bytea (which would fail against a jsonb column).
	_, err := q.Exec(ctx,
		`INSERT INTO jobs (id, type, payload, priority, run_at, max_attempts)
		 VALUES ($1,$2,$3::jsonb,$4,$5,$6)`,
		id, jobType, string(payload), priority, runAt, maxAttempts)
	return id, err
}

// Claim atomically leases the next runnable job. The single UPDATE...RETURNING
// with a FOR UPDATE SKIP LOCKED subquery guarantees no two workers grab the same
// job, even under high concurrency, without an explicit transaction. attempts is
// incremented on claim so a crashed worker's reclaim still counts toward the cap.
func (r *JobRepository) Claim(ctx context.Context, q store.Querier, leaseSeconds int) (*model.Job, error) {
	var j model.Job
	err := q.QueryRow(ctx,
		`UPDATE jobs SET status='inflight',
		        leased_until = now() + ($1 * interval '1 second'),
		        attempts = attempts + 1
		 WHERE id = (
		     SELECT id FROM jobs
		     WHERE status='queued' AND run_at <= now()
		     ORDER BY priority DESC, run_at ASC
		     FOR UPDATE SKIP LOCKED
		     LIMIT 1
		 )
		 RETURNING id, type, payload, priority, run_at, status, attempts, max_attempts, leased_until, last_error, created_at`,
		leaseSeconds).
		Scan(&j.ID, &j.Type, &j.Payload, &j.Priority, &j.RunAt, &j.Status,
			&j.Attempts, &j.MaxAttempts, &j.LeasedUntil, &j.LastError, &j.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil // nothing to do
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

// Complete marks a job done.
func (r *JobRepository) Complete(ctx context.Context, q store.Querier, id uuid.UUID) error {
	_, err := q.Exec(ctx, `UPDATE jobs SET status='done', leased_until=NULL WHERE id=$1`, id)
	return err
}

// Retry reschedules a failed job for a later run with the error recorded.
func (r *JobRepository) Retry(ctx context.Context, q store.Querier, id uuid.UUID, runAt time.Time, errMsg string) error {
	_, err := q.Exec(ctx,
		`UPDATE jobs SET status='queued', run_at=$2, leased_until=NULL, last_error=$3 WHERE id=$1`,
		id, runAt, errMsg)
	return err
}

// MoveToDead copies a job to the dead-letter table and removes it from jobs.
func (r *JobRepository) MoveToDead(ctx context.Context, pool store.Querier, j *model.Job, errMsg string) error {
	if _, err := pool.Exec(ctx,
		`INSERT INTO dead_letters (id, type, payload, attempts, last_error)
		 VALUES ($1,$2,$3::jsonb,$4,$5)`, j.ID, j.Type, string(j.Payload), j.Attempts, errMsg); err != nil {
		return err
	}
	_, err := pool.Exec(ctx, `DELETE FROM jobs WHERE id=$1`, j.ID)
	return err
}

// ReapExpired requeues jobs whose lease expired (worker likely died); jobs that
// have already exhausted their attempts are sent to the dead-letter table.
func (r *JobRepository) ReapExpired(ctx context.Context, q store.Querier) (requeued int64, dead int64, err error) {
	// Dead first: exhausted attempts.
	dl, err := q.Exec(ctx,
		`WITH moved AS (
		     DELETE FROM jobs
		     WHERE status='inflight' AND leased_until < now() AND attempts >= max_attempts
		     RETURNING id, type, payload, attempts
		 )
		 INSERT INTO dead_letters (id, type, payload, attempts, last_error)
		 SELECT id, type, payload, attempts, 'lease expired (max attempts reached)' FROM moved`)
	if err != nil {
		return 0, 0, err
	}
	rq, err := q.Exec(ctx,
		`UPDATE jobs SET status='queued', leased_until=NULL,
		        last_error='lease expired, requeued'
		 WHERE status='inflight' AND leased_until < now()`)
	if err != nil {
		return 0, 0, err
	}
	return rq.RowsAffected(), dl.RowsAffected(), nil
}

// ListDead returns dead-letter rows for inspection.
func (r *JobRepository) ListDead(ctx context.Context, q store.Querier, limit int) ([]model.Job, error) {
	rows, err := q.Query(ctx,
		`SELECT id, type, payload, attempts, last_error, died_at FROM dead_letters
		 ORDER BY died_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Job
	for rows.Next() {
		var j model.Job
		if err := rows.Scan(&j.ID, &j.Type, &j.Payload, &j.Attempts, &j.LastError, &j.CreatedAt); err != nil {
			return nil, err
		}
		j.Status = "dead"
		out = append(out, j)
	}
	return out, rows.Err()
}

// Replay moves a dead-letter row back into the live queue with a reset attempt
// count.
func (r *JobRepository) Replay(ctx context.Context, q store.Querier, id uuid.UUID) error {
	tag, err := q.Exec(ctx,
		`WITH revived AS (
		     DELETE FROM dead_letters WHERE id=$1
		     RETURNING id, type, payload
		 )
		 INSERT INTO jobs (id, type, payload, status, attempts, run_at)
		 SELECT id, type, payload, 'queued', 0, now() FROM revived`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Stats reports queue depth, in-flight count, and dead-letter count.
type QueueStats struct {
	Queued   int64 `json:"queued"`
	Inflight int64 `json:"inflight"`
	Done     int64 `json:"done"`
	Dead     int64 `json:"dead"`
}

func (r *JobRepository) Stats(ctx context.Context, q store.Querier) (QueueStats, error) {
	var s QueueStats
	err := q.QueryRow(ctx,
		`SELECT
		   COALESCE(SUM(CASE WHEN status='queued'   THEN 1 ELSE 0 END),0)::bigint,
		   COALESCE(SUM(CASE WHEN status='inflight' THEN 1 ELSE 0 END),0)::bigint,
		   COALESCE(SUM(CASE WHEN status='done'     THEN 1 ELSE 0 END),0)::bigint
		 FROM jobs`).Scan(&s.Queued, &s.Inflight, &s.Done)
	if err != nil {
		return s, err
	}
	if err := q.QueryRow(ctx, `SELECT COUNT(*) FROM dead_letters`).Scan(&s.Dead); err != nil {
		return s, err
	}
	return s, nil
}

// HasActive reports whether a queued or in-flight job of the given type exists.
// Used to avoid spawning duplicate recurring pollers across worker restarts.
func (r *JobRepository) HasActive(ctx context.Context, q store.Querier, jobType string) (bool, error) {
	var n int
	err := q.QueryRow(ctx,
		`SELECT COUNT(*) FROM jobs WHERE type=$1 AND status IN ('queued','inflight')`, jobType).Scan(&n)
	return n > 0, err
}

// MarkProcessed records an idempotency key. Returns false if the key already
// existed (meaning the side effect already ran and must not run again).
func (r *JobRepository) MarkProcessed(ctx context.Context, q store.Querier, key string) (bool, error) {
	tag, err := q.Exec(ctx,
		`INSERT INTO processed_keys (key) VALUES ($1) ON CONFLICT (key) DO NOTHING`, key)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}
