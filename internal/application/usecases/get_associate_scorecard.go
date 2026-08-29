package usecases

import (
	"context"

	"github.com/claudioed/labor-performance/internal/application/ports"
	"github.com/claudioed/labor-performance/internal/domain/performance"
	"github.com/claudioed/labor-performance/internal/domain/shared"
)

// recentWindowSize is how many of an associate's most recent
// TaskPerformance rows GetAssociateScorecard pulls to compute Trend and
// CoachingFlag. 10 is large enough to give ClassifyTrend's
// minScoredForTrend=3 room even when some recent rows are unscored
// (nil EfficiencyPct, e.g. pre-migration or no-active-standard rows),
// while staying small enough that this stays a cheap, bounded query
// regardless of an associate's total tenure.
const recentWindowSize = 10

// GetAssociateScorecard returns the Scorecard read model for one
// associate: task count, mean EfficiencyPct across tasks that have one,
// a per-TaskType breakdown, and a Trend/CoachingFlag signal computed
// over their most recent tasks. Returns ErrAssociateNotFound only when
// this service has never recorded a single TaskPerformance row for
// associateId — an associate with 1+ rows but an all-nil EfficiencyPct
// still returns a Scorecard with a nil MeanEfficiencyPct, not an error.
type GetAssociateScorecard struct {
	Performances ports.PerformanceRepo
}

func (uc *GetAssociateScorecard) Execute(ctx context.Context, associateId shared.AssociateId) (ports.Scorecard, error) {
	// An empty AssociateId is fulfillment-execution's "no checked-in
	// occupant" marker, never a real associate identity — a caller
	// querying it can never mean a genuine associate, so this always
	// 404s regardless of how many empty-AssociateId rows this service
	// has recorded (those rows are deliberately excluded from every
	// scorecard; see the TaskPerformance aggregate's doc comment).
	if associateId == "" {
		return ports.Scorecard{}, ErrAssociateNotFound
	}

	exists, err := uc.Performances.ExistsByAssociateID(ctx, associateId)
	if err != nil {
		return ports.Scorecard{}, err
	}
	if !exists {
		return ports.Scorecard{}, ErrAssociateNotFound
	}

	sc, err := uc.Performances.ScorecardFor(ctx, associateId)
	if err != nil {
		return ports.Scorecard{}, err
	}

	recent, err := uc.Performances.RecentByAssociateID(ctx, associateId, recentWindowSize)
	if err != nil {
		return ports.Scorecard{}, err
	}

	sc.Trend, sc.CoachingFlag = trendAndCoachingFlag(recent, sc.MeanEfficiencyPct)
	return sc, nil
}

// trendAndCoachingFlag composes the two pure domain functions
// (performance.ClassifyTrend, performance.DetectCoachingFlag) from a
// NEWEST-FIRST recent window. It reorders to oldest-first for
// DetectCoachingFlag (which reads "the last N" as chronologically most
// recent) and extracts only the scored (non-nil EfficiencyPct) values —
// unscored rows carry no signal for either function and are silently
// skipped, never treated as a zero or an error.
func trendAndCoachingFlag(recentNewestFirst []*performance.TaskPerformance, baselineMean *float64) (performance.TrendDirection, bool) {
	scoredOldestFirst := make([]float64, 0, len(recentNewestFirst))
	for i := len(recentNewestFirst) - 1; i >= 0; i-- {
		if pct := recentNewestFirst[i].EfficiencyPct(); pct != nil {
			scoredOldestFirst = append(scoredOldestFirst, *pct)
		}
	}

	var recentMean *float64
	if len(scoredOldestFirst) > 0 {
		var sum float64
		for _, v := range scoredOldestFirst {
			sum += v
		}
		mean := sum / float64(len(scoredOldestFirst))
		recentMean = &mean
	}

	trend := performance.ClassifyTrend(recentMean, baselineMean, len(scoredOldestFirst))
	flag := performance.DetectCoachingFlag(scoredOldestFirst)
	return trend, flag
}
