package usecases

import (
	"context"
	"time"

	"github.com/claudioed/labor-performance/internal/application/ports"
	"github.com/claudioed/labor-performance/internal/domain/performance"
	"github.com/claudioed/labor-performance/internal/domain/shared"
)

// RecordTaskPerformanceRequest is one already-completed task's facts, as
// carried on a fulfillment-execution TaskCompleted Kafka event.
type RecordTaskPerformanceRequest struct {
	KafkaEventId  string
	TaskId        string
	AssociateId   shared.AssociateId
	TaskType      shared.TaskType
	ActualSeconds int64
	CompletedAt   time.Time
}

// RecordTaskPerformance is the Kafka-consumer-driven use case: it is
// called from the inbound Kafka adapter, never from HTTP. It is idempotent
// on KafkaEventId — a redelivered/duplicate message is a no-op, not a
// double-count — and resolves whichever LaborStandard was active for
// TaskType AS OF CompletedAt (not "active right now"), so a possibly
// out-of-order or replayed message is scored against the standard that
// was genuinely in force when the task actually completed.
type RecordTaskPerformance struct {
	Performances ports.PerformanceRepo
	Standards    ports.StandardRepo
	Processed    ports.ProcessedEvents
	Events       ports.EventPublisher
	Clock        ports.Clock
	// UnitOfWork brackets MarkProcessed + Save + Publish atomically (ADR
	// 0010). Optional: nil means the calls run back to back, which is
	// the in-memory / log-publisher dev configuration.
	UnitOfWork ports.UnitOfWork
}

// Execute returns (nil, nil) when req.KafkaEventId was already processed
// — a benign no-op, not an error, so a consumer redelivery never appears
// as a failure.
//
// The idempotency marker, the TaskPerformance row and the
// TaskPerformanceRecorded event are written in ONE atomic scope: if any
// of them fails, none survives, so a redelivered message after a partial
// failure is scored (the marker was rolled back too) rather than silently
// dropped as a "duplicate" of a row that never existed.
func (uc *RecordTaskPerformance) Execute(ctx context.Context, req RecordTaskPerformanceRequest) (*performance.TaskPerformance, error) {
	var p *performance.TaskPerformance
	err := atomically(ctx, uc.UnitOfWork, func(ctx context.Context) error {
		isNew, err := uc.Processed.MarkProcessed(ctx, req.KafkaEventId)
		if err != nil {
			return err
		}
		if !isNew {
			return nil
		}

		var standardSecondsAtCompletion int64
		if req.TaskType != "" {
			active, err := uc.Standards.FindActiveAsOf(ctx, req.TaskType, req.CompletedAt)
			if err != nil {
				return err
			}
			if active != nil {
				standardSecondsAtCompletion = active.ExpectedSeconds()
			}
		}

		recorded, err := performance.New(req.KafkaEventId, req.TaskId, req.AssociateId, req.TaskType, req.ActualSeconds, standardSecondsAtCompletion, req.CompletedAt)
		if err != nil {
			return err
		}

		if err := uc.Performances.Save(ctx, recorded); err != nil {
			return err
		}

		if err := uc.Events.Publish(ctx, shared.NewTaskPerformanceRecorded(uc.Clock.Now(), recorded.TaskId(), recorded.AssociateId(), recorded.TaskType(), recorded.ActualSeconds(), recorded.EfficiencyPct(), recorded.CompletedAt())); err != nil {
			return err
		}
		p = recorded
		return nil
	})
	if err != nil {
		return nil, err
	}
	return p, nil
}
