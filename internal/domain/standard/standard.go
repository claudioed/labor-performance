// Package standard implements the LaborStandard aggregate: how long a
// TaskType should take. A standard can be revised, but revising it never
// overwrites the prior record in place — it closes the prior record's
// effective range and starts a new one, so already-recorded
// TaskPerformance rows' frozen StandardSecondsAtCompletion values remain
// historically accurate even after a later revision (see ADR
// 0004-standard-frozen-at-completion-time-not-recomputed.md).
package standard

import (
	"errors"
	"time"

	"github.com/claudioed/labor-performance/internal/domain/shared"
)

// ErrNonPositiveExpectedSeconds enforces the one aggregate invariant on
// LaborStandard: ExpectedSeconds must be > 0. A standard that says a task
// should take zero or negative seconds is not a business fact.
var ErrNonPositiveExpectedSeconds = errors.New("expected seconds must be greater than zero")

// LaborStandard is the aggregate root for "how long a task TYPE should
// take". EffectiveTo is nil while the standard is the currently active one
// for its TaskType; Close sets it once a revision supersedes this record.
type LaborStandard struct {
	id              shared.StandardId
	taskType        shared.TaskType
	expectedSeconds int64
	effectiveFrom   time.Time
	effectiveTo     *time.Time
}

// New constructs a LaborStandard, freshly active (EffectiveTo nil) from
// effectiveFrom onward. Returns ErrNonPositiveExpectedSeconds if
// expectedSeconds <= 0.
func New(id shared.StandardId, taskType shared.TaskType, expectedSeconds int64, effectiveFrom time.Time) (*LaborStandard, error) {
	if expectedSeconds <= 0 {
		return nil, ErrNonPositiveExpectedSeconds
	}
	return &LaborStandard{
		id:              id,
		taskType:        taskType,
		expectedSeconds: expectedSeconds,
		effectiveFrom:   effectiveFrom,
	}, nil
}

// Rehydrate reconstructs a LaborStandard from persisted state without
// re-validating construction invariants (used by repository adapters).
func Rehydrate(id shared.StandardId, taskType shared.TaskType, expectedSeconds int64, effectiveFrom time.Time, effectiveTo *time.Time) *LaborStandard {
	return &LaborStandard{
		id:              id,
		taskType:        taskType,
		expectedSeconds: expectedSeconds,
		effectiveFrom:   effectiveFrom,
		effectiveTo:     effectiveTo,
	}
}

// Close ends this standard's effective range at at, marking it superseded
// by a revision. It does NOT overwrite ExpectedSeconds or EffectiveFrom —
// this record remains exactly as it was while it was active, which is what
// keeps historically-frozen TaskPerformance rows accurate.
func (s *LaborStandard) Close(at time.Time) {
	s.effectiveTo = &at
}

// IsActiveAt reports whether this standard was the active one for its
// TaskType at instant t: t is on or after EffectiveFrom, and strictly
// before EffectiveTo (or EffectiveTo is nil, meaning still active).
func (s *LaborStandard) IsActiveAt(t time.Time) bool {
	if t.Before(s.effectiveFrom) {
		return false
	}
	if s.effectiveTo != nil && !t.Before(*s.effectiveTo) {
		return false
	}
	return true
}

func (s *LaborStandard) ID() shared.StandardId     { return s.id }
func (s *LaborStandard) TaskType() shared.TaskType { return s.taskType }
func (s *LaborStandard) ExpectedSeconds() int64    { return s.expectedSeconds }
func (s *LaborStandard) EffectiveFrom() time.Time  { return s.effectiveFrom }
func (s *LaborStandard) EffectiveTo() *time.Time   { return s.effectiveTo }
