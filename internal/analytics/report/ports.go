package report

import (
	"context"
	"time"
)

// TaskPerformanceFact is the projection-relevant slice of a
// TaskPerformanceRecorded event, already extracted from the analytics
// envelope by the consumer. It is a plain struct declared here (rather than
// the OLTP domain event) so this port stays free of any OLTP dependency.
//
// DEVIATION FROM order-management's identically-purposed ProjectionStore
// (documented, not silently dropped): the sibling passes its derivation
// fields positionally, because every one of its Apply* methods needs
// exactly (eventId, pathId, at). This report's facts carry five and four
// fields respectively, including TWO distinct timestamps — a positional
// signature with two adjacent time.Time parameters is exactly the kind a
// caller silently transposes, so they are named here instead.
type TaskPerformanceFact struct {
	// TaskType is the dimension the row is keyed by; "" is the legitimate
	// UnclassifiedTaskType bucket.
	TaskType string
	// ActualSeconds is the measured duration; <= 0 means unmeasurable and
	// is excluded from the actual-seconds mean.
	ActualSeconds int64
	// EfficiencyPct is nil when the task could not be scored (no active
	// standard at completion time, or a non-positive duration) — such a
	// row still counts toward TaskCount, never toward the mean.
	EfficiencyPct *float64
	// CompletedAt is the BUSINESS time of the work and the row's bucket
	// dimension: a replayed event lands in the hour the task actually
	// completed, not the hour it was projected.
	CompletedAt time.Time
	// OccurredAt is the event's own emission time, used only as the
	// projection's freshness watermark.
	OccurredAt time.Time
}

// StandardFact is the projection-relevant slice of a LaborStandardDefined
// or LaborStandardRevised event.
type StandardFact struct {
	// TaskType is the dimension the row is keyed by.
	TaskType string
	// ExpectedSeconds is the engineered standard being put into effect. It
	// is carried for the analytics contract's completeness (and for future
	// rollups) though the v1 rollup only counts the definition/revision.
	ExpectedSeconds int64
	// EffectiveFrom is the standard's business time and the row's bucket
	// dimension.
	EffectiveFrom time.Time
	// OccurredAt is the event's own emission time, used only as the
	// projection's freshness watermark.
	OccurredAt time.Time
}

// ReportStore is the read side of the labor-performance data product: the
// reader process (cmd/labor-reports) queries it to serve reports. It is
// read-only by contract — the Postgres implementation runs over a pool
// pinned to a read-only role.
type ReportStore interface {
	// Query returns the report rows matching q.
	Query(ctx context.Context, q ReportQuery) (LaborPerformanceReport, error)
	// FreshnessLag reports how far the read model lags real time: the age
	// of the most recently applied event. A larger lag means the
	// projection is further behind the event stream.
	FreshnessLag(ctx context.Context) (time.Duration, error)
}

// ProjectionStore is the write side of the labor-performance data product:
// the projector process (cmd/labor-projector) applies each consumed event
// to it. Every Apply* method is idempotent on eventId — applying the same
// eventId twice records the effect once, so the at-least-once Kafka stream
// is projected exactly once.
type ProjectionStore interface {
	// ApplyTaskPerformanceRecorded folds one scored (or unscored) task into
	// the (TaskType, hour-of-CompletedAt) row. Idempotent on eventId.
	ApplyTaskPerformanceRecorded(ctx context.Context, eventId string, f TaskPerformanceFact) error
	// ApplyLaborStandardDefined counts one first-ever standard for a
	// TaskType onto the (TaskType, hour-of-EffectiveFrom) row. Idempotent
	// on eventId.
	ApplyLaborStandardDefined(ctx context.Context, eventId string, f StandardFact) error
	// ApplyLaborStandardRevised counts one standard revision, which marks
	// the bucket where efficiency numbers stop being comparable across the
	// boundary. Idempotent on eventId.
	ApplyLaborStandardRevised(ctx context.Context, eventId string, f StandardFact) error
}
