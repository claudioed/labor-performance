// Package kafka holds this service's outbound Kafka adapters. Today that
// is the ANALYTICS publisher only: it fans this service's own past-tense
// domain events onto warehouse.labor-performance.analytics, feeding the
// data product's projector (ADR-0007).
//
// It is strictly ADDITIVE. The OLTP write path is untouched: the domain
// and application layers still publish through the same
// ports.EventPublisher they always have, the log publisher still exists
// and is still the default, and no existing consumer of any other topic
// is affected — because this service publishes to no other topic.
package kafka

import (
	"context"
	"encoding/json"
	"fmt"

	kafkago "github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel/codes"

	"github.com/claudioed/labor-performance/internal/adapters/kafka/envelope"
	"github.com/claudioed/labor-performance/internal/adapters/kafka/otelkafka"
	"github.com/claudioed/labor-performance/internal/application/ports"
	"github.com/claudioed/labor-performance/internal/domain/shared"
)

// Writer is the slice of kafka-go's *Writer this adapter needs, so tests
// can substitute a recording fake without a broker.
type Writer interface {
	WriteMessages(ctx context.Context, msgs ...kafkago.Message) error
}

// AnalyticsPublisher publishes each labor-performance domain event onto
// envelope.TopicLaborPerformanceAnalytics as an
// envelope.AnalyticsEnvelope. It satisfies ports.EventPublisher, so the
// composition root can hand it the same event stream the log publisher
// gets.
//
// Unlike order-management's equivalent adapter, this one performs NO
// repository enrichment: every field the report is keyed or aggregated by
// (TaskType, EfficiencyPct, ActualSeconds, CompletedAt, EffectiveFrom) is
// already carried on the domain events themselves, so the publisher stays
// a pure serializer with no read-back into the OLTP store.
type AnalyticsPublisher struct {
	Writer Writer
	// NewID mints the envelope's event_id. It is the projection's
	// idempotency key, so it must be unique per published message —
	// notably NOT the TaskId, which CLAUDE.md explicitly rules out as a
	// dedup key.
	NewID func() string
}

// NewAnalyticsPublisher constructs an AnalyticsPublisher writing to the
// analytics topic on brokers. newID mints each envelope's event_id.
func NewAnalyticsPublisher(brokers []string, newID func() string) *AnalyticsPublisher {
	return &AnalyticsPublisher{
		Writer: &kafkago.Writer{
			Addr:                   kafkago.TCP(brokers...),
			Topic:                  envelope.TopicLaborPerformanceAnalytics,
			Balancer:               &kafkago.LeastBytes{},
			AllowAutoTopicCreation: true,
		},
		NewID: newID,
	}
}

// Publish emits every event onto the analytics topic. An event type
// outside the analytics contract is skipped rather than erroring, so the
// caller can hand it the full event stream indiscriminately and a future
// domain event needs no change here to be safely ignored.
func (p *AnalyticsPublisher) Publish(ctx context.Context, events ...shared.DomainEvent) error {
	for _, event := range events {
		if err := p.publishOne(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func (p *AnalyticsPublisher) publishOne(ctx context.Context, event shared.DomainEvent) error {
	eventType, key, data, ok := marshalData(event)
	if !ok {
		return nil
	}

	payload, err := json.Marshal(envelope.AnalyticsEnvelope{
		EventId:       p.newID(),
		EventType:     eventType,
		OccurredAt:    event.OccurredAt(),
		Source:        envelope.Source,
		SchemaVersion: envelope.AnalyticsSchemaVersion,
		Data:          data,
	})
	if err != nil {
		return fmt.Errorf("kafka: marshal analytics envelope: %w", err)
	}
	return p.write(ctx, eventType, key, payload)
}

// newID mints an envelope event id, returning "" only when no generator
// was injected (which never happens in wiring, but keeps the zero value
// usable).
func (p *AnalyticsPublisher) newID() string {
	if p.NewID == nil {
		return ""
	}
	return p.NewID()
}

// marshalData maps a domain event to its analytics event_type, partition
// key, and snake_case JSON payload. The bool return is false for an event
// type outside the analytics contract, so publishOne can skip it.
//
// The partition key is the TaskType for every event: it keeps all the
// events that fold into one report dimension on a single partition, so
// the projector applies them in the order they were published.
func marshalData(e shared.DomainEvent) (eventType, key string, data json.RawMessage, ok bool) {
	switch ev := e.(type) {
	case shared.LaborStandardDefined:
		return envelope.EventTypeLaborStandardDefined, string(ev.TaskType), mustMarshal(map[string]any{
			"standard_id":      string(ev.StandardId),
			"task_type":        string(ev.TaskType),
			"expected_seconds": ev.ExpectedSeconds,
			"effective_from":   ev.EffectiveFrom,
		}), true

	case shared.LaborStandardRevised:
		return envelope.EventTypeLaborStandardRevised, string(ev.TaskType), mustMarshal(map[string]any{
			"standard_id":               string(ev.StandardId),
			"task_type":                 string(ev.TaskType),
			"previous_expected_seconds": ev.PreviousExpectedSeconds,
			"expected_seconds":          ev.NewExpectedSeconds,
			"effective_from":            ev.EffectiveFrom,
		}), true

	case shared.TaskPerformanceRecorded:
		return envelope.EventTypeTaskPerformanceRecorded, string(ev.TaskType), mustMarshal(map[string]any{
			"task_id":      ev.TaskId,
			"associate_id": string(ev.AssociateId),
			"task_type":    string(ev.TaskType),
			// A nil EfficiencyPct marshals to JSON null, which the
			// projector decodes back to nil — the "unscorable" fact
			// travels over the wire intact rather than degrading to 0.
			"efficiency_pct": ev.EfficiencyPct,
			"actual_seconds": ev.ActualSeconds,
			"completed_at":   ev.CompletedAt,
		}), true

	default:
		return "", "", nil, false
	}
}

// mustMarshal marshals a map whose shape is fully controlled by
// marshalData, so an error here would be a programming mistake rather
// than a runtime condition.
func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("kafka: marshal analytics data: %v", err))
	}
	return b
}

// write publishes one already-marshalled envelope inside a producer span,
// injecting that span's context into the message headers so the
// projector's consume span becomes its child.
func (p *AnalyticsPublisher) write(ctx context.Context, eventType, key string, payload []byte) error {
	ctx, span := otelkafka.StartPublishSpan(ctx, envelope.TopicLaborPerformanceAnalytics)
	defer span.End()

	headers := []kafkago.Header{}
	otelkafka.Inject(ctx, &headers)

	if err := p.Writer.WriteMessages(ctx, kafkago.Message{
		Key:     []byte(key),
		Value:   payload,
		Headers: headers,
	}); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("kafka: publish %s analytics event: %w", eventType, err)
	}
	return nil
}

// Close releases the underlying Kafka writer, if it is a real one.
func (p *AnalyticsPublisher) Close() error {
	if w, ok := p.Writer.(*kafkago.Writer); ok {
		return w.Close()
	}
	return nil
}

// Compile-time assertion that AnalyticsPublisher satisfies the outbound
// event-publishing port.
var _ ports.EventPublisher = (*AnalyticsPublisher)(nil)
