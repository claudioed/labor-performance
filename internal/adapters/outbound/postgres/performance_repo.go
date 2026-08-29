package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/claudioed/labor-performance/internal/application/ports"
	"github.com/claudioed/labor-performance/internal/domain/performance"
	"github.com/claudioed/labor-performance/internal/domain/shared"
)

// PerformanceRepo is a pgxpool-backed implementation of
// ports.PerformanceRepo.
type PerformanceRepo struct {
	pool *pgxpool.Pool
}

// NewPerformanceRepo constructs a PerformanceRepo over pool.
func NewPerformanceRepo(pool *pgxpool.Pool) *PerformanceRepo {
	return &PerformanceRepo{pool: pool}
}

func (r *PerformanceRepo) Save(ctx context.Context, p *performance.TaskPerformance) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO task_performances
			(event_id, task_id, associate_id, task_type, actual_seconds, standard_seconds_at_completion, efficiency_pct, completed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (event_id) DO NOTHING
	`, p.EventId(), p.TaskId(), string(p.AssociateId()), string(p.TaskType()), p.ActualSeconds(), p.StandardSecondsAtCompletion(), p.EfficiencyPct(), p.CompletedAt())
	return err
}

func (r *PerformanceRepo) ExistsByAssociateID(ctx context.Context, associateId shared.AssociateId) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM task_performances WHERE associate_id = $1)
	`, string(associateId)).Scan(&exists)
	return exists, err
}

func (r *PerformanceRepo) ScorecardFor(ctx context.Context, associateId shared.AssociateId) (ports.Scorecard, error) {
	sc := ports.Scorecard{AssociateId: associateId, ByTaskType: make(map[shared.TaskType]ports.TaskTypeBreakdown)}

	row := r.pool.QueryRow(ctx, `
		SELECT COUNT(*), AVG(efficiency_pct)
		FROM task_performances
		WHERE associate_id = $1
	`, string(associateId))
	var mean *float64
	if err := row.Scan(&sc.TaskCount, &mean); err != nil {
		return ports.Scorecard{}, err
	}
	sc.MeanEfficiencyPct = mean

	rows, err := r.pool.Query(ctx, `
		SELECT task_type, COUNT(*), AVG(efficiency_pct)
		FROM task_performances
		WHERE associate_id = $1
		GROUP BY task_type
	`, string(associateId))
	if err != nil {
		return ports.Scorecard{}, err
	}
	defer rows.Close()

	for rows.Next() {
		var taskType string
		var breakdown ports.TaskTypeBreakdown
		if err := rows.Scan(&taskType, &breakdown.TaskCount, &breakdown.MeanEfficiencyPct); err != nil {
			return ports.Scorecard{}, err
		}
		sc.ByTaskType[shared.TaskType(taskType)] = breakdown
	}
	if err := rows.Err(); err != nil {
		return ports.Scorecard{}, err
	}

	return sc, nil
}

func (r *PerformanceRepo) TaskTypePerformanceFor(ctx context.Context, taskType shared.TaskType) (ports.TaskTypePerformance, error) {
	out := ports.TaskTypePerformance{TaskType: taskType}
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*), AVG(efficiency_pct)
		FROM task_performances
		WHERE task_type = $1
	`, string(taskType)).Scan(&out.TaskCount, &out.MeanEfficiencyPct)
	return out, err
}
