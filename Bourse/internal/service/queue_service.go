package service

import (
	"context"

	"bourse/internal/model"
	"bourse/internal/repository"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// QueueService exposes queue introspection and control for the admin API.
type QueueService struct {
	pool *pgxpool.Pool
	jobs *repository.JobRepository
}

func NewQueueService(pool *pgxpool.Pool, jobs *repository.JobRepository) *QueueService {
	return &QueueService{pool: pool, jobs: jobs}
}

func (s *QueueService) Stats(ctx context.Context) (repository.QueueStats, error) {
	return s.jobs.Stats(ctx, s.pool)
}

func (s *QueueService) ListDead(ctx context.Context, limit int) ([]model.Job, error) {
	if limit <= 0 {
		limit = 50
	}
	return s.jobs.ListDead(ctx, s.pool, limit)
}

func (s *QueueService) Replay(ctx context.Context, id uuid.UUID) error {
	return s.jobs.Replay(ctx, s.pool, id)
}
