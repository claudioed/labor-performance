package usecases

import (
	"context"

	"github.com/claudioed/labor-performance/internal/application/ports"
	"github.com/claudioed/labor-performance/internal/domain/shared"
	"github.com/claudioed/labor-performance/internal/domain/standard"
)

// DefineStandard defines (or revises) the engineered labor standard for a
// TaskType. If a standard is already active for that TaskType, this use
// case closes it (ends its effective range at "now" — the same instant
// the new one begins) rather than overwriting it in place, so a past
// TaskPerformance's frozen StandardSecondsAtCompletion stays historically
// accurate. See ADR 0004-standard-frozen-at-completion-time-not-recomputed.md.
type DefineStandard struct {
	Standards ports.StandardRepo
	Events    ports.EventPublisher
	Clock     ports.Clock
	// UnitOfWork brackets the Save(s) + Publish atomically (ADR 0010).
	// Optional: a nil value means "no transactional backing" and the
	// calls run back to back, which is exactly the in-memory /
	// log-publisher dev configuration.
	UnitOfWork ports.UnitOfWork
	// Metrics records the labor_performance.standards.defined business
	// counter (fleet-standard-metrics ADR, Tier 2), split by outcome
	// (accepted/rejected). Nil is a valid "not instrumented" value.
	Metrics ports.StandardMetrics
}

func (uc *DefineStandard) Execute(ctx context.Context, taskType shared.TaskType, expectedSeconds int64) (*standard.LaborStandard, error) {
	now := uc.Clock.Now()

	prior, err := uc.Standards.FindCurrentlyActive(ctx, taskType)
	if err != nil {
		return nil, err
	}

	id, err := uc.Standards.NextID(ctx)
	if err != nil {
		return nil, err
	}

	next, err := standard.New(id, taskType, expectedSeconds, now)
	if err != nil {
		if uc.Metrics != nil {
			uc.Metrics.StandardDefinitionRejected(ctx)
		}
		return nil, err
	}

	// Closing the prior standard, saving the new one and publishing the
	// event are one business fact ("the standard changed") and must
	// commit together or not at all.
	err = atomically(ctx, uc.UnitOfWork, func(ctx context.Context) error {
		if prior != nil {
			prior.Close(now)
			if err := uc.Standards.Save(ctx, prior); err != nil {
				return err
			}
		}

		if err := uc.Standards.Save(ctx, next); err != nil {
			return err
		}

		if prior != nil {
			return uc.Events.Publish(ctx, shared.NewLaborStandardRevised(now, id, taskType, prior.ExpectedSeconds(), expectedSeconds, now))
		}
		return uc.Events.Publish(ctx, shared.NewLaborStandardDefined(now, id, taskType, expectedSeconds, now))
	})
	if err != nil {
		return nil, err
	}

	if uc.Metrics != nil {
		uc.Metrics.StandardDefinitionAccepted(ctx)
	}

	return next, nil
}

// atomically runs fn inside uow when one is wired, or directly otherwise.
// Keeping this in one place means every use case treats a nil UnitOfWork
// identically instead of each re-deciding the fallback.
func atomically(ctx context.Context, uow ports.UnitOfWork, fn func(ctx context.Context) error) error {
	if uow == nil {
		return fn(ctx)
	}
	return uow.Execute(ctx, fn)
}
