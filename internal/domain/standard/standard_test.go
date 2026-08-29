package standard_test

import (
	"errors"
	"testing"
	"time"

	"github.com/claudioed/labor-performance/internal/domain/shared"
	"github.com/claudioed/labor-performance/internal/domain/standard"
)

func mustNew(t *testing.T, taskType shared.TaskType, expectedSeconds int64, effectiveFrom time.Time) *standard.LaborStandard {
	t.Helper()
	s, err := standard.New("std-1", taskType, expectedSeconds, effectiveFrom)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func TestNew_Success(t *testing.T) {
	from := time.Date(2026, 8, 29, 8, 0, 0, 0, time.UTC)
	s := mustNew(t, shared.Pick, 45, from)

	if s.TaskType() != shared.Pick {
		t.Fatalf("TaskType = %v, want PICK", s.TaskType())
	}
	if s.ExpectedSeconds() != 45 {
		t.Fatalf("ExpectedSeconds = %d, want 45", s.ExpectedSeconds())
	}
	if !s.EffectiveFrom().Equal(from) {
		t.Fatalf("EffectiveFrom = %v, want %v", s.EffectiveFrom(), from)
	}
	if s.EffectiveTo() != nil {
		t.Fatalf("EffectiveTo = %v, want nil (freshly active)", s.EffectiveTo())
	}
}

func TestNew_FailingPath_NonPositiveExpectedSeconds(t *testing.T) {
	from := time.Now()
	for _, seconds := range []int64{0, -1, -100} {
		if _, err := standard.New("std-1", shared.Pick, seconds, from); !errors.Is(err, standard.ErrNonPositiveExpectedSeconds) {
			t.Fatalf("New(%d) error = %v, want ErrNonPositiveExpectedSeconds", seconds, err)
		}
	}
}

func TestClose(t *testing.T) {
	from := time.Date(2026, 8, 29, 8, 0, 0, 0, time.UTC)
	s := mustNew(t, shared.Pick, 45, from)

	closedAt := from.Add(24 * time.Hour)
	s.Close(closedAt)

	if s.EffectiveTo() == nil || !s.EffectiveTo().Equal(closedAt) {
		t.Fatalf("EffectiveTo = %v, want %v", s.EffectiveTo(), closedAt)
	}
	// Closing must not mutate ExpectedSeconds/EffectiveFrom — this is
	// what keeps a frozen historical TaskPerformance row accurate after
	// a later revision.
	if s.ExpectedSeconds() != 45 {
		t.Fatalf("Close must not change ExpectedSeconds, got %d", s.ExpectedSeconds())
	}
	if !s.EffectiveFrom().Equal(from) {
		t.Fatalf("Close must not change EffectiveFrom, got %v", s.EffectiveFrom())
	}
}

func TestIsActiveAt(t *testing.T) {
	from := time.Date(2026, 8, 29, 8, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)

	t.Run("open-ended standard (never closed)", func(t *testing.T) {
		s := mustNew(t, shared.Pick, 45, from)
		if s.IsActiveAt(from.Add(-time.Second)) {
			t.Fatal("must not be active before EffectiveFrom")
		}
		if !s.IsActiveAt(from) {
			t.Fatal("must be active exactly at EffectiveFrom")
		}
		if !s.IsActiveAt(from.Add(1000 * time.Hour)) {
			t.Fatal("an open-ended standard must remain active arbitrarily far in the future")
		}
	})

	t.Run("closed standard", func(t *testing.T) {
		s := standard.Rehydrate("std-1", shared.Pick, 45, from, &to)
		if !s.IsActiveAt(from) {
			t.Fatal("must be active at EffectiveFrom")
		}
		if !s.IsActiveAt(to.Add(-time.Nanosecond)) {
			t.Fatal("must be active just before EffectiveTo")
		}
		if s.IsActiveAt(to) {
			t.Fatal("must NOT be active exactly at EffectiveTo (half-open interval)")
		}
		if s.IsActiveAt(to.Add(time.Hour)) {
			t.Fatal("must not be active after EffectiveTo")
		}
	})
}

func TestRehydrate_RoundTrip(t *testing.T) {
	from := time.Date(2026, 8, 29, 8, 0, 0, 0, time.UTC)
	s := standard.Rehydrate("std-42", shared.Slam, 60, from, nil)

	if s.ID() != "std-42" || s.TaskType() != shared.Slam || s.ExpectedSeconds() != 60 {
		t.Fatalf("unexpected rehydrated standard: %+v", s)
	}
}
