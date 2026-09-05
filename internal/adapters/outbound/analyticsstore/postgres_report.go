package analyticsstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/claudioed/labor-performance/internal/analytics/report"
)

// PostgresReport is the READER implementation of report.ReportStore,
// backed by a pgxpool over the analytical database. The pool it is given
// is expected to be pinned to a read-only role /
// default_transaction_read_only=on, so a bug in the reader cannot mutate
// the read model (ADR-0007). It never issues writes.
type PostgresReport struct {
	pool *pgxpool.Pool
}

// NewPostgresReport constructs a PostgresReport over pool.
func NewPostgresReport(pool *pgxpool.Pool) *PostgresReport {
	return &PostgresReport{pool: pool}
}

// Query returns the report covering q. From is inclusive, To is
// exclusive; an empty TaskType disables that filter. Aggregation (the
// per-TaskType breakdown and the window totals) is delegated to
// report.Build, so this adapter and MemoryStore cannot disagree about
// what a mean over zero observations is.
func (r *PostgresReport) Query(ctx context.Context, q report.ReportQuery) (report.LaborPerformanceReport, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT task_type, hour_bucket,
			tasks_recorded, tasks_scored, efficiency_pct_sum,
			tasks_measured, actual_seconds_sum,
			standards_defined, standards_revised
		 FROM labor_performance_rollup
		 WHERE hour_bucket >= $1 AND hour_bucket < $2
		   AND ($3 = '' OR task_type = $3)
		 ORDER BY hour_bucket, task_type`,
		q.From, q.To, q.TaskType)
	if err != nil {
		return report.LaborPerformanceReport{}, fmt.Errorf("analyticsstore: query rollup: %w", err)
	}
	defer rows.Close()

	var out []report.Row
	for rows.Next() {
		var (
			row    report.Row
			bucket time.Time
		)
		if err := rows.Scan(
			&row.Key.TaskType, &bucket,
			&row.TasksRecorded, &row.TasksScored, &row.EfficiencyPctSum,
			&row.TasksMeasured, &row.ActualSecondsSum,
			&row.StandardsDefined, &row.StandardsRevised,
		); err != nil {
			return report.LaborPerformanceReport{}, fmt.Errorf("analyticsstore: scan row: %w", err)
		}
		row.Key.HourBucket = bucket.UTC()
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return report.LaborPerformanceReport{}, fmt.Errorf("analyticsstore: iterate rows: %w", err)
	}
	return report.Build(out), nil
}

// FreshnessLag returns now minus the most recent event's occurred_at,
// i.e. how far the read model trails the event stream. Zero when the read
// model is empty or (defensively) when the latest event is future-dated.
func (r *PostgresReport) FreshnessLag(ctx context.Context) (time.Duration, error) {
	// max() over an empty table returns a single NULL row (not zero
	// rows), so scan into a nullable *time.Time and treat NULL as "read
	// model empty".
	var latest *time.Time
	err := r.pool.QueryRow(ctx,
		`SELECT max(occurred_at) FROM analytics_processed_events`).Scan(&latest)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return 0, nil
	case err != nil:
		return 0, fmt.Errorf("analyticsstore: freshness query: %w", err)
	}
	if latest == nil || latest.IsZero() {
		return 0, nil
	}
	lag := time.Since(*latest)
	if lag < 0 {
		return 0, nil
	}
	return lag, nil
}

// Compile-time assertion that PostgresReport satisfies the read port.
var _ report.ReportStore = (*PostgresReport)(nil)
