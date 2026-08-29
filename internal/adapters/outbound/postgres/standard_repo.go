package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/claudioed/labor-performance/internal/domain/shared"
	"github.com/claudioed/labor-performance/internal/domain/standard"
)

// StandardRepo is a pgxpool-backed implementation of ports.StandardRepo.
type StandardRepo struct {
	pool *pgxpool.Pool
}

// NewStandardRepo constructs a StandardRepo over pool.
func NewStandardRepo(pool *pgxpool.Pool) *StandardRepo {
	return &StandardRepo{pool: pool}
}

func (r *StandardRepo) Save(ctx context.Context, s *standard.LaborStandard) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO labor_standards (id, task_type, expected_seconds, effective_from, effective_to)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE
		  SET effective_to = EXCLUDED.effective_to
	`, string(s.ID()), string(s.TaskType()), s.ExpectedSeconds(), s.EffectiveFrom(), s.EffectiveTo())
	return err
}

func (r *StandardRepo) FindActiveAsOf(ctx context.Context, taskType shared.TaskType, t time.Time) (*standard.LaborStandard, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, task_type, expected_seconds, effective_from, effective_to
		FROM labor_standards
		WHERE task_type = $1
		  AND effective_from <= $2
		  AND (effective_to IS NULL OR effective_to > $2)
	`, string(taskType), t)
	return scanStandard(row)
}

func (r *StandardRepo) FindCurrentlyActive(ctx context.Context, taskType shared.TaskType) (*standard.LaborStandard, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, task_type, expected_seconds, effective_from, effective_to
		FROM labor_standards
		WHERE task_type = $1 AND effective_to IS NULL
	`, string(taskType))
	return scanStandard(row)
}

func scanStandard(row pgx.Row) (*standard.LaborStandard, error) {
	var (
		id              string
		taskType        string
		expectedSeconds int64
		effectiveFrom   time.Time
		effectiveTo     *time.Time
	)
	err := row.Scan(&id, &taskType, &expectedSeconds, &effectiveFrom, &effectiveTo)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return standard.Rehydrate(shared.StandardId(id), shared.TaskType(taskType), expectedSeconds, effectiveFrom, effectiveTo), nil
}

// NextID mints a standard id.
func (r *StandardRepo) NextID(_ context.Context) (shared.StandardId, error) {
	return shared.StandardId("std-" + newUUID()), nil
}
