package usecases_test

import (
	"context"
	"errors"
	"time"

	"github.com/claudioed/labor-performance/internal/application/ports"
	"github.com/claudioed/labor-performance/internal/domain/performance"
	"github.com/claudioed/labor-performance/internal/domain/shared"
	"github.com/claudioed/labor-performance/internal/domain/standard"
)

// errUnmapped is a generic infrastructure failure used to exercise a use
// case's error-propagation paths (repo/publisher failures), which the
// happy-path in-memory adapters never produce on their own.
var errUnmapped = errors.New("unmapped: simulated infrastructure failure")

// failingStandardRepo wraps a real ports.StandardRepo and can be told to
// fail on specific calls, to exercise DefineStandard/GetStandard/
// RecordTaskPerformance error-propagation paths.
type failingStandardRepo struct {
	ports.StandardRepo
	failSave           bool
	failFindActiveAsOf bool
	failFindCurrent    bool
	failNextID         bool
}

func (f *failingStandardRepo) Save(ctx context.Context, s *standard.LaborStandard) error {
	if f.failSave {
		return errUnmapped
	}
	return f.StandardRepo.Save(ctx, s)
}

func (f *failingStandardRepo) FindActiveAsOf(ctx context.Context, taskType shared.TaskType, t time.Time) (*standard.LaborStandard, error) {
	if f.failFindActiveAsOf {
		return nil, errUnmapped
	}
	return f.StandardRepo.FindActiveAsOf(ctx, taskType, t)
}

func (f *failingStandardRepo) FindCurrentlyActive(ctx context.Context, taskType shared.TaskType) (*standard.LaborStandard, error) {
	if f.failFindCurrent {
		return nil, errUnmapped
	}
	return f.StandardRepo.FindCurrentlyActive(ctx, taskType)
}

func (f *failingStandardRepo) NextID(ctx context.Context) (shared.StandardId, error) {
	if f.failNextID {
		return "", errUnmapped
	}
	return f.StandardRepo.NextID(ctx)
}

// failingPerformanceRepo wraps a real ports.PerformanceRepo and can be
// told to fail Save or RecentByAssociateID, to exercise
// RecordTaskPerformance's and GetAssociateScorecard's error paths.
type failingPerformanceRepo struct {
	ports.PerformanceRepo
	failSave   bool
	failRecent bool
}

func (f *failingPerformanceRepo) Save(ctx context.Context, p *performance.TaskPerformance) error {
	if f.failSave {
		return errUnmapped
	}
	return f.PerformanceRepo.Save(ctx, p)
}

func (f *failingPerformanceRepo) RecentByAssociateID(ctx context.Context, associateId shared.AssociateId, limit int) ([]*performance.TaskPerformance, error) {
	if f.failRecent {
		return nil, errUnmapped
	}
	return f.PerformanceRepo.RecentByAssociateID(ctx, associateId, limit)
}

// failingPublisher can be told to fail Publish, to exercise a use case's
// event-publish error path.
type failingPublisher struct {
	fail bool
}

func (p *failingPublisher) Publish(ctx context.Context, events ...shared.DomainEvent) error {
	if p.fail {
		return errUnmapped
	}
	return nil
}

// failingProcessedEvents can be told to fail MarkProcessed, to exercise
// RecordTaskPerformance's idempotency-gate error path.
type failingProcessedEvents struct {
	fail bool
}

func (p *failingProcessedEvents) MarkProcessed(ctx context.Context, eventId string) (bool, error) {
	if p.fail {
		return false, errUnmapped
	}
	return true, nil
}
