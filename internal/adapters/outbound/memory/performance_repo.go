package memory

import (
	"context"
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
	var sum float64
	var scored int

	for _, p := range r.byTID {
		if p.TaskType() != taskType {
			continue
		}
		out.TaskCount++
		if p.EfficiencyPct() != nil {
			sum += *p.EfficiencyPct()
			scored++
		}
	}

	if scored > 0 {
		mean := sum / float64(scored)
		out.MeanEfficiencyPct = &mean
	}

	return out, nil
}
