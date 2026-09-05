// Package analyticsstore provides the outbound adapters that persist and
// serve the labor-performance "Labor Performance Report" read model: an
// in-memory implementation (MemoryStore) for tests and local runs, and
// Postgres implementations (a writer projection and a read-only reader)
// for deployment. All satisfy the report.ProjectionStore and/or
// report.ReportStore ports (ADR-0007).
package analyticsstore

import (
	"context"
	"sync"
	"time"

	"github.com/claudioed/labor-performance/internal/analytics/report"
)

// MemoryStore is an in-memory implementation of both
// report.ProjectionStore (write) and report.ReportStore (read), backed by
// maps. It is idempotent per eventId via a seen-set, so a duplicate
// delivery is a no-op, and it is safe for concurrent use.
type MemoryStore struct {
	// Now supplies the current time for FreshnessLag; defaults to
	// time.Now when nil so lag is deterministic under test.
	Now func() time.Time

	mu sync.Mutex
	// seen is the PROJECTION's idempotency set, claimed by Apply*.
	seen map[string]struct{}
	// consumed is the CONSUMER gate's idempotency set, claimed by
	// MarkProcessed. It is deliberately separate from seen — exactly as
	// analytics_consumed_events is a separate table from
	// analytics_processed_events in the Postgres schema — so the two
	// layers never claim the same key and starve each other: the gate
	// admits the event, the projection then records its effect.
	consumed map[string]struct{}
	rows     map[report.RowKey]*rowAcc
	// latest is the OccurredAt of the most recently applied event, used
	// to compute FreshnessLag.
	latest time.Time
}

// rowAcc accumulates the running counters for one report row. It mirrors
// report.Row's raw counters (not its derived means) — every mean is
// computed at read time by report.Build.
type rowAcc struct {
	tasksRecorded    int
	tasksScored      int
	efficiencyPctSum float64
	tasksMeasured    int
	actualSecondsSum int64
	standardsDefined int
	standardsRevised int
}

// NewMemoryStore constructs an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		seen:     map[string]struct{}{},
		consumed: map[string]struct{}{},
		rows:     map[report.RowKey]*rowAcc{},
	}
}

// firstApply marks eventId as seen and reports whether this is the first
// time (so the caller should apply the effect) or a duplicate (skip). It
// also advances the freshness watermark. The caller must hold s.mu.
func (s *MemoryStore) firstApply(eventId string, occurredAt time.Time) bool {
	if _, dup := s.seen[eventId]; dup {
		return false
	}
	s.seen[eventId] = struct{}{}
	if occurredAt.After(s.latest) {
		s.latest = occurredAt
	}
	return true
}

// row returns (creating if needed) the accumulator for k. The caller must
// hold s.mu.
func (s *MemoryStore) row(k report.RowKey) *rowAcc {
	r, ok := s.rows[k]
	if !ok {
		r = &rowAcc{}
		s.rows[k] = r
	}
	return r
}

// bump applies fn to the (taskType, hour-of-bucketAt) row for eventId,
// unless eventId is a duplicate. It centralises the lock, the idempotency
// gate, the task-type normalisation and the row lookup shared by every
// Apply* method.
func (s *MemoryStore) bump(eventId, taskType string, bucketAt, occurredAt time.Time, fn func(*rowAcc)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.firstApply(eventId, occurredAt) {
		return
	}
	fn(s.row(report.RowKey{
		TaskType:   report.NormalizeTaskType(taskType),
		HourBucket: report.HourBucket(bucketAt),
	}))
}

// ApplyTaskPerformanceRecorded folds one completed task into its
// (taskType, hour) row. Idempotent on eventId.
//
// A task with no EfficiencyPct still increments tasksRecorded but not
// tasksScored, and a task with a non-positive ActualSeconds still
// increments tasksRecorded but not tasksMeasured — so an unscorable or
// unmeasurable task is counted as the real business fact it is without
// ever contributing to a mean.
func (s *MemoryStore) ApplyTaskPerformanceRecorded(_ context.Context, eventId string, f report.TaskPerformanceFact) error {
	s.bump(eventId, f.TaskType, f.CompletedAt, f.OccurredAt, func(r *rowAcc) {
		r.tasksRecorded++
		if f.EfficiencyPct != nil {
			r.tasksScored++
			r.efficiencyPctSum += *f.EfficiencyPct
		}
		if f.ActualSeconds > 0 {
			r.tasksMeasured++
			r.actualSecondsSum += f.ActualSeconds
		}
	})
	return nil
}

// ApplyLaborStandardDefined counts one first-ever standard for a
// TaskType. Idempotent on eventId.
func (s *MemoryStore) ApplyLaborStandardDefined(_ context.Context, eventId string, f report.StandardFact) error {
	s.bump(eventId, f.TaskType, f.EffectiveFrom, f.OccurredAt, func(r *rowAcc) { r.standardsDefined++ })
	return nil
}

// ApplyLaborStandardRevised counts one standard revision. Idempotent on
// eventId.
func (s *MemoryStore) ApplyLaborStandardRevised(_ context.Context, eventId string, f report.StandardFact) error {
	s.bump(eventId, f.TaskType, f.EffectiveFrom, f.OccurredAt, func(r *rowAcc) { r.standardsRevised++ })
	return nil
}

// Query returns the report covering q. From is inclusive, To is
// exclusive, both compared against a row's HourBucket; an empty TaskType
// means no filter on that dimension. Aggregation is delegated to
// report.Build so this adapter and the Postgres one cannot disagree.
func (s *MemoryStore) Query(_ context.Context, q report.ReportQuery) (report.LaborPerformanceReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows := make([]report.Row, 0, len(s.rows))
	for k, r := range s.rows {
		if k.HourBucket.Before(q.From) || !k.HourBucket.Before(q.To) {
			continue
		}
		if q.TaskType != "" && k.TaskType != q.TaskType {
			continue
		}
		rows = append(rows, report.Row{
			Key:              k,
			TasksRecorded:    r.tasksRecorded,
			TasksScored:      r.tasksScored,
			EfficiencyPctSum: r.efficiencyPctSum,
			TasksMeasured:    r.tasksMeasured,
			ActualSecondsSum: r.actualSecondsSum,
			StandardsDefined: r.standardsDefined,
			StandardsRevised: r.standardsRevised,
		})
	}
	return report.Build(rows), nil
}

// FreshnessLag returns how far the read model lags real time: now minus
// the OccurredAt of the most recently applied event. Zero when nothing
// has been applied yet, and never negative (a future-dated event clamps
// to zero).
func (s *MemoryStore) FreshnessLag(_ context.Context) (time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.latest.IsZero() {
		return 0, nil
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	lag := now.Sub(s.latest)
	if lag < 0 {
		return 0, nil
	}
	return lag, nil
}

// MarkProcessed records eventId in the CONSUMER gate's set, returning
// true iff this call newly recorded it. It lets MemoryStore stand in for
// the analytics consumer's ProcessedEvents port in tests and local runs,
// mirroring the Postgres split where ConsumedEventsRepo plays that role
// over its own table.
func (s *MemoryStore) MarkProcessed(_ context.Context, eventId string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dup := s.consumed[eventId]; dup {
		return false, nil
	}
	s.consumed[eventId] = struct{}{}
	return true, nil
}

// Compile-time assertions that MemoryStore satisfies both ports.
var (
	_ report.ProjectionStore = (*MemoryStore)(nil)
	_ report.ReportStore     = (*MemoryStore)(nil)
)
