// Package otelkafka carries W3C trace context across the Kafka message
// boundary. kafka-go has no automatic instrumentation, so the inbound
// adapter extracts the active span context from a message's headers,
// making each consumer span a child of the producer span that emitted the
// message. Copied in shape from wes-work-planning's identically-purposed
// package (this service is consumer-only, so it needs Extract/
// StartConsumeSpan but not the producer-side Inject/StartPublishSpan).
package otelkafka

import (
	"context"

	kafkago "github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

// TracerName identifies the instrumentation scope of the Kafka spans.
const TracerName = "github.com/claudioed/labor-performance/internal/adapters/kafka"

// HeaderCarrier adapts a kafka-go header slice to the propagation
// TextMapCarrier interface.
type HeaderCarrier struct {
	Headers *[]kafkago.Header
}

var _ propagation.TextMapCarrier = HeaderCarrier{}

// NewHeaderCarrier returns a carrier over headers.
func NewHeaderCarrier(headers *[]kafkago.Header) HeaderCarrier {
	return HeaderCarrier{Headers: headers}
}

// Get returns the value of the first header with the given key, or "".
func (c HeaderCarrier) Get(key string) string {
	if c.Headers == nil {
		return ""
	}
	for _, h := range *c.Headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}

// Set writes key/value, replacing any existing header with that key.
func (c HeaderCarrier) Set(key, value string) {
	if c.Headers == nil {
		return
	}
	for i, h := range *c.Headers {
		if h.Key == key {
			(*c.Headers)[i].Value = []byte(value)
			return
		}
	}
	*c.Headers = append(*c.Headers, kafkago.Header{Key: key, Value: []byte(value)})
}

// Keys returns every header key, in order.
func (c HeaderCarrier) Keys() []string {
	if c.Headers == nil {
		return nil
	}
	keys := make([]string, 0, len(*c.Headers))
	for _, h := range *c.Headers {
		keys = append(keys, h.Key)
	}
	return keys
}

// Extract returns ctx enriched with the trace context found in msg's
// headers, so a span started from it becomes a child of the producer's
// span.
func Extract(ctx context.Context, msg *kafkago.Message) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, NewHeaderCarrier(&msg.Headers))
}

// Inject writes the trace context active in ctx into headers, so the
// consumer that later reads the message can make its consume span a child
// of this publish span. Added alongside the analytics publisher
// (ADR-0007): this service was consumer-only until it began publishing
// its own domain events to its analytics topic.
func Inject(ctx context.Context, headers *[]kafkago.Header) {
	otel.GetTextMapPropagator().Inject(ctx, NewHeaderCarrier(headers))
}

// StartPublishSpan starts the producer-side span for topic, named
// "kafka.publish <topic>" per the OTel messaging semantic conventions.
func StartPublishSpan(ctx context.Context, topic string, extra ...attribute.KeyValue) (context.Context, trace.Span) {
	attrs := append([]attribute.KeyValue{
		semconv.MessagingSystemKafka,
		semconv.MessagingOperationName("publish"),
		semconv.MessagingOperationTypeSend,
		semconv.MessagingDestinationName(topic),
	}, extra...)

	return otel.Tracer(TracerName).Start(ctx, "kafka.publish "+topic,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(attrs...),
	)
}

// StartConsumeSpan starts the consumer-side span for topic, named
// "kafka.consume <topic>" per the OTel messaging semantic conventions.
func StartConsumeSpan(ctx context.Context, topic string, extra ...attribute.KeyValue) (context.Context, trace.Span) {
	attrs := append([]attribute.KeyValue{
		semconv.MessagingSystemKafka,
		semconv.MessagingOperationName("consume"),
		semconv.MessagingOperationTypeProcess,
		semconv.MessagingDestinationName(topic),
	}, extra...)

	return otel.Tracer(TracerName).Start(ctx, "kafka.consume "+topic,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(attrs...),
	)
}
