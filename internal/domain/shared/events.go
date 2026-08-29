package shared

import "time"

// DomainEvent is a past-tense fact produced by an aggregate in this
// domain. The outbound event publisher serializes and logs these; the
// domain never depends on the publishing mechanism. Per SPEC.md, v1 ships
// a log publisher only — no Kafka publish of these events is required
// (they are not yet consumed by any other service).
type DomainEvent interface {
	EventName() string
	OccurredAt() time.Time
}

type base struct {
	Name string    `json:"eventName"`
	At   time.Time `json:"occurredAt"`
}

func (b base) EventName() string     { return b.Name }
func (b base) OccurredAt() time.Time { return b.At }

func newBase(name string, occurredAt time.Time) base {
	return base{Name: name, At: occurredAt}
}

// LaborStandardDefined: a TaskType had no active standard, and one was
// just defined.
type LaborStandardDefined struct {
	base
	StandardId      StandardId
	TaskType        TaskType
	ExpectedSeconds int64
	EffectiveFrom   time.Time
}

func NewLaborStandardDefined(occurredAt time.Time, id StandardId, taskType TaskType, expectedSeconds int64, effectiveFrom time.Time) LaborStandardDefined {
	return LaborStandardDefined{
		base:            newBase("LaborStandardDefined", occurredAt),
		StandardId:      id,
		TaskType:        taskType,
		ExpectedSeconds: expectedSeconds,
		EffectiveFrom:   effectiveFrom,
	}
}

// LaborStandardRevised: a TaskType already had an active standard, which
// was closed, and a new one started in its place (append-only history —
// the prior standard's row is never overwritten, only closed).
type LaborStandardRevised struct {
	base
	StandardId              StandardId
	TaskType                TaskType
	PreviousExpectedSeconds int64
	NewExpectedSeconds      int64
	EffectiveFrom           time.Time
}

func NewLaborStandardRevised(occurredAt time.Time, id StandardId, taskType TaskType, previousExpectedSeconds, newExpectedSeconds int64, effectiveFrom time.Time) LaborStandardRevised {
	return LaborStandardRevised{
		base:                    newBase("LaborStandardRevised", occurredAt),
		StandardId:              id,
		TaskType:                taskType,
		PreviousExpectedSeconds: previousExpectedSeconds,
		NewExpectedSeconds:      newExpectedSeconds,
		EffectiveFrom:           effectiveFrom,
	}
}

// TaskPerformanceRecorded: one completed task was scored against whatever
// standard was active at completion time (or no standard, if none was).
type TaskPerformanceRecorded struct {
	base
	TaskId        string
	AssociateId   AssociateId
	TaskType      TaskType
	ActualSeconds int64
	EfficiencyPct *float64
	CompletedAt   time.Time
}

func NewTaskPerformanceRecorded(occurredAt time.Time, taskId string, associateId AssociateId, taskType TaskType, actualSeconds int64, efficiencyPct *float64, completedAt time.Time) TaskPerformanceRecorded {
	return TaskPerformanceRecorded{
		base:          newBase("TaskPerformanceRecorded", occurredAt),
		TaskId:        taskId,
		AssociateId:   associateId,
		TaskType:      taskType,
		ActualSeconds: actualSeconds,
		EfficiencyPct: efficiencyPct,
		CompletedAt:   completedAt,
	}
}
