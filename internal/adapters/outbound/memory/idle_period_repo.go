package memory

import (
	"context"
	"sync"
	"time"

	"github.com/claudioed/labor-performance/internal/domain/idleness"
	"github.com/claudioed/labor-performance/internal/domain/shared"
)

// IdlePeriodRepo is an in-memory implementation of ports.IdlePeriodRepo.
type IdlePeriodRepo struct {
	mu    sync.RWMutex
	items []*idleness.IdlePeriod
}

// NewIdlePeriodRepo constructs an empty IdlePeriodRepo.
func NewIdlePeriodRepo() *IdlePeriodRepo {
	return &IdlePeriodRepo{}
}

func (r *IdlePeriodRepo) Save(_ context.Context, p *idleness.IdlePeriod) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = append(r.items, p)
	return nil
}

func (r *IdlePeriodRepo) SumByTaskType(_ context.Context, taskType shared.TaskType, since time.Time) (int64, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var sum int64
	var count int
	for _, p := range r.items {
		if p.TaskType() != taskType {
			continue
		}
		if p.EndedAt().Before(since) {
			continue
		}
		sum += p.Seconds()
		count++
	}
	return sum, count, nil
}

func (r *IdlePeriodRepo) SumByAssociate(_ context.Context, associateId shared.AssociateId, since time.Time) (int64, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var sum int64
	var count int
	for _, p := range r.items {
		if p.AssociateId() != associateId {
			continue
		}
		if p.EndedAt().Before(since) {
			continue
		}
		sum += p.Seconds()
		count++
	}
	return sum, count, nil
}

func (r *IdlePeriodRepo) LastEndedAtByAssociate(_ context.Context, associateId shared.AssociateId) (time.Time, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var last time.Time
	for _, p := range r.items {
		if p.AssociateId() != associateId {
			continue
		}
		if p.EndedAt().After(last) {
			last = p.EndedAt()
		}
	}
	return last, nil
}

func (r *IdlePeriodRepo) DistinctAssociatesByTaskType(_ context.Context, taskType shared.TaskType, since time.Time) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[shared.AssociateId]struct{})
	for _, p := range r.items {
		if p.TaskType() != taskType {
			continue
		}
		if p.EndedAt().Before(since) {
			continue
		}
		seen[p.AssociateId()] = struct{}{}
	}
	return len(seen), nil
}
