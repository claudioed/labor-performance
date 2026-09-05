package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	kafkago "github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/claudioed/labor-performance/internal/adapters/kafka/envelope"
	"github.com/claudioed/labor-performance/internal/adapters/kafka/otelkafka"
	"github.com/claudioed/labor-performance/internal/analytics/report"
)

// AnalyticsConsumerGroup is the Kafka consumer group the analytics
// projector reads under. It is distinct from the OLTP consumer group so
// the two pipelines track their offsets independently — the projector can
// be reset and replayed from the beginning without disturbing the OLTP
// consumer's position.
const AnalyticsConsumerGroup = "labor-performance-analytics"

// ProcessedEvents is the analytics consumer's idempotency gate: it
// records which event_ids have been admitted, so an at-least-once
// redelivery is a no-op.
//
// It is declared HERE — where it is consumed — rather than reused from
// internal/application/ports, so the analytics side owns its own port and
// the OLTP application layer stays entirely untouched by this data
// product (ADR-0007). The Postgres implementation lives in the
// analyticsstore outbound adapter.
type ProcessedEvents interface {
	// MarkProcessed records eventId if absent, returning true iff this
	// call newly recorded it.
	MarkProcessed(ctx context.Context, eventId string) (bool, error)
}

// analyticsData is the union of the fields the three projecting payloads
// carry. Decoding all of them into one struct keeps the decode total: a
// field absent from a given event type simply stays at its zero value,
// and only the fields that event's apply path reads are ever consulted.
type analyticsData struct {
	TaskType        string    `json:"task_type"`
	ExpectedSeconds int64     `json:"expected_seconds"`
	EffectiveFrom   time.Time `json:"effective_from"`
	TaskId          string    `json:"task_id"`
	AssociateId     string    `json:"associate_id"`
	ActualSeconds   int64     `json:"actual_seconds"`
	// EfficiencyPct is a POINTER on purpose: JSON null must decode back
	// to nil ("this task could not be scored"), never to 0.0. Decoding
	// it into a plain float64 would silently turn every unscorable task
	// into a 0% performer — exactly the fabricated number ADR-0004
	// exists to prevent.
	EfficiencyPct *float64  `json:"efficiency_pct"`
	CompletedAt   time.Time `json:"completed_at"`
}

// AnalyticsConsumer reads this service's own domain events off the
// analytics topic and applies each to the report ProjectionStore, exactly
// once per event_id.
//
// It is a SECOND, independent inbound Kafka adapter alongside Consumer:
// Consumer feeds the OLTP write path from fulfillment-execution's
// integration topic, this one feeds the analytical read model from this
// service's own analytics topic. They share no state and run in separate
// processes (cmd/labor and cmd/labor-projector).
type AnalyticsConsumer struct {
	Reader     *kafkago.Reader
	Projection report.ProjectionStore
	Processed  ProcessedEvents
	Logger     *slog.Logger
}

// NewAnalyticsConsumer constructs an AnalyticsConsumer reading the
// analytics topic from brokers under AnalyticsConsumerGroup.
func NewAnalyticsConsumer(brokers []string, projection report.ProjectionStore, processed ProcessedEvents, logger *slog.Logger) *AnalyticsConsumer {
	if logger == nil {
		logger = slog.Default()
	}
	return &AnalyticsConsumer{
		Reader: kafkago.NewReader(kafkago.ReaderConfig{
			Brokers: brokers,
			Topic:   envelope.TopicLaborPerformanceAnalytics,
			GroupID: AnalyticsConsumerGroup,
			// Start a brand-new consumer group at the EARLIEST offset.
			// The projection is a replayable read model, not a live
			// integration reaction: a fresh projector — or a backfill
			// into a new group — must see the topic's full history
			// rather than kafka-go's default of the latest offset,
			// which would silently drop every event produced before
			// the group first committed. Once the group has committed
			// offsets those take precedence, so this only affects the
			// first join.
			StartOffset: kafkago.FirstOffset,
		}),
		Projection: projection,
		Processed:  processed,
		Logger:     logger,
	}
}

// Close releases the underlying Kafka reader.
func (c *AnalyticsConsumer) Close() error {
	return c.Reader.Close()
}

// Run reads and handles messages until ctx is cancelled or the reader
// returns a fatal error. A handling error is logged and the loop
// continues, so one bad message cannot wedge the projector.
func (c *AnalyticsConsumer) Run(ctx context.Context) error {
	for {
		msg, err := c.Reader.ReadMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || ctx.Err() != nil {
				return nil
			}
			return err
		}
		if err := c.Handle(ctx, msg); err != nil {
			c.Logger.ErrorContext(ctx, "analytics message handling failed",
				"topic", msg.Topic, "offset", msg.Offset, "error", err)
		}
	}
}

// Handle processes one consumed message inside a
// "kafka.consume <topic>" span whose parent is the publisher's span, read
// from the message headers. It is exported separately from Run so trace
// propagation can be tested without a live broker.
func (c *AnalyticsConsumer) Handle(ctx context.Context, msg kafkago.Message) error {
	ctx, span := otelkafka.StartConsumeSpan(otelkafka.Extract(ctx, &msg), envelope.TopicLaborPerformanceAnalytics)
	defer span.End()

	if err := c.HandleMessage(ctx, msg.Value); err != nil {
		recordSpanError(span, err)
		return err
	}
	return nil
}

// HandleMessage decodes raw as an analytics envelope and applies the
// matching projection method for its event_type. It is exported
// separately from Run so tests can feed raw envelopes without a live
// broker.
//
// Three behaviours are load-bearing here:
//
//   - An event type outside the projection contract is ignored and NOT
//     marked processed, so widening the contract later can reprocess it
//     on a replay.
//   - A projecting event is deduped on event_id BEFORE it is applied, so
//     an at-least-once redelivery cannot double-count.
//   - A malformed payload is an error (returned, logged by Run), not a
//     silent skip — unlike an unknown event type, which is expected.
func (c *AnalyticsConsumer) HandleMessage(ctx context.Context, raw []byte) error {
	var env envelope.AnalyticsEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("analytics: decode envelope: %w", err)
	}

	// Carry the envelope's identity on whatever consume span is active,
	// so a projection failure is traceable to the exact message.
	trace.SpanFromContext(ctx).SetAttributes(
		attribute.String("messaging.message.event_id", env.EventId),
		attribute.String("messaging.message.event_type", env.EventType),
		attribute.String("messaging.message.source", env.Source),
	)

	if !isProjecting(env.EventType) {
		return nil
	}

	isNew, err := c.Processed.MarkProcessed(ctx, env.EventId)
	if err != nil {
		return fmt.Errorf("analytics: mark processed: %w", err)
	}
	if !isNew {
		return nil
	}

	var data analyticsData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return fmt.Errorf("analytics: decode data: %w", err)
	}

	return c.apply(ctx, env, data)
}

// isProjecting reports whether eventType moves a report counter.
func isProjecting(eventType string) bool {
	switch eventType {
	case envelope.EventTypeLaborStandardDefined,
		envelope.EventTypeLaborStandardRevised,
		envelope.EventTypeTaskPerformanceRecorded:
		return true
	default:
		return false
	}
}

// apply routes a decoded event to the projection method for its type,
// resolving each fact's BUSINESS time (which chooses the hour bucket)
// separately from the envelope's occurred_at (which feeds the freshness
// watermark). A zero business time falls back to occurred_at rather than
// bucketing the row at the zero instant.
func (c *AnalyticsConsumer) apply(ctx context.Context, env envelope.AnalyticsEnvelope, data analyticsData) error {
	switch env.EventType {
	case envelope.EventTypeLaborStandardDefined:
		return c.Projection.ApplyLaborStandardDefined(ctx, env.EventId, report.StandardFact{
			TaskType:        data.TaskType,
			ExpectedSeconds: data.ExpectedSeconds,
			EffectiveFrom:   orOccurredAt(data.EffectiveFrom, env.OccurredAt),
			OccurredAt:      env.OccurredAt,
		})

	case envelope.EventTypeLaborStandardRevised:
		return c.Projection.ApplyLaborStandardRevised(ctx, env.EventId, report.StandardFact{
			TaskType:        data.TaskType,
			ExpectedSeconds: data.ExpectedSeconds,
			EffectiveFrom:   orOccurredAt(data.EffectiveFrom, env.OccurredAt),
			OccurredAt:      env.OccurredAt,
		})

	case envelope.EventTypeTaskPerformanceRecorded:
		return c.Projection.ApplyTaskPerformanceRecorded(ctx, env.EventId, report.TaskPerformanceFact{
			TaskType:      data.TaskType,
			ActualSeconds: data.ActualSeconds,
			EfficiencyPct: data.EfficiencyPct,
			CompletedAt:   orOccurredAt(data.CompletedAt, env.OccurredAt),
			OccurredAt:    env.OccurredAt,
		})

	default:
		return nil
	}
}

// orOccurredAt returns businessTime, or occurredAt when businessTime is
// the zero instant.
func orOccurredAt(businessTime, occurredAt time.Time) time.Time {
	if businessTime.IsZero() {
		return occurredAt
	}
	return businessTime
}
