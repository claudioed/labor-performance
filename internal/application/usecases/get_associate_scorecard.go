package usecases

import (
	"context"

	"github.com/claudioed/labor-performance/internal/application/ports"
	"github.com/claudioed/labor-performance/internal/domain/shared"
)

// GetAssociateScorecard returns the Scorecard read model for one
// associate: task count, mean EfficiencyPct across tasks that have one,
// and a per-TaskType breakdown. Returns ErrAssociateNotFound only when
// this service has never recorded a single TaskPerformance row for
// associateId — an associate with 1+ rows but an all-nil EfficiencyPct
// still returns a Scorecard with a nil MeanEfficiencyPct, not an error.
type GetAssociateScorecard struct {
	Performances ports.PerformanceRepo
}

func (uc *GetAssociateScorecard) Execute(ctx context.Context, associateId shared.AssociateId) (ports.Scorecard, error) {
	exists, err := uc.Performances.ExistsByAssociateID(ctx, associateId)
	if err != nil {
		return ports.Scorecard{}, err
	}
	if !exists {
		return ports.Scorecard{}, ErrAssociateNotFound
	}
	return uc.Performances.ScorecardFor(ctx, associateId)
}
