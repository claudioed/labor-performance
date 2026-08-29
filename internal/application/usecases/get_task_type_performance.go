package usecases

import (
	"context"

	"github.com/claudioed/labor-performance/internal/application/ports"
	"github.com/claudioed/labor-performance/internal/domain/shared"
)

// GetTaskTypePerformance returns the fleet-wide (all-associates) read
// model for one TaskType: task count and mean EfficiencyPct across rows
// that have one. Unlike GetAssociateScorecard, this always returns
// successfully (task count 0 for a TaskType this service has never seen)
// — there is no "not found" case, since a TaskType is a closed, known
// enum rather than an arbitrary caller-supplied identity.
type GetTaskTypePerformance struct {
	Performances ports.PerformanceRepo
}

func (uc *GetTaskTypePerformance) Execute(ctx context.Context, taskType shared.TaskType) (ports.TaskTypePerformance, error) {
	return uc.Performances.TaskTypePerformanceFor(ctx, taskType)
}
