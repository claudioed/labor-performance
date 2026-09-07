package usecases_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/claudioed/labor-performance/internal/adapters/outbound/memory"
	"github.com/claudioed/labor-performance/internal/application/usecases"
	"github.com/claudioed/labor-performance/internal/domain/performance"
	"github.com/claudioed/labor-performance/internal/domain/shared"
	"github.com/claudioed/labor-performance/internal/domain/standard"
)

// scopeKey marks a context as "inside the unit of work" so the fakes can
// assert every Save/MarkProcessed/Publish happened within the scope,
// never outside it.
type scopeKey struct{}

// recordingUnitOfWork is a ports.UnitOfWork fake that (a) tags the ctx it
// hands to fn, (b) counts how many scopes were opened, and (c) reports
// whether each scope committed (fn returned nil) or rolled back.
type recordingUnitOfWork struct {
	opened     int
	committed  int
	rolledBack int
	beginErr   error
}

func (u *recordingUnitOfWork) Execute(ctx context.Context, fn func(ctx context.Context) error) error {
	if u.beginErr != nil {
		return u.beginErr
	}
	u.opened++
	err := fn(context.WithValue(ctx, scopeKey{}, true))
	if err != nil {
		u.rolledBack++
		return err
	}
	u.committed++
	return nil
}

func inScope(ctx context.Context) bool {
	v, _ := ctx.Value(scopeKey{}).(bool)
	return v
}

// scopedPublisher records, per Publish, whether it happened inside a
// scope, and can be told to fail.
type scopedPublisher struct {
	inScope []bool
	events  []shared.DomainEvent
	err     error
}

func (p *scopedPublisher) Publish(ctx context.Context, events ...shared.DomainEvent) error {
	p.inScope = append(p.inScope, inScope(ctx))
	if p.err != nil {
		return p.err
	}
	p.events = append(p.events, events...)
	return nil
}

// scopedStandardRepo records whether each Save happened inside a scope.
type scopedStandardRepo struct {
	*memory.StandardRepo
	saveInScope []bool
}

func (r *scopedStandardRepo) Save(ctx context.Context, s *standard.LaborStandard) error {
	r.saveInScope = append(r.saveInScope, inScope(ctx))
	return r.StandardRepo.Save(ctx, s)
}

// scopedPerformanceRepo records whether each Save happened inside a scope.
type scopedPerformanceRepo struct {
	*memory.PerformanceRepo
	saveInScope []bool
}

func (r *scopedPerformanceRepo) Save(ctx context.Context, p *performance.TaskPerformance) error {
	r.saveInScope = append(r.saveInScope, inScope(ctx))
	return r.PerformanceRepo.Save(ctx, p)
}

// scopedProcessedEvents records whether each MarkProcessed happened
// inside a scope.
type scopedProcessedEvents struct {
	*memory.ProcessedEventRepo
	markInScope []bool
}

func (r *scopedProcessedEvents) MarkProcessed(ctx context.Context, eventId string) (bool, error) {
	r.markInScope = append(r.markInScope, inScope(ctx))
	return r.ProcessedEventRepo.MarkProcessed(ctx, eventId)
}

func allTrue(bs []bool) bool {
	for _, b := range bs {
		if !b {
			return false
		}
	}
	return true
}

// --- DefineStandard ---------------------------------------------------

func TestDefineStandard_FirstDefinition_SaveAndPublishRunInsideOneUnitOfWork(t *testing.T) {
	standards := &scopedStandardRepo{StandardRepo: memory.NewStandardRepo()}
	pub := &scopedPublisher{}
	uow := &recordingUnitOfWork{}
	uc := &usecases.DefineStandard{Standards: standards, Events: pub, Clock: memory.FixedClock{At: baseTime}, UnitOfWork: uow}

	if _, err := uc.Execute(context.Background(), shared.Pick, 45); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if uow.opened != 1 || uow.committed != 1 || uow.rolledBack != 0 {
		t.Fatalf("expected exactly one committed scope, got opened=%d committed=%d rolledBack=%d", uow.opened, uow.committed, uow.rolledBack)
	}
	if len(standards.saveInScope) != 1 || !allTrue(standards.saveInScope) {
		t.Fatalf("expected the single Save inside the unit of work, got %v", standards.saveInScope)
	}
	if len(pub.inScope) != 1 || !pub.inScope[0] {
		t.Fatalf("expected Publish to run inside the unit of work, got %v", pub.inScope)
	}
	if _, ok := pub.events[0].(shared.LaborStandardDefined); !ok {
		t.Fatalf("expected LaborStandardDefined, got %T", pub.events[0])
	}
}

func TestDefineStandard_Revision_BothSavesAndPublishRunInsideOneUnitOfWork(t *testing.T) {
	standards := &scopedStandardRepo{StandardRepo: memory.NewStandardRepo()}
	setup := &usecases.DefineStandard{Standards: standards, Events: &scopedPublisher{}, Clock: memory.FixedClock{At: baseTime}}
	if _, err := setup.Execute(context.Background(), shared.Pick, 45); err != nil {
		t.Fatalf("setup: %v", err)
	}
	standards.saveInScope = nil

	pub := &scopedPublisher{}
	uow := &recordingUnitOfWork{}
	uc := &usecases.DefineStandard{Standards: standards, Events: pub, Clock: memory.FixedClock{At: baseTime.Add(time.Hour)}, UnitOfWork: uow}
	if _, err := uc.Execute(context.Background(), shared.Pick, 40); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if uow.opened != 1 || uow.committed != 1 {
		t.Fatalf("expected exactly one committed scope, got opened=%d committed=%d", uow.opened, uow.committed)
	}
	// Closing the prior AND inserting the new one are both inside.
	if len(standards.saveInScope) != 2 || !allTrue(standards.saveInScope) {
		t.Fatalf("expected both Saves (close prior, insert next) inside the unit of work, got %v", standards.saveInScope)
	}
	if len(pub.inScope) != 1 || !pub.inScope[0] {
		t.Fatalf("expected Publish inside the unit of work, got %v", pub.inScope)
	}
	if _, ok := pub.events[0].(shared.LaborStandardRevised); !ok {
		t.Fatalf("expected LaborStandardRevised, got %T", pub.events[0])
	}
}

func TestDefineStandard_PublishFailure_RollsBackTheUnitOfWork(t *testing.T) {
	pub := &scopedPublisher{err: errors.New("outbox insert failed")}
	uow := &recordingUnitOfWork{}
	metrics := &fakeStandardMetrics{}
	uc := &usecases.DefineStandard{Standards: memory.NewStandardRepo(), Events: pub, Clock: memory.FixedClock{At: baseTime}, UnitOfWork: uow, Metrics: metrics}

	_, err := uc.Execute(context.Background(), shared.Pick, 45)
	if err == nil || err.Error() != "outbox insert failed" {
		t.Fatalf("expected the publish error to propagate, got %v", err)
	}
	if uow.rolledBack != 1 || uow.committed != 0 {
		t.Fatalf("expected the scope to roll back, got committed=%d rolledBack=%d", uow.committed, uow.rolledBack)
	}
	if metrics.accepted != 0 {
		t.Fatalf("a rolled-back definition must not be counted as accepted, got accepted=%d", metrics.accepted)
	}
}

func TestDefineStandard_UnitOfWorkBeginFailure_Propagates(t *testing.T) {
	pub := &scopedPublisher{}
	uow := &recordingUnitOfWork{beginErr: errors.New("begin failed")}
	uc := &usecases.DefineStandard{Standards: memory.NewStandardRepo(), Events: pub, Clock: memory.FixedClock{At: baseTime}, UnitOfWork: uow}

	if _, err := uc.Execute(context.Background(), shared.Pick, 45); err == nil || err.Error() != "begin failed" {
		t.Fatalf("expected begin error, got %v", err)
	}
	if len(pub.events) != 0 {
		t.Fatal("expected nothing published when the unit of work cannot begin")
	}
}

func TestDefineStandard_RejectedInput_OpensNoUnitOfWork(t *testing.T) {
	uow := &recordingUnitOfWork{}
	uc := &usecases.DefineStandard{Standards: memory.NewStandardRepo(), Events: &scopedPublisher{}, Clock: memory.FixedClock{At: baseTime}, UnitOfWork: uow}

	if _, err := uc.Execute(context.Background(), shared.Pick, 0); err == nil {
		t.Fatal("expected a non-positive ExpectedSeconds to be rejected")
	}
	if uow.opened != 0 {
		t.Fatalf("a rejected definition must not open a unit of work, got opened=%d", uow.opened)
	}
}

func TestDefineStandard_NilUnitOfWork_StillSavesAndPublishes(t *testing.T) {
	pub := &scopedPublisher{}
	uc := &usecases.DefineStandard{Standards: memory.NewStandardRepo(), Events: pub, Clock: memory.FixedClock{At: baseTime}}

	if _, err := uc.Execute(context.Background(), shared.Pick, 45); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pub.events) != 1 || pub.inScope[0] {
		t.Fatalf("expected one publish outside any scope, got events=%d inScope=%v", len(pub.events), pub.inScope)
	}
}

// --- RecordTaskPerformance ---------------------------------------------

func recordRequest(eventId string) usecases.RecordTaskPerformanceRequest {
	return usecases.RecordTaskPerformanceRequest{
		KafkaEventId:  eventId,
		TaskId:        "task-1",
		AssociateId:   "assoc-1",
		TaskType:      shared.Pick,
		ActualSeconds: 52,
		CompletedAt:   baseTime,
	}
}

func TestRecordTaskPerformance_MarkSaveAndPublishRunInsideOneUnitOfWork(t *testing.T) {
	performances := &scopedPerformanceRepo{PerformanceRepo: memory.NewPerformanceRepo()}
	processed := &scopedProcessedEvents{ProcessedEventRepo: memory.NewProcessedEventRepo()}
	pub := &scopedPublisher{}
	uow := &recordingUnitOfWork{}
	uc := &usecases.RecordTaskPerformance{
		Performances: performances, Standards: memory.NewStandardRepo(), Processed: processed,
		Events: pub, Clock: memory.FixedClock{At: baseTime}, UnitOfWork: uow,
	}

	p, err := uc.Execute(context.Background(), recordRequest("evt-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("expected the recorded TaskPerformance to be returned")
	}
	if uow.opened != 1 || uow.committed != 1 || uow.rolledBack != 0 {
		t.Fatalf("expected exactly one committed scope, got opened=%d committed=%d rolledBack=%d", uow.opened, uow.committed, uow.rolledBack)
	}
	// The idempotency marker is part of the atomic scope: it must never
	// commit without the row it guards.
	if len(processed.markInScope) != 1 || !processed.markInScope[0] {
		t.Fatalf("expected MarkProcessed inside the unit of work, got %v", processed.markInScope)
	}
	if len(performances.saveInScope) != 1 || !performances.saveInScope[0] {
		t.Fatalf("expected Save inside the unit of work, got %v", performances.saveInScope)
	}
	if len(pub.inScope) != 1 || !pub.inScope[0] {
		t.Fatalf("expected Publish inside the unit of work, got %v", pub.inScope)
	}
}

func TestRecordTaskPerformance_PublishFailure_RollsBackTheUnitOfWork(t *testing.T) {
	pub := &scopedPublisher{err: errors.New("outbox insert failed")}
	uow := &recordingUnitOfWork{}
	uc := &usecases.RecordTaskPerformance{
		Performances: memory.NewPerformanceRepo(), Standards: memory.NewStandardRepo(), Processed: memory.NewProcessedEventRepo(),
		Events: pub, Clock: memory.FixedClock{At: baseTime}, UnitOfWork: uow,
	}

	p, err := uc.Execute(context.Background(), recordRequest("evt-1"))
	if err == nil || err.Error() != "outbox insert failed" {
		t.Fatalf("expected the publish error to propagate, got %v", err)
	}
	if p != nil {
		t.Fatal("a rolled-back recording must not return a TaskPerformance")
	}
	if uow.rolledBack != 1 || uow.committed != 0 {
		t.Fatalf("expected the scope to roll back, got committed=%d rolledBack=%d", uow.committed, uow.rolledBack)
	}
}

func TestRecordTaskPerformance_Duplicate_CommitsAnEmptyScopeAndPublishesNothing(t *testing.T) {
	pub := &scopedPublisher{}
	uow := &recordingUnitOfWork{}
	uc := &usecases.RecordTaskPerformance{
		Performances: memory.NewPerformanceRepo(), Standards: memory.NewStandardRepo(), Processed: memory.NewProcessedEventRepo(),
		Events: pub, Clock: memory.FixedClock{At: baseTime}, UnitOfWork: uow,
	}

	if _, err := uc.Execute(context.Background(), recordRequest("evt-1")); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	p, err := uc.Execute(context.Background(), recordRequest("evt-1"))
	if err != nil || p != nil {
		t.Fatalf("redelivery must be a benign no-op, got p=%v err=%v", p, err)
	}
	if uow.opened != 2 || uow.committed != 2 {
		t.Fatalf("expected both deliveries to open+commit a scope, got opened=%d committed=%d", uow.opened, uow.committed)
	}
	if len(pub.events) != 1 {
		t.Fatalf("expected exactly one publish across both deliveries, got %d", len(pub.events))
	}
}

func TestRecordTaskPerformance_UnitOfWorkBeginFailure_Propagates(t *testing.T) {
	pub := &scopedPublisher{}
	processed := memory.NewProcessedEventRepo()
	uow := &recordingUnitOfWork{beginErr: errors.New("begin failed")}
	uc := &usecases.RecordTaskPerformance{
		Performances: memory.NewPerformanceRepo(), Standards: memory.NewStandardRepo(), Processed: processed,
		Events: pub, Clock: memory.FixedClock{At: baseTime}, UnitOfWork: uow,
	}

	if _, err := uc.Execute(context.Background(), recordRequest("evt-1")); err == nil || err.Error() != "begin failed" {
		t.Fatalf("expected begin error, got %v", err)
	}
	if len(pub.events) != 0 {
		t.Fatal("expected nothing published when the unit of work cannot begin")
	}
	// The marker must not have been written either — a redelivery must
	// still be processed.
	isNew, err := processed.MarkProcessed(context.Background(), "evt-1")
	if err != nil || !isNew {
		t.Fatalf("expected evt-1 to be unmarked after a begin failure, got isNew=%v err=%v", isNew, err)
	}
}

func TestRecordTaskPerformance_NilUnitOfWork_StillRecordsAndPublishes(t *testing.T) {
	pub := &scopedPublisher{}
	uc := &usecases.RecordTaskPerformance{
		Performances: memory.NewPerformanceRepo(), Standards: memory.NewStandardRepo(), Processed: memory.NewProcessedEventRepo(),
		Events: pub, Clock: memory.FixedClock{At: baseTime},
	}

	p, err := uc.Execute(context.Background(), recordRequest("evt-1"))
	if err != nil || p == nil {
		t.Fatalf("unexpected result: p=%v err=%v", p, err)
	}
	if len(pub.events) != 1 || pub.inScope[0] {
		t.Fatalf("expected one publish outside any scope, got events=%d inScope=%v", len(pub.events), pub.inScope)
	}
}
