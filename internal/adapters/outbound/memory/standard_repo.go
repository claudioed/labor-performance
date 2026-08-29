package memory

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/claudioed/labor-performance/internal/domain/shared"
	"github.com/claudioed/labor-performance/internal/domain/standard"
)

// StandardRepo is an in-memory implementation of ports.StandardRepo.
type StandardRepo struct {
	mu        sync.RWMutex
	standards map[shared.StandardId]*standard.LaborStandard
	nextID    int
}

// NewStandardRepo constructs an empty StandardRepo.
func NewStandardRepo() *StandardRepo {
	return &StandardRepo{standards: make(map[shared.StandardId]*standard.LaborStandard)}
}

func (r *StandardRepo) Save(_ context.Context, s *standard.LaborStandard) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.standards[s.ID()] = s
	return nil
}

func (r *StandardRepo) FindActiveAsOf(_ context.Context, taskType shared.TaskType, t time.Time) (*standard.LaborStandard, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, s := range r.standards {
		if s.TaskType() == taskType && s.IsActiveAt(t) {
			return s, nil
		}
	}
	return nil, nil
}

func (r *StandardRepo) FindCurrentlyActive(ctx context.Context, taskType shared.TaskType) (*standard.LaborStandard, error) {
	return r.FindActiveAsOf(ctx, taskType, time.Now())
}

func (r *StandardRepo) NextID(_ context.Context) (shared.StandardId, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	return shared.StandardId(fmt.Sprintf("std-%d", r.nextID)), nil
}
