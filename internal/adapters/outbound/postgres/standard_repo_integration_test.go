//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/claudioed/labor-performance/internal/adapters/outbound/postgres"
	"github.com/claudioed/labor-performance/internal/domain/shared"
	"github.com/claudioed/labor-performance/internal/domain/standard"
)

func TestPostgres_StandardRoundTrip(t *testing.T) {
	databaseURL := requireDatabaseURL(t)
	if err := postgres.RunMigrations(databaseURL, migrationsDir(t)); err != nil {
		t.Fatalf("unexpected error running migrations: %v", err)
	}

	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, databaseURL)
	if err != nil {
		t.Fatalf("unexpected error opening pool: %v", err)
	}
	defer pool.Close()

	repo := postgres.NewStandardRepo(pool)

	id, err := repo.NextID(ctx)
	if err != nil {
		t.Fatalf("unexpected error generating ID: %v", err)
	}

	taskType := shared.Pick
	effectiveFrom := time.Now().UTC().Truncate(time.Microsecond)

	// Round-trip a freshly-active standard (EffectiveTo nil).
	fresh := mustStandard(t, id, taskType, 45, effectiveFrom)
	if err := repo.Save(ctx, fresh); err != nil {
		t.Fatalf("unexpected error saving standard: %v", err)
	}

	found, err := repo.FindCurrentlyActive(ctx, taskType)
	if err != nil {
		t.Fatalf("unexpected error finding currently active standard: %v", err)
	}
	if found == nil {
		t.Fatal("expected to find the currently active standard")
	}
	if found.ExpectedSeconds() != 45 {
		t.Fatalf("expected ExpectedSeconds=45, got %d", found.ExpectedSeconds())
	}
	if found.EffectiveTo() != nil {
		t.Fatalf("expected EffectiveTo=nil for a freshly active standard, got %v", found.EffectiveTo())
	}

	// FindActiveAsOf at effectiveFrom itself must also resolve it.
	asOf, err := repo.FindActiveAsOf(ctx, taskType, effectiveFrom)
	if err != nil {
		t.Fatalf("unexpected error finding active-as-of standard: %v", err)
	}
	if asOf == nil {
		t.Fatal("expected FindActiveAsOf to resolve the standard active at effectiveFrom")
	}

	// Close it (simulating a revision) and verify the persisted EffectiveTo
	// round-trips, and FindCurrentlyActive no longer resolves it.
	closedAt := effectiveFrom.Add(time.Hour)
	found.Close(closedAt)
	if err := repo.Save(ctx, found); err != nil {
		t.Fatalf("unexpected error saving closed standard: %v", err)
	}

	stillActive, err := repo.FindCurrentlyActive(ctx, taskType)
	if err != nil {
		t.Fatalf("unexpected error re-checking currently active standard: %v", err)
	}
	if stillActive != nil {
		t.Fatalf("expected no currently-active standard after closing the only one, got %+v", stillActive)
	}

	// A lookup "as of" a time before the close still resolves the closed
	// record — this is the frozen-history guarantee RecordTaskPerformance
	// depends on for a possibly out-of-order/replayed Kafka message.
	beforeClose, err := repo.FindActiveAsOf(ctx, taskType, effectiveFrom.Add(time.Minute))
	if err != nil {
		t.Fatalf("unexpected error finding active-as-of before close: %v", err)
	}
	if beforeClose == nil {
		t.Fatal("expected FindActiveAsOf before the close time to still resolve the closed record")
	}
	if beforeClose.ExpectedSeconds() != 45 {
		t.Fatalf("expected the frozen ExpectedSeconds=45, got %d", beforeClose.ExpectedSeconds())
	}
}

func mustStandard(t *testing.T, id shared.StandardId, taskType shared.TaskType, expectedSeconds int64, effectiveFrom time.Time) *standard.LaborStandard {
	t.Helper()
	s, err := standard.New(id, taskType, expectedSeconds, effectiveFrom)
	if err != nil {
		t.Fatalf("unexpected error building standard: %v", err)
	}
	return s
}
