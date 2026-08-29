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
}

// Execute returns (nil, nil) when req.KafkaEventId was already processed
// — a benign no-op, not an error, so a consumer redelivery never appears
// as a failure.
func (uc *RecordTaskPerformance) Execute(ctx context.Context, req RecordTaskPerformanceRequest) (*performance.TaskPerformance, error) {
	isNew, err := uc.Processed.MarkProcessed(ctx, req.KafkaEventId)
	if err != nil {
		return nil, err
	}
	if !isNew {
		return nil, nil
	}

	var standardSecondsAtCompletion int64
	if req.TaskType != "" {
		active, err := uc.Standards.FindActiveAsOf(ctx, req.TaskType, req.CompletedAt)
		if err != nil {
			return nil, err
		}
		if active != nil {
			standardSecondsAtCompletion = active.ExpectedSeconds()
		}
	}

	p, err := performance.New(req.KafkaEventId, req.TaskId, req.AssociateId, req.TaskType, req.ActualSeconds, standardSecondsAtCompletion, req.CompletedAt)
	if err != nil {
		return nil, err
	}

	if err := uc.Performances.Save(ctx, p); err != nil {
		return nil, err
	}

	if err := uc.Events.Publish(ctx, shared.NewTaskPerformanceRecorded(uc.Clock.Now(), p.TaskId(), p.AssociateId(), p.TaskType(), p.ActualSeconds(), p.EfficiencyPct(), p.CompletedAt())); err != nil {
		return nil, err
	}

	return p, nil
}
