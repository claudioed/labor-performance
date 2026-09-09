package telemetry

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/claudioed/labor-performance/internal/application/ports"
)

// meterName scopes this service's own instruments, keeping them distinct
// from the ones otelchi and the runtime collector register.
const meterName = "github.com/claudioed/labor-performance"

// standardDefinitionCounterName is the business metric: how often a
// caller's attempt to set an engineered labor standard actually takes
// effect. A rejection rate climbing means callers (or an upstream UI) are
// submitting standards that violate the one aggregate invariant
// (ExpectedSeconds must be > 0) — a proxy for a broken input form or a
// misconfigured industrial-engineering feed, not a transient failure.
const standardDefinitionCounterName = "labor_performance.standards.defined"

// outcomeKey distinguishes accepted from rejected definition attempts on
// the single counter, per the fleet-standard-metrics ADR's convention of
// one counter with an outcome attribute over two separately-named
// counters for the same event's success/failure split.
const outcomeKey = attribute.Key("outcome")

const (
	outcomeAccepted = "accepted"
	outcomeRejected = "rejected"
)

// StandardMetrics implements ports.StandardMetrics against the global
// MeterProvider. Until Setup installs a real provider, the global one is a
// no-op, so recording is cheap and safe in tests and local runs.
type StandardMetrics struct {
	counter metric.Int64Counter
}

var _ ports.StandardMetrics = (*StandardMetrics)(nil)

// NewStandardMetrics registers the standards-defined counter. It only
// fails if the instrument name is invalid, which is a programming error,
// not a runtime condition — callers that would rather run
// un-instrumented than not at all can ignore the error and pass a nil
// ports.StandardMetrics instead.
func NewStandardMetrics() (*StandardMetrics, error) {
	counter, err := otel.Meter(meterName).Int64Counter(
		standardDefinitionCounterName,
		metric.WithDescription("Attempts to define or revise an engineered labor standard, by outcome (accepted or rejected)."),
		metric.WithUnit("{standard}"),
	)
	if err != nil {
		return nil, err
	}
	return &StandardMetrics{counter: counter}, nil
}

func (m *StandardMetrics) StandardDefinitionAccepted(ctx context.Context) {
	m.record(ctx, outcomeAccepted)
}

func (m *StandardMetrics) StandardDefinitionRejected(ctx context.Context) {
	m.record(ctx, outcomeRejected)
}

func (m *StandardMetrics) record(ctx context.Context, outcome string) {
	m.counter.Add(ctx, 1, metric.WithAttributes(outcomeKey.String(outcome)))
}
