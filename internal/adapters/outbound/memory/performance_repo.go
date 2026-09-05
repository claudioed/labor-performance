package memory

import (
	"context"
	"sort"
	"sync"

	"github.com/claudioed/labor-performance/internal/application/ports"
	"github.com/claudioed/labor-performance/internal/domain/performance"
	"github.com/claudioed/labor-performance/internal/domain/shared"
)

// PerformanceRepo is an in-memory implementation of ports.PerformanceRepo.
type PerformanceRepo struct {
	mu    sync.RWMutex
	byTID []*performance.TaskPerformance
}

// NewPerformanceRepo constructs an empty PerformanceRepo.
func NewPerformanceRepo() *PerformanceRepo {
	return &PerformanceRepo{}
}

func (r *PerformanceRepo) Save(_ context.Context, p *performance.TaskPerformance) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byTID = append(r.byTID, p)
	return nil
}

func (r *PerformanceRepo) ExistsByAssociateID(_ context.Context, associateId shared.AssociateId) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.byTID {
		if p.AssociateId() == associateId {
			return true, nil
		}
	}
	return false, nil
}

func (r *PerformanceRepo) ScorecardFor(_ context.Context, associateId shared.AssociateId) (ports.Scorecard, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	sc := ports.Scorecard{
		AssociateId: associateId,
		ByTaskType:  make(map[shared.TaskType]ports.TaskTypeBreakdown),
	}

	var sum float64
	var scored int
	byType := make(map[shared.TaskType]struct {
		count int
		sum   float64
		n     int
	})

	for _, p := range r.byTID {
		if p.AssociateId() != associateId {
			continue
		}
		sc.TaskCount++
		agg := byType[p.TaskType()]
		agg.count++
		if p.EfficiencyPct() != nil {
			sum += *p.EfficiencyPct()
			scored++
			agg.sum += *p.EfficiencyPct()
			agg.n++
		}
		byType[p.TaskType()] = agg
	}

	if scored > 0 {
		mean := sum / float64(scored)
		sc.MeanEfficiencyPct = &mean
	}

	for tt, agg := range byType {
		b := ports.TaskTypeBreakdown{TaskCount: agg.count}
		if agg.n > 0 {
			mean := agg.sum / float64(agg.n)
			b.MeanEfficiencyPct = &mean
		}
		sc.ByTaskType[tt] = b
	}

	return sc, nil
}

func (r *PerformanceRepo) TaskTypePerformanceFor(_ context.Context, taskType shared.TaskType) (ports.TaskTypePerformance, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := ports.TaskTypePerformance{TaskType: taskType}
	var effSum float64
	var effScored int
	var actualSum float64
	var actualMeasured int

	for _, p := range r.byTID {
		if p.TaskType() != taskType {
			continue
		}
		out.TaskCount++
		if p.EfficiencyPct() != nil {
			effSum += *p.EfficiencyPct()
			effScored++
		}
		if p.ActualSeconds() > 0 {
			actualSum += float64(p.ActualSeconds())
			actualMeasured++
		}
	}

	if effScored > 0 {
		mean := effSum / float64(effScored)
		out.MeanEfficiencyPct = &mean
	}
	if actualMeasured > 0 {
		mean := actualSum / float64(actualMeasured)
		out.MeanActualSeconds = &mean
	}

	return out, nil
}

// RecentByAssociateID returns associateId's most recent rows, ordered
// newest-first (descending CompletedAt), capped at limit. byTID is
// append-ordered (insertion order), NOT CompletedAt order — a Kafka
// consumer could in principle deliver TaskCompleted events out of
// CompletedAt order (though not out of eventId/ProcessedEvents order),
// so this filters by associate then sorts by CompletedAt explicitly
// rather than assuming insertion order already reflects it.
func (r *PerformanceRepo) RecentByAssociateID(_ context.Context, associateId shared.AssociateId, limit int) ([]*performance.TaskPerformance, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	matches := make([]*performance.TaskPerformance, 0)
	for _, p := range r.byTID {
		if p.AssociateId() == associateId {
			matches = append(matches, p)
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].CompletedAt().After(matches[j].CompletedAt())
	})

	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches, nil
}
