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
		return nil, err
	}

	if prior != nil {
		prior.Close(now)
		if err := uc.Standards.Save(ctx, prior); err != nil {
			return nil, err
		}
	}

	if err := uc.Standards.Save(ctx, next); err != nil {
		return nil, err
	}

	if prior != nil {
		if err := uc.Events.Publish(ctx, shared.NewLaborStandardRevised(now, id, taskType, prior.ExpectedSeconds(), expectedSeconds, now)); err != nil {
			return nil, err
		}
	} else {
		if err := uc.Events.Publish(ctx, shared.NewLaborStandardDefined(now, id, taskType, expectedSeconds, now)); err != nil {
			return nil, err
		}
	}

	return next, nil
}
