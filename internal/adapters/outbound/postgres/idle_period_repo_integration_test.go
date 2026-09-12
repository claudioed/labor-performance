//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/claudioed/labor-performance/internal/adapters/outbound/memory"
	"github.com/claudioed/labor-performance/internal/adapters/outbound/postgres"
	"github.com/claudioed/labor-performance/internal/application/usecases"
	"github.com/claudioed/labor-performance/internal/domain/idleness"
	"github.com/claudioed/labor-performance/internal/domain/shared"
)

// TestPostgres_IdlePeriodRoundTrip uses the SAME testcontainers-backed
// outboxDB helper the transactional-outbox suite already uses (never a
// DATABASE_URL skip-gate) — the test owns its own throwaway Postgres
// container end to end.
func TestPostgres_IdlePeriodRoundTrip(t *testing.T) {
	pool := outboxDB(t)
	ctx := context.Background()
	repo := postgres.NewIdlePeriodRepo(pool)

	associateId := shared.AssociateId(fmt.Sprintf("it-assoc-%d", time.Now().UnixNano()))
	startedAt := time.Now().UTC().Truncate(time.Microsecond)
	endedAt := startedAt.Add(90 * time.Second)

	gap, err := idleness.New(associateId, shared.Pick, startedAt, endedAt, 3600)
	if err != nil {
		t.Fatalf("unexpected error building idle period: %v", err)
	}

	if err := repo.Save(ctx, gap); err != nil {
		t.Fatalf("unexpected error saving idle period: %v", err)
	}

	since := startedAt.Add(-time.Hour)

	idleSeconds, count, err := repo.SumByAssociate(ctx, associateId, since)
	if err != nil {
		t.Fatalf("unexpected error summing by associate: %v", err)
	}
	if count != 1 || idleSeconds != 90 {
		t.Fatalf("SumByAssociate = (%d, %d), want (90, 1)", idleSeconds, count)
	}

	idleSecondsByType, countByType, err := repo.SumByTaskType(ctx, shared.Pick, since)
	if err != nil {
		t.Fatalf("unexpected error summing by task type: %v", err)
	}
	if countByType != 1 || idleSecondsByType != 90 {
		t.Fatalf("SumByTaskType = (%d, %d), want (90, 1)", idleSecondsByType, countByType)
	}

	lastEndedAt, err := repo.LastEndedAtByAssociate(ctx, associateId)
	if err != nil {
		t.Fatalf("unexpected error resolving last ended at: %v", err)
	}
	if !lastEndedAt.Equal(endedAt) {
		t.Fatalf("LastEndedAtByAssociate = %v, want %v", lastEndedAt, endedAt)
	}

	distinctAssociates, err := repo.DistinctAssociatesByTaskType(ctx, shared.Pick, since)
	if err != nil {
		t.Fatalf("unexpected error counting distinct associates: %v", err)
	}
	if distinctAssociates != 1 {
		t.Fatalf("DistinctAssociatesByTaskType = %d, want 1", distinctAssociates)
	}

	// An associate with no recorded idle periods must resolve the zero
	// time, not an error.
	neverSeen, err := repo.LastEndedAtByAssociate(ctx, shared.AssociateId("it-never-seen"))
	if err != nil {
		t.Fatalf("unexpected error resolving last ended at for an unseen associate: %v", err)
	}
	if !neverSeen.IsZero() {
		t.Fatalf("LastEndedAtByAssociate(never-seen) = %v, want the zero time", neverSeen)
	}
}

// TestPostgres_RecordTaskPerformance_DerivesAndPersistsIdlePeriod proves
// the idleness feature end to end against a real Postgres: two
// RecordTaskPerformance executions for one associate, through the real
// outbox-backed unit of work, produce exactly one idle_periods row with
// the expected seconds.
func TestPostgres_RecordTaskPerformance_DerivesAndPersistsIdlePeriod(t *testing.T) {
	pool := outboxDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	idlePeriods := postgres.NewIdlePeriodRepo(pool)
	uc := &usecases.RecordTaskPerformance{
		Performances: postgres.NewPerformanceRepo(pool),
		Standards:    postgres.NewStandardRepo(pool),
		Processed:    postgres.NewProcessedEventRepo(pool),
		Events:       postgres.NewOutboxPublisher(pool, analyticsEncoder(), integrationEncoder()),
		Clock:        memory.FixedClock{At: now},
		UnitOfWork:   postgres.NewUnitOfWork(pool),
		IdlePeriods:  idlePeriods,
	}

	associateId := shared.AssociateId(fmt.Sprintf("it-idle-assoc-%d", time.Now().UnixNano()))

	if _, err := uc.Execute(ctx, usecases.RecordTaskPerformanceRequest{
		KafkaEventId: "evt-idle-1", TaskId: "task-idle-1", AssociateId: associateId, TaskType: shared.Pick,
		ActualSeconds: 40, CompletedAt: now,
	}); err != nil {
		t.Fatalf("first record: %v", err)
	}
	secondCompletedAt := now.Add(190 * time.Second) // 100s idle gap + 90s task
	if _, err := uc.Execute(ctx, usecases.RecordTaskPerformanceRequest{
		KafkaEventId: "evt-idle-2", TaskId: "task-idle-2", AssociateId: associateId, TaskType: shared.Pick,
		ActualSeconds: 90, CompletedAt: secondCompletedAt,
	}); err != nil {
		t.Fatalf("second record: %v", err)
	}

	idleSeconds, count, err := idlePeriods.SumByAssociate(ctx, associateId, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("SumByAssociate: %v", err)
	}
	if count != 1 || idleSeconds != 100 {
		t.Fatalf("SumByAssociate = (%d, %d), want (100, 1)", idleSeconds, count)
	}
}
