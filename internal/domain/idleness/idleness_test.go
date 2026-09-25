package idleness_test

import (
	"errors"
	"testing"
	"time"

	"github.com/claudioed/labor-performance/internal/domain/idleness"
	"github.com/claudioed/labor-performance/internal/domain/shared"
)

var started = time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)

func TestNew_Success_UncappedGap(t *testing.T) {
	ended := started.Add(90 * time.Second)
	p, err := idleness.New("assoc-1", shared.Pick, started, ended, 3600)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if p.AssociateId() != "assoc-1" || p.TaskType() != shared.Pick {
		t.Fatalf("unexpected identity: %+v", p)
	}
	if p.Seconds() != 90 {
		t.Fatalf("Seconds = %d, want 90", p.Seconds())
	}
	if p.Capped() {
		t.Fatal("a 90s gap under a 3600s cap must not be reported as capped")
	}
}

func TestNew_Success_CappedGap(t *testing.T) {
	ended := started.Add(2 * time.Hour)
	p, err := idleness.New("assoc-1", shared.Pick, started, ended, 3600)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if p.Seconds() != 3600 {
		t.Fatalf("Seconds = %d, want the capped 3600", p.Seconds())
	}
	if !p.Capped() {
		t.Fatal("a gap exceeding the cap must be reported as capped")
	}
	// EndedAt/StartedAt must still reflect the REAL boundaries, not the
	// capped duration -- capping only clips the reported Seconds.
	if !p.EndedAt().Equal(ended) || !p.StartedAt().Equal(started) {
		t.Fatalf("boundaries must remain uncapped: started=%v ended=%v", p.StartedAt(), p.EndedAt())
	}
}

func TestNew_NoCapWhenCapSecondsIsZeroOrNegative(t *testing.T) {
	ended := started.Add(2 * time.Hour)
	for _, capSeconds := range []int64{0, -1} {
		p, err := idleness.New("assoc-1", shared.Pick, started, ended, capSeconds)
		if err != nil {
			t.Fatalf("New(cap=%d): %v", capSeconds, err)
		}
		if p.Capped() {
			t.Fatalf("cap=%d must mean 'no cap', got Capped=true", capSeconds)
		}
		if p.Seconds() != 7200 {
			t.Fatalf("Seconds = %d, want the full uncapped 7200", p.Seconds())
		}
	}
}

// TestNew_GapExactlyAtCap_NotReportedAsCapped covers the boundary case:
// a gap whose raw duration equals the cap exactly must NOT be reported
// as capped -- capping only applies to a gap that exceeds the cap, never
// one that lands exactly on it.
func TestNew_GapExactlyAtCap_NotReportedAsCapped(t *testing.T) {
	ended := started.Add(3600 * time.Second)
	p, err := idleness.New("assoc-1", shared.Pick, started, ended, 3600)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if p.Capped() {
		t.Fatal("a gap exactly at the cap must not be reported as capped")
	}
	if p.Seconds() != 3600 {
		t.Fatalf("Seconds = %d, want 3600", p.Seconds())
	}
}

func TestNew_FailingPath_EmptyAssociateId(t *testing.T) {
	_, err := idleness.New("", shared.Pick, started, started.Add(time.Minute), 3600)
	if !errors.Is(err, idleness.ErrEmptyAssociateId) {
		t.Fatalf("error = %v, want ErrEmptyAssociateId", err)
	}
}

// TestNew_FailingPath_NegativeGap covers the routine (not exceptional)
// out-of-order-Kafka-delivery case: a "next" claim instant that lands
// before or at the previous completion.
func TestNew_FailingPath_NegativeGap(t *testing.T) {
	cases := []struct {
		name    string
		endedAt time.Time
	}{
		{"ended before started", started.Add(-time.Second)},
		{"ended equal to started (zero gap)", started},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := idleness.New("assoc-1", shared.Pick, started, tc.endedAt, 3600)
			if !errors.Is(err, idleness.ErrNegativeGap) {
				t.Fatalf("error = %v, want ErrNegativeGap", err)
			}
		})
	}
}

func TestRehydrate_TrustsPersistedState(t *testing.T) {
	ended := started.Add(time.Hour)
	// Deliberately "wrong" vs the real started/ended delta, to prove no
	// recompute happens -- Rehydrate trusts the persisted value exactly
	// like performance.Rehydrate does for EfficiencyPct.
	p := idleness.Rehydrate("assoc-1", shared.Pick, started, ended, 999, true)
	if p.Seconds() != 999 || !p.Capped() {
		t.Fatalf("Rehydrate must trust persisted state as-is, got seconds=%d capped=%v", p.Seconds(), p.Capped())
	}
}

func TestUtilizationPct_NeverDividesByZero(t *testing.T) {
	if got := idleness.UtilizationPct(0, 0); got != nil {
		t.Fatalf("UtilizationPct(0,0) = %v, want nil", *got)
	}
}

func TestUtilizationPct_ComputesShareCorrectly(t *testing.T) {
	cases := []struct {
		name                     string
		taskSeconds, idleSeconds int64
		want                     float64
	}{
		{"all task time, no idle", 100, 0, 100},
		{"all idle, no task time", 0, 100, 0},
		{"even split", 50, 50, 50},
		{"mostly idle", 25, 75, 25},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := idleness.UtilizationPct(tc.taskSeconds, tc.idleSeconds)
			if got == nil {
				t.Fatal("UtilizationPct = nil, want a value")
			}
			if *got != tc.want {
				t.Fatalf("UtilizationPct = %v, want %v", *got, tc.want)
			}
		})
	}
}
