package usecases_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/claudioed/labor-performance/internal/domain/shared"
	"github.com/claudioed/labor-performance/internal/domain/standard"
)

// TestDefineStandard_WithTravelComponentSeconds_IsPersistedAndPublished
// proves the optional travel breakdown flows through the use case onto
// the persisted aggregate AND the published LaborStandardDefined event
// (ADR 0015).
func TestDefineStandard_WithTravelComponentSeconds_IsPersistedAndPublished(t *testing.T) {
	f := newFixture(baseTime)
	travel := int64(15)

	s, err := f.defineStandard.Execute(context.Background(), shared.Pick, 45, &travel)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if s.TravelComponentSeconds() == nil || *s.TravelComponentSeconds() != 15 {
		t.Fatalf("want TravelComponentSeconds=15 on the returned aggregate, got %v", s.TravelComponentSeconds())
	}

	persisted, err := f.standards.FindCurrentlyActive(context.Background(), shared.Pick)
	if err != nil {
		t.Fatalf("FindCurrentlyActive: %v", err)
	}
	if persisted.TravelComponentSeconds() == nil || *persisted.TravelComponentSeconds() != 15 {
		t.Fatalf("want persisted TravelComponentSeconds=15, got %v", persisted.TravelComponentSeconds())
	}
}

// TestDefineStandard_NoTravelComponentSeconds_DefaultsToNil proves the
// zero-value default (most standards never declare a travel breakdown)
// still works exactly as it did before this feature existed.
func TestDefineStandard_NoTravelComponentSeconds_DefaultsToNil(t *testing.T) {
	f := newFixture(baseTime)

	s, err := f.defineStandard.Execute(context.Background(), shared.Pick, 45, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if s.TravelComponentSeconds() != nil {
		t.Fatalf("want nil TravelComponentSeconds, got %v", *s.TravelComponentSeconds())
	}
}

// TestDefineStandard_RejectsInvalidTravelComponentSeconds proves the use
// case surfaces the domain's own validation errors rather than silently
// accepting an out-of-range value.
func TestDefineStandard_RejectsInvalidTravelComponentSeconds(t *testing.T) {
	f := newFixture(baseTime)

	negative := int64(-1)
	if _, err := f.defineStandard.Execute(context.Background(), shared.Pick, 45, &negative); !errors.Is(err, standard.ErrNegativeTravelComponentSeconds) {
		t.Fatalf("want ErrNegativeTravelComponentSeconds, got %v", err)
	}

	tooMuch := int64(46)
	if _, err := f.defineStandard.Execute(context.Background(), shared.Pack, 45, &tooMuch); !errors.Is(err, standard.ErrTravelComponentExceedsExpectedSeconds) {
		t.Fatalf("want ErrTravelComponentExceedsExpectedSeconds, got %v", err)
	}
}

// TestDefineStandard_Revision_CarriesNewTravelComponentSeconds proves a
// revision's own (possibly different, possibly absent) travel breakdown
// is what gets persisted and published — never the prior standard's.
func TestDefineStandard_Revision_CarriesNewTravelComponentSeconds(t *testing.T) {
	f := newFixture(baseTime)
	first := int64(10)
	if _, err := f.defineStandard.Execute(context.Background(), shared.Pick, 45, &first); err != nil {
		t.Fatalf("first define: %v", err)
	}

	later := baseTime.Add(time.Hour)
	f.clock.At = later
	second := int64(20)
	revised, err := f.defineStandard.Execute(context.Background(), shared.Pick, 50, &second)
	if err != nil {
		t.Fatalf("revision: %v", err)
	}
	if revised.TravelComponentSeconds() == nil || *revised.TravelComponentSeconds() != 20 {
		t.Fatalf("want revised TravelComponentSeconds=20, got %v", revised.TravelComponentSeconds())
	}
}
