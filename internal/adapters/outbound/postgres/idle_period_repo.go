package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/claudioed/labor-performance/internal/domain/idleness"
	"github.com/claudioed/labor-performance/internal/domain/shared"
)

// IdlePeriodRepo is a pgxpool-backed implementation of
// ports.IdlePeriodRepo.
type IdlePeriodRepo struct {
	pool *pgxpool.Pool
}

// NewIdlePeriodRepo constructs an IdlePeriodRepo over pool.
func NewIdlePeriodRepo(pool *pgxpool.Pool) *IdlePeriodRepo {
	return &IdlePeriodRepo{pool: pool}
}

func (r *IdlePeriodRepo) Save(ctx context.Context, p *idleness.IdlePeriod) error {
	_, err := querierFrom(ctx, r.pool).Exec(ctx, `
		INSERT INTO idle_periods (associate_id, task_type, started_at, ended_at, seconds, capped)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, string(p.AssociateId()), string(p.TaskType()), p.StartedAt(), p.EndedAt(), p.Seconds(), p.Capped())
	return err
}

func (r *IdlePeriodRepo) SumByTaskType(ctx context.Context, taskType shared.TaskType, since time.Time) (int64, int, error) {
	var sum int64
	var count int
	err := querierFrom(ctx, r.pool).QueryRow(ctx, `
		SELECT COALESCE(SUM(seconds), 0), COUNT(*)
		FROM idle_periods
		WHERE task_type = $1 AND ended_at >= $2
	`, string(taskType), since).Scan(&sum, &count)
	return sum, count, err
}

func (r *IdlePeriodRepo) SumByAssociate(ctx context.Context, associateId shared.AssociateId, since time.Time) (int64, int, error) {
	var sum int64
	var count int
	err := querierFrom(ctx, r.pool).QueryRow(ctx, `
		SELECT COALESCE(SUM(seconds), 0), COUNT(*)
		FROM idle_periods
		WHERE associate_id = $1 AND ended_at >= $2
	`, string(associateId), since).Scan(&sum, &count)
	return sum, count, err
}

func (r *IdlePeriodRepo) LastEndedAtByAssociate(ctx context.Context, associateId shared.AssociateId) (time.Time, error) {
	var lastEndedAt *time.Time
	err := querierFrom(ctx, r.pool).QueryRow(ctx, `
		SELECT MAX(ended_at)
		FROM idle_periods
		WHERE associate_id = $1
	`, string(associateId)).Scan(&lastEndedAt)
	if err != nil {
		return time.Time{}, err
	}
	if lastEndedAt == nil {
		return time.Time{}, nil
	}
	return *lastEndedAt, nil
}

func (r *IdlePeriodRepo) DistinctAssociatesByTaskType(ctx context.Context, taskType shared.TaskType, since time.Time) (int, error) {
	var count int
	err := querierFrom(ctx, r.pool).QueryRow(ctx, `
		SELECT COUNT(DISTINCT associate_id)
		FROM idle_periods
		WHERE task_type = $1 AND ended_at >= $2
	`, string(taskType), since).Scan(&count)
	return count, err
}
