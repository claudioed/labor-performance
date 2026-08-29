package usecases_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/claudioed/labor-performance/internal/adapters/outbound/events"
	"github.com/claudioed/labor-performance/internal/adapters/outbound/memory"
	"github.com/claudioed/labor-performance/internal/application/ports"
	"github.com/claudioed/labor-performance/internal/application/usecases"
	"github.com/claudioed/labor-performance/internal/domain/performance"
	"github.com/claudioed/labor-performance/internal/domain/shared"
	"github.com/claudioed/labor-performance/internal/domain/standard"
)

// fixture wires the real in-memory stack for the application-layer tests.
type fixture struct {
	standards    *memory.StandardRepo
	performances *memory.PerformanceRepo
	processed    *memory.ProcessedEventRepo
	clock        memory.FixedClock

	defineStandard         *usecases.DefineStandard
	getStandard            *usecases.GetStandard
	recordTaskPerformance  *usecases.RecordTaskPerformance
	getAssociateScorecard  *usecases.GetAssociateScorecard
	getTaskTypePerformance *usecases.GetTaskTypePerformance
}

func newFixture(now time.Time) *fixture {
	standards := memory.NewStandardRepo()
	performances := memory.NewPerformanceRepo()
	processed := memory.NewProcessedEventRepo()
	publisher := events.NewLogPublisher(nil)
	clock := memory.FixedClock{At: now}

	return &fixture{
		standards:    standards,
		performances: performances,
		processed:    processed,
		clock:        clock,

		defineStandard: &usecases.DefineStandard{Standards: standards, Events: publisher, Clock: clock},
		getStandard:    &usecases.GetStandard{Standards: standards},
		recordTaskPerformance: &usecases.RecordTaskPerformance{
			Performances: performances, Standards: standards, Processed: processed, Events: publisher, Clock: clock,
		},
		getAssociateScorecard:  &usecases.GetAssociateScorecard{Performances: performances},
		getTaskTypePerformance: &usecases.GetTaskTypePerformance{Performances: performances},
	}
}

var baseTime = time.Date(2026, 8, 29, 8, 0, 0, 0, time.UTC)

func TestDefineStandard_Success_FirstDefinition(t *testing.T) {
	f := newFixture(baseTime)

	s, err := f.defineStandard.Execute(context.Background(), shared.Pick, 45)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if s.TaskType() != shared.Pick || s.ExpectedSeconds() != 45 {
		t.Fatalf("unexpected standard: %+v", s)
	}
	if s.EffectiveTo() != nil {
		t.Fatal("a freshly defined standard must be open-ended")
	}
}

func TestDefineStandard_FailingPath_NonPositiveExpectedSeconds(t *testing.T) {
	f := newFixture(baseTime)
	_, err := f.defineStandard.Execute(context.Background(), shared.Pick, 0)
	if !errors.Is(err, standard.ErrNonPositiveExpectedSeconds) {
		t.Fatalf("error = %v, want ErrNonPositiveExpectedSeconds", err)
	}
}

// TestDefineStandard_Revision covers the "DefineStandard called twice for
// the same TaskType closes the first" invariant end to end, including the
// "resolve active as of CompletedAt" resolution a subsequent
// RecordTaskPerformance depends on.
func TestDefineStandard_Revision(t *testing.T) {
	f := newFixture(baseTime)
	ctx := context.Background()

	first, err := f.defineStandard.Execute(ctx, shared.Pick, 45)
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}

	revisedAt := baseTime.Add(24 * time.Hour)
	f2 := newFixtureAt(f, revisedAt)
	second, err := f2.defineStandard.Execute(ctx, shared.Pick, 50)
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if second.ExpectedSeconds() != 50 {
		t.Fatalf("second.ExpectedSeconds = %d, want 50", second.ExpectedSeconds())
	}

	// The first standard must now be closed at revisedAt, not deleted or
	// overwritten — its ExpectedSeconds must remain 45.
	closedFirst, err := f.standards.FindActiveAsOf(ctx, shared.Pick, baseTime)
	if err != nil {
		t.Fatalf("FindActiveAsOf(baseTime): %v", err)
	}
	if closedFirst == nil || closedFirst.ExpectedSeconds() != 45 {
		t.Fatalf("standard active at baseTime = %+v, want ExpectedSeconds=45", closedFirst)
	}
	if first.ID() != closedFirst.ID() {
		t.Fatalf("expected the same standard id to still resolve for baseTime")
	}

	// A TaskPerformance completed BEFORE the revision must freeze the OLD
	// standard's value even when recorded (replayed) AFTER the revision.
	backfilled, err := f.recordTaskPerformance.Execute(ctx, usecases.RecordTaskPerformanceRequest{
		KafkaEventId: "evt-backfill", TaskId: "task-1", TaskType: shared.Pick,
		ActualSeconds: 50, CompletedAt: baseTime.Add(time.Hour), // before the revision
	})
	if err != nil {
		t.Fatalf("RecordTaskPerformance (backfill): %v", err)
	}
	if backfilled.StandardSecondsAtCompletion() != 45 {
		t.Fatalf("backfilled StandardSecondsAtCompletion = %d, want 45 (the OLD standard)", backfilled.StandardSecondsAtCompletion())
	}

	// A TaskPerformance completed AFTER the revision must freeze the NEW
	// standard's value.
	fresh, err := f.recordTaskPerformance.Execute(ctx, usecases.RecordTaskPerformanceRequest{
		KafkaEventId: "evt-fresh", TaskId: "task-2", TaskType: shared.Pick,
		ActualSeconds: 50, CompletedAt: revisedAt.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("RecordTaskPerformance (fresh): %v", err)
	}
	if fresh.StandardSecondsAtCompletion() != 50 {
		t.Fatalf("fresh StandardSecondsAtCompletion = %d, want 50 (the NEW standard)", fresh.StandardSecondsAtCompletion())
	}
}

// newFixtureAt reuses f's repos/publisher but with a clock set to at —
// simulating "time has passed" for a use case under test without
// reconstructing the whole in-memory stack.
func newFixtureAt(f *fixture, at time.Time) *fixture {
	clock := memory.FixedClock{At: at}
	return &fixture{
		standards: f.standards, performances: f.performances, processed: f.processed, clock: clock,
		defineStandard: &usecases.DefineStandard{Standards: f.standards, Events: events.NewLogPublisher(nil), Clock: clock},
		getStandard:    &usecases.GetStandard{Standards: f.standards},
		recordTaskPerformance: &usecases.RecordTaskPerformance{
			Performances: f.performances, Standards: f.standards, Processed: f.processed, Events: events.NewLogPublisher(nil), Clock: clock,
		},
		getAssociateScorecard:  &usecases.GetAssociateScorecard{Performances: f.performances},
		getTaskTypePerformance: &usecases.GetTaskTypePerformance{Performances: f.performances},
	}
}

func TestGetStandard_Success(t *testing.T) {
	f := newFixture(baseTime)
	if _, err := f.defineStandard.Execute(context.Background(), shared.Pack, 60); err != nil {
		t.Fatalf("DefineStandard: %v", err)
	}

	s, err := f.getStandard.Execute(context.Background(), shared.Pack)
	if err != nil {
		t.Fatalf("GetStandard: %v", err)
	}
	if s.ExpectedSeconds() != 60 {
		t.Fatalf("ExpectedSeconds = %d, want 60", s.ExpectedSeconds())
	}
}

func TestGetStandard_FailingPath_NotFound(t *testing.T) {
	f := newFixture(baseTime)
	_, err := f.getStandard.Execute(context.Background(), shared.Slam)
	if !errors.Is(err, usecases.ErrStandardNotFound) {
		t.Fatalf("error = %v, want ErrStandardNotFound", err)
	}
}

func TestRecordTaskPerformance_Success_WithActiveStandard(t *testing.T) {
	f := newFixture(baseTime)
	ctx := context.Background()
	if _, err := f.defineStandard.Execute(ctx, shared.Pick, 45); err != nil {
		t.Fatalf("DefineStandard: %v", err)
	}

	p, err := f.recordTaskPerformance.Execute(ctx, usecases.RecordTaskPerformanceRequest{
		KafkaEventId: "evt-1", TaskId: "task-1", AssociateId: "assoc-1", TaskType: shared.Pick,
		ActualSeconds: 52, CompletedAt: baseTime.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if p.StandardSecondsAtCompletion() != 45 {
		t.Fatalf("StandardSecondsAtCompletion = %d, want 45", p.StandardSecondsAtCompletion())
	}
	if p.EfficiencyPct() == nil {
		t.Fatal("EfficiencyPct must be set")
	}
}

// TestRecordTaskPerformance_NoActiveStandard covers the "TaskCompleted
// with no active standard for its TaskType yields EfficiencyPct=nil not
// an error" invariant.
func TestRecordTaskPerformance_NoActiveStandard(t *testing.T) {
	f := newFixture(baseTime)
	p, err := f.recordTaskPerformance.Execute(context.Background(), usecases.RecordTaskPerformanceRequest{
		KafkaEventId: "evt-1", TaskId: "task-1", TaskType: shared.Slam,
		ActualSeconds: 52, CompletedAt: baseTime,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if p.StandardSecondsAtCompletion() != 0 {
		t.Fatalf("StandardSecondsAtCompletion = %d, want 0", p.StandardSecondsAtCompletion())
	}
	if p.EfficiencyPct() != nil {
		t.Fatalf("EfficiencyPct = %v, want nil", *p.EfficiencyPct())
	}
}

// TestRecordTaskPerformance_ZeroDurationSeconds covers "TaskCompleted with
// duration_seconds=0 yields ActualSeconds=0, EfficiencyPct=nil".
func TestRecordTaskPerformance_ZeroDurationSeconds(t *testing.T) {
	f := newFixture(baseTime)
	ctx := context.Background()
	if _, err := f.defineStandard.Execute(ctx, shared.Pick, 45); err != nil {
		t.Fatalf("DefineStandard: %v", err)
	}

	p, err := f.recordTaskPerformance.Execute(ctx, usecases.RecordTaskPerformanceRequest{
		KafkaEventId: "evt-1", TaskId: "task-1", TaskType: shared.Pick,
		ActualSeconds: 0, CompletedAt: baseTime.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if p.ActualSeconds() != 0 {
		t.Fatalf("ActualSeconds = %d, want 0", p.ActualSeconds())
	}
	if p.EfficiencyPct() != nil {
		t.Fatalf("EfficiencyPct = %v, want nil", *p.EfficiencyPct())
	}
}

// TestRecordTaskPerformance_Idempotent covers "duplicate event_id
// consumed twice is idempotent (no double-count)".
func TestRecordTaskPerformance_Idempotent(t *testing.T) {
	f := newFixture(baseTime)
	ctx := context.Background()
	req := usecases.RecordTaskPerformanceRequest{
		KafkaEventId: "evt-dup", TaskId: "task-1", AssociateId: "assoc-1", TaskType: shared.Pick,
		ActualSeconds: 52, CompletedAt: baseTime,
	}

	first, err := f.recordTaskPerformance.Execute(ctx, req)
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	if first == nil {
		t.Fatal("first call must return the recorded performance")
	}

	second, err := f.recordTaskPerformance.Execute(ctx, req)
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if second != nil {
		t.Fatalf("redelivery must be a no-op (nil, nil), got %+v", second)
	}

	sc, err := f.getAssociateScorecard.Execute(ctx, "assoc-1")
	if err != nil {
		t.Fatalf("GetAssociateScorecard: %v", err)
	}
	if sc.TaskCount != 1 {
		t.Fatalf("TaskCount = %d, want 1 (no double-count)", sc.TaskCount)
	}
}

// TestRecordTaskPerformance_EmptyAssociateId covers "TaskCompleted with
// empty associate_id is recorded and counted in GetTaskTypePerformance but
// excluded from any scorecard".
func TestRecordTaskPerformance_EmptyAssociateId(t *testing.T) {
	f := newFixture(baseTime)
	ctx := context.Background()

	if _, err := f.recordTaskPerformance.Execute(ctx, usecases.RecordTaskPerformanceRequest{
		KafkaEventId: "evt-robot", TaskId: "task-1", AssociateId: "", TaskType: shared.Pick,
		ActualSeconds: 40, CompletedAt: baseTime,
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	tp, err := f.getTaskTypePerformance.Execute(ctx, shared.Pick)
	if err != nil {
		t.Fatalf("GetTaskTypePerformance: %v", err)
	}
	if tp.TaskCount != 1 {
		t.Fatalf("TaskCount = %d, want 1 — an empty-associate row must still count fleet-wide", tp.TaskCount)
	}

	if _, err := f.getAssociateScorecard.Execute(ctx, ""); !errors.Is(err, usecases.ErrAssociateNotFound) {
		t.Fatalf("GetAssociateScorecard(\"\") error = %v, want ErrAssociateNotFound", err)
	}
}

func TestGetAssociateScorecard_FailingPath_NeverSeen(t *testing.T) {
	f := newFixture(baseTime)
	_, err := f.getAssociateScorecard.Execute(context.Background(), "assoc-unknown")
	if !errors.Is(err, usecases.ErrAssociateNotFound) {
		t.Fatalf("error = %v, want ErrAssociateNotFound", err)
	}
}

// TestGetAssociateScorecard_KnownButAllNilEfficiency covers "an associate
// with 1+ rows but all-nil EfficiencyPct returns 200 with
// meanEfficiencyPct: null, not 404".
func TestGetAssociateScorecard_KnownButAllNilEfficiency(t *testing.T) {
	f := newFixture(baseTime)
	ctx := context.Background()
	// No standard defined, so EfficiencyPct will be nil for every row.
	if _, err := f.recordTaskPerformance.Execute(ctx, usecases.RecordTaskPerformanceRequest{
		KafkaEventId: "evt-1", TaskId: "task-1", AssociateId: "assoc-1", TaskType: shared.Pick,
		ActualSeconds: 52, CompletedAt: baseTime,
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	sc, err := f.getAssociateScorecard.Execute(ctx, "assoc-1")
	if err != nil {
		t.Fatalf("GetAssociateScorecard must succeed for a known associate: %v", err)
	}
	if sc.TaskCount != 1 {
		t.Fatalf("TaskCount = %d, want 1", sc.TaskCount)
	}
	if sc.MeanEfficiencyPct != nil {
		t.Fatalf("MeanEfficiencyPct = %v, want nil", *sc.MeanEfficiencyPct)
	}
}

func TestGetTaskTypePerformance_NeverSeenReturnsZeroNotError(t *testing.T) {
	f := newFixture(baseTime)
	tp, err := f.getTaskTypePerformance.Execute(context.Background(), shared.Slam)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if tp.TaskCount != 0 || tp.MeanEfficiencyPct != nil {
		t.Fatalf("unexpected non-zero result for never-seen task type: %+v", tp)
	}
}

var _ ports.Clock = memory.FixedClock{}

// The following tests exercise each use case's infrastructure-error
// propagation paths (repo/publisher failures the happy-path in-memory
// adapters never produce on their own), rounding out branch coverage
// beyond the domain-invariant-focused tests above.

func TestDefineStandard_PropagatesFindCurrentlyActiveError(t *testing.T) {
	f := newFixture(baseTime)
	wrapped := &failingStandardRepo{StandardRepo: f.standards, failFindCurrent: true}
	uc := &usecases.DefineStandard{Standards: wrapped, Events: events.NewLogPublisher(nil), Clock: f.clock}
	if _, err := uc.Execute(context.Background(), shared.Pick, 45); !errors.Is(err, errUnmapped) {
		t.Fatalf("error = %v, want errUnmapped", err)
	}
}

func TestDefineStandard_PropagatesNextIDError(t *testing.T) {
	f := newFixture(baseTime)
	wrapped := &failingStandardRepo{StandardRepo: f.standards, failNextID: true}
	uc := &usecases.DefineStandard{Standards: wrapped, Events: events.NewLogPublisher(nil), Clock: f.clock}
	if _, err := uc.Execute(context.Background(), shared.Pick, 45); !errors.Is(err, errUnmapped) {
		t.Fatalf("error = %v, want errUnmapped", err)
	}
}

func TestDefineStandard_PropagatesSaveError(t *testing.T) {
	f := newFixture(baseTime)
	wrapped := &failingStandardRepo{StandardRepo: f.standards, failSave: true}
	uc := &usecases.DefineStandard{Standards: wrapped, Events: events.NewLogPublisher(nil), Clock: f.clock}
	if _, err := uc.Execute(context.Background(), shared.Pick, 45); !errors.Is(err, errUnmapped) {
		t.Fatalf("error = %v, want errUnmapped", err)
	}
}

func TestDefineStandard_PropagatesPublishError(t *testing.T) {
	f := newFixture(baseTime)
	uc := &usecases.DefineStandard{Standards: f.standards, Events: &failingPublisher{fail: true}, Clock: f.clock}
	if _, err := uc.Execute(context.Background(), shared.Pick, 45); !errors.Is(err, errUnmapped) {
		t.Fatalf("error = %v, want errUnmapped", err)
	}
}

func TestGetStandard_PropagatesRepoError(t *testing.T) {
	f := newFixture(baseTime)
	wrapped := &failingStandardRepo{StandardRepo: f.standards, failFindCurrent: true}
	uc := &usecases.GetStandard{Standards: wrapped}
	if _, err := uc.Execute(context.Background(), shared.Pick); !errors.Is(err, errUnmapped) {
		t.Fatalf("error = %v, want errUnmapped", err)
	}
}

func TestRecordTaskPerformance_PropagatesProcessedEventsError(t *testing.T) {
	f := newFixture(baseTime)
	uc := &usecases.RecordTaskPerformance{
		Performances: f.performances, Standards: f.standards, Processed: &failingProcessedEvents{fail: true}, Events: events.NewLogPublisher(nil), Clock: f.clock,
	}
	_, err := uc.Execute(context.Background(), usecases.RecordTaskPerformanceRequest{KafkaEventId: "evt-1", TaskId: "task-1", TaskType: shared.Pick, CompletedAt: baseTime})
	if !errors.Is(err, errUnmapped) {
		t.Fatalf("error = %v, want errUnmapped", err)
	}
}

func TestRecordTaskPerformance_PropagatesStandardLookupError(t *testing.T) {
	f := newFixture(baseTime)
	wrapped := &failingStandardRepo{StandardRepo: f.standards, failFindActiveAsOf: true}
	uc := &usecases.RecordTaskPerformance{
		Performances: f.performances, Standards: wrapped, Processed: f.processed, Events: events.NewLogPublisher(nil), Clock: f.clock,
	}
	_, err := uc.Execute(context.Background(), usecases.RecordTaskPerformanceRequest{KafkaEventId: "evt-1", TaskId: "task-1", TaskType: shared.Pick, CompletedAt: baseTime})
	if !errors.Is(err, errUnmapped) {
		t.Fatalf("error = %v, want errUnmapped", err)
	}
}

func TestRecordTaskPerformance_PropagatesSaveError(t *testing.T) {
	f := newFixture(baseTime)
	wrapped := &failingPerformanceRepo{PerformanceRepo: f.performances, failSave: true}
	uc := &usecases.RecordTaskPerformance{
		Performances: wrapped, Standards: f.standards, Processed: f.processed, Events: events.NewLogPublisher(nil), Clock: f.clock,
	}
	_, err := uc.Execute(context.Background(), usecases.RecordTaskPerformanceRequest{KafkaEventId: "evt-1", TaskId: "task-1", TaskType: shared.Pick, CompletedAt: baseTime})
	if !errors.Is(err, errUnmapped) {
		t.Fatalf("error = %v, want errUnmapped", err)
	}
}

func TestRecordTaskPerformance_PropagatesPublishError(t *testing.T) {
	f := newFixture(baseTime)
	uc := &usecases.RecordTaskPerformance{
		Performances: f.performances, Standards: f.standards, Processed: f.processed, Events: &failingPublisher{fail: true}, Clock: f.clock,
	}
	_, err := uc.Execute(context.Background(), usecases.RecordTaskPerformanceRequest{KafkaEventId: "evt-1", TaskId: "task-1", TaskType: shared.Pick, CompletedAt: baseTime})
	if !errors.Is(err, errUnmapped) {
		t.Fatalf("error = %v, want errUnmapped", err)
	}
}

func TestRecordTaskPerformance_PropagatesConstructionError(t *testing.T) {
	// An empty TaskId fails performance.New's own validation; the use
	// case must surface that error rather than swallowing it.
	f := newFixture(baseTime)
	_, err := f.recordTaskPerformance.Execute(context.Background(), usecases.RecordTaskPerformanceRequest{
		KafkaEventId: "evt-1", TaskId: "", TaskType: shared.Pick, CompletedAt: baseTime,
	})
	if !errors.Is(err, performance.ErrEmptyTaskId) {
		t.Fatalf("error = %v, want ErrEmptyTaskId", err)
	}
}
