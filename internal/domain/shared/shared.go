// Package shared holds the value objects, domain events, and error types
// common to both aggregates in the Labor Performance domain: LaborStandard
// and TaskPerformance.
package shared

import "errors"

// TaskType is the task type an engineered labor standard applies to, and
// (when known) the type a scored TaskPerformance belongs to. It mirrors
// fulfillment-execution's task.Type enum exactly (PICK, PACK, SLAM) — this
// context does not invent new task types.
type TaskType string

const (
	Pick TaskType = "PICK"
	Pack TaskType = "PACK"
	Slam TaskType = "SLAM"
)

// ErrUnknownTaskType is returned when a caller-supplied (REST path/body)
// task type string is not one of the three known values.
var ErrUnknownTaskType = errors.New("unknown task type: must be PICK, PACK, or SLAM")

// NewTaskType validates a caller-supplied task type string. Used at the
// REST boundary (DefineStandard, GetStandard, GetTaskTypePerformance),
// where an unrecognized value is a caller mistake worth rejecting with a
// 400. Contrast with ParseTaskTypeLenient, used only when consuming a
// TaskCompleted Kafka event, where an unrecognized/absent value must never
// block recording real performance data.
func NewTaskType(value string) (TaskType, error) {
	switch TaskType(value) {
	case Pick, Pack, Slam:
		return TaskType(value), nil
	default:
		return "", ErrUnknownTaskType
	}
}

// ParseTaskTypeLenient coerces raw into a known TaskType, or "" ("
// unclassified") when raw is empty or not one of the three known values.
//
// This exists because of a genuine, documented wire-contract gap: as
// verified against fulfillment-execution's feature/labor-performance-hooks
// publisher this session, TaskCompletedData carries no task_type field at
// all today (only task_id, station_id, work_unit_id, associate_id,
// duration_seconds — see the exact envelope in SPEC.md's "Inbound Kafka
// contract" section). Since this service MUST NOT call fulfillment-
// execution synchronously to resolve a task's type, and a TaskCompleted
// event is a real, must-be-recorded business fact regardless, an
// absent/unrecognized task_type degrades to "" (unclassified) exactly like
// an absent associate_id degrades to "" — it never blocks recording the
// row. A "" TaskType simply never has a LaborStandard to compare against
// (no lookup is possible without a known type) and never appears under any
// GetTaskTypePerformance query (those require one of the three known
// values), which is documented explicitly in the README as a known v1 gap
// pending a future fulfillment-execution enrichment.
func ParseTaskTypeLenient(raw string) TaskType {
	switch TaskType(raw) {
	case Pick, Pack, Slam:
		return TaskType(raw)
	default:
		return ""
	}
}

// AssociateId identifies the associate who completed a task. Empty is a
// legitimate, expected value — fulfillment-execution's TaskCompleted
// enrichment supplies "" when the completing station had no checked-in
// occupant (e.g. a robot station).
type AssociateId string

// StandardId identifies one LaborStandard record (one row in its
// append-only history — a revision mints a new StandardId rather than
// reusing the prior one).
type StandardId string
