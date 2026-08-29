package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ProcessedEventRepo is a pgxpool-backed implementation of
// ports.ProcessedEvents.
type ProcessedEventRepo struct {
	pool *pgxpool.Pool
}

// NewProcessedEventRepo constructs a ProcessedEventRepo over pool.
func NewProcessedEventRepo(pool *pgxpool.Pool) *ProcessedEventRepo {
	return &ProcessedEventRepo{pool: pool}
}

func (r *ProcessedEventRepo) MarkProcessed(ctx context.Context, eventId string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO processed_events (event_id) VALUES ($1)
		ON CONFLICT (event_id) DO NOTHING
	`, eventId)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}
