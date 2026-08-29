package performance_test

import (
	"errors"
	"testing"
	"time"

	"github.com/claudioed/labor-performance/internal/domain/performance"
	"github.com/claudioed/labor-performance/internal/domain/shared"
)

var completedAt = time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC)

func TestNew_Success(t *testing.T) {
	p, err := performance.New("evt-1", "task-1", shared.AssociateId("assoc-1"), shared.Pick, 52, 45, completedAt)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if p.EventId() != "evt-1" || p.TaskId() != "task-1" {
		t.Fatalf("unexpected identity: %+v", p)
	}
	if p.AssociateId() != "assoc-1" {
		t.Fatalf("AssociateId = %v", p.AssociateId())
	}
	if p.ActualSeconds() != 52 || p.StandardSecondsAtCompletion() != 45 {
		t.Fatalf("unexpected durations: actual=%d standard=%d", p.ActualSeconds(), p.StandardSecondsAtCompletion())
	}
	if p.EfficiencyPct() == nil {
		t.Fatal("EfficiencyPct must be set when both durations are positive")
	}
	want := 100 * 45.0 / 52.0
	if got := *p.EfficiencyPct(); got != want {
		t.Fatalf("EfficiencyPct = %v, want %v", got, want)
	}
}

func TestNew_FailingPath_EmptyEventId(t *testing.T) {
	_, err := performance.New("", "task-1", "", shared.Pick, 52, 45, completedAt)
	if !errors.Is(err, performance.ErrEmptyEventId) {
		t.Fatalf("error = %v, want ErrEmptyEventId", err)
	}
}

func TestNew_FailingPath_EmptyTaskId(t *testing.T) {
	_, err := performance.New("evt-1", "", "", shared.Pick, 52, 45, completedAt)
	if !errors.Is(err, performance.ErrEmptyTaskId) {
		t.Fatalf("error = %v, want ErrEmptyTaskId", err)
	}
}

// TestEfficiencyPct_NeverDividesByZero exercises every named invariant
// case from CLAUDE.md's "EfficiencyPct computation" section.
func TestEfficiencyPct_NeverDividesByZero(t *testing.T) {
	cases := []struct {
		name                        string
		actualSeconds               int64
		standardSecondsAtCompletion int64
	}{
		{"zero actual seconds (unmeasurable completion)", 0, 45},
		{"negative actual seconds (defensive)", -1, 45},
		{"zero standard seconds (no active standard at completion time)", 52, 0},
		{"both zero", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := performance.New("evt-1", "task-1", "", shared.Pick, tc.actualSeconds, tc.standardSecondsAtCompletion, completedAt)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if p.EfficiencyPct() != nil {
				t.Fatalf("EfficiencyPct = %v, want nil", *p.EfficiencyPct())
			}
			if p.ActualSeconds() != tc.actualSeconds {
				t.Fatalf("ActualSeconds must still be recorded as-is, got %d want %d", p.ActualSeconds(), tc.actualSeconds)
			}
		})
	}
}

func TestNew_EmptyAssociateIdIsLegitimate(t *testing.T) {
	// A TaskCompleted with no checked-in occupant (e.g. a robot station)
	// must still be recorded — an empty AssociateId is not an error.
	p, err := performance.New("evt-1", "task-1", "", shared.Pick, 52, 45, completedAt)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if p.AssociateId() != "" {
		t.Fatalf("AssociateId = %q, want empty", p.AssociateId())
	}
}

func TestNew_UnclassifiedTaskTypeIsLegitimate(t *testing.T) {
	// A TaskCompleted whose task type could not be resolved degrades to
	// "" (unclassified) rather than blocking the recording.
	p, err := performance.New("evt-1", "task-1", "", "", 52, 0, completedAt)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if p.TaskType() != "" {
		t.Fatalf("TaskType = %q, want empty", p.TaskType())
	}
}

func TestRehydrate_TrustsPersistedEfficiencyPct(t *testing.T) {
	// Rehydrate must never recompute EfficiencyPct — the persisted value
	// is a frozen historical fact, trusted as-is.
	pct := 999.0 // deliberately "wrong" vs actual/standard, to prove no recompute happens
	p := performance.Rehydrate("evt-1", "task-1", "assoc-1", shared.Pick, 52, 45, &pct, completedAt)
	if p.EfficiencyPct() == nil || *p.EfficiencyPct() != 999.0 {
		t.Fatalf("EfficiencyPct = %v, want the persisted 999.0 (no recompute)", p.EfficiencyPct())
	}
}
