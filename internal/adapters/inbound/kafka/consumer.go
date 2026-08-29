// Package kafka is the inbound Kafka adapter: it consumes
// warehouse.fulfillment.events (the shared, fan-out topic
// wes-work-planning already consumes) and feeds TaskCompleted into the
// existing RecordTaskPerformance use case. Every other event type on that
// topic is silently skipped — mirroring wes-work-planning's own consumer's
// skip-unrecognized-event-type behavior — since this is a shared topic by
// convention even though fulfillment-execution publishes no other event
// type to it today.
package kafka

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"

	kafkago "github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/claudioed/labor-performance/internal/adapters/kafka/envelope"
	"github.com/claudioed/labor-performance/internal/adapters/kafka/otelkafka"
	"github.com/claudioed/labor-performance/internal/application/usecases"
	"github.com/claudioed/labor-performance/internal/domain/shared"
)

// taskCompletedData is fulfillment-execution's TaskCompleted payload, as
// verified against feature/labor-performance-hooks's actual publisher
// (internal/adapters/outbound/kafka/publisher.go's TaskCompletedData
// struct) this session. AssociateId and DurationSeconds are marked
// `omitempty` on that struct's OWN JSON tags, so an older
// fulfillment-execution payload that predates that enrichment simply
// omits them from the wire — this struct's zero values ("" / 0) already
// degrade gracefully to exactly the "unmeasurable"/"no occupant" cases
// SPEC.md's aggregate invariants require, so no special-casing is needed
// here beyond ordinary Go zero-value JSON unmarshaling. task_type is
// deliberately NOT part of that struct today (a real, documented wire gap
// — see shared.ParseTaskTypeLenient's doc comment), so it is likewise
// absent here and always resolves to "" (unclassified).
type taskCompletedData struct {
	TaskId          string `json:"task_id"`
	StationId       string `json:"station_id"`
	WorkUnitId      string `json:"work_unit_id"`
	AssociateId     string `json:"associate_id"`
	DurationSeconds int64  `json:"duration_seconds"`
}

// Consumer consumes warehouse.fulfillment.events, feeding TaskCompleted
// into the existing RecordTaskPerformance use case.
type Consumer struct {
	reader                *kafkago.Reader
	recordTaskPerformance *usecases.RecordTaskPerformance
	logger                *slog.Logger
}

// NewConsumer constructs a Consumer reading TopicFulfillmentEvents on
// brokers under groupID.
func NewConsumer(brokers []string, groupID string, recordTaskPerformance *usecases.RecordTaskPerformance, logger *slog.Logger) *Consumer {
	return &Consumer{
		reader: kafkago.NewReader(kafkago.ReaderConfig{
			Brokers: brokers,
			GroupID: groupID,
			Topic:   envelope.TopicFulfillmentEvents,
		}),
		recordTaskPerformance: recordTaskPerformance,
		logger:                logger,
	}
}

// Close releases the underlying Kafka reader.
func (c *Consumer) Close() error {
	return c.reader.Close()
}

// Run consumes the topic until ctx is cancelled.
func (c *Consumer) Run(ctx context.Context) error {
	for {
		msg, err := c.reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}

		if err := c.handleMessage(ctx, msg); err != nil {
			return err
		}
	}
}

// handleMessage processes one fetched message inside a
// "kafka.consume <topic>" span whose parent is the producing service's
// publish span, recovered from the message's W3C trace-context headers.
// Unparseable or unhandleable messages are logged and committed rather
// than redelivered forever; only a commit failure aborts the consume loop,
// which is the error this returns.
func (c *Consumer) handleMessage(ctx context.Context, msg kafkago.Message) error {
	topic := c.reader.Config().Topic

	msgCtx, span := otelkafka.StartConsumeSpan(otelkafka.Extract(ctx, &msg), topic,
		semconv.MessagingKafkaOffset(int(msg.Offset)),
		semconv.MessagingDestinationPartitionID(strconv.Itoa(msg.Partition)),
	)
	defer span.End()

	var env envelope.Envelope
	if err := json.Unmarshal(msg.Value, &env); err != nil {
		recordSpanError(span, err)
		c.log(msgCtx, "skipping unparseable kafka message", "topic", topic, "error", err)
		_ = c.reader.CommitMessages(ctx, msg)
		return nil
	}

	span.SetAttributes(
		attribute.String("messaging.message.event_id", env.EventId),
		attribute.String("messaging.message.event_type", env.EventType),
		attribute.String("messaging.message.source", env.Source),
	)

	if err := c.handleFulfillmentEvent(msgCtx, env); err != nil {
		recordSpanError(span, err)
		c.log(msgCtx, "skipping kafka event",
			"topic", topic, "event_id", env.EventId, "event_type", env.EventType, "error", err)
		_ = c.reader.CommitMessages(ctx, msg)
		return nil
	}

	if err := c.reader.CommitMessages(ctx, msg); err != nil {
		recordSpanError(span, err)
		return err
	}
	return nil
}

// handleFulfillmentEvent filters for TaskCompleted and feeds it into the
// existing RecordTaskPerformance use case. Every other event type on this
// shared topic is silently skipped, not an error.
func (c *Consumer) handleFulfillmentEvent(ctx context.Context, env envelope.Envelope) error {
	if env.EventType != envelope.EventTypeTaskCompleted {
		return nil
	}

	var data taskCompletedData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return err
	}

	// task_type is not yet on the wire (see taskCompletedData's doc
	// comment) — ParseTaskTypeLenient("") always resolves to ""
	// (unclassified), the same degrade-gracefully path an unrecognized
	// value would take once fulfillment-execution does add the field.
	_, err := c.recordTaskPerformance.Execute(ctx, usecases.RecordTaskPerformanceRequest{
		KafkaEventId:  env.EventId,
		TaskId:        data.TaskId,
		AssociateId:   shared.AssociateId(data.AssociateId),
		TaskType:      shared.ParseTaskTypeLenient(""),
		ActualSeconds: data.DurationSeconds,
		CompletedAt:   env.OccurredAt,
	})
	return err
}

// recordSpanError marks span as failed without changing any control flow.
func recordSpanError(span trace.Span, err error) {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// log emits a structured record through the configured logger, carrying
// the consume span's trace_id/span_id via ctx. A nil logger silences
// output, as the tests rely on.
func (c *Consumer) log(ctx context.Context, msg string, args ...any) {
	if c.logger != nil {
		c.logger.WarnContext(ctx, msg, args...)
	}
}
