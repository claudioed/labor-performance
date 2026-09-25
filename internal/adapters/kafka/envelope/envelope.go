// Package envelope defines the integration-event wire format shared by
// every warehouse-systems service: a fixed envelope with an event-type-
// specific JSON payload. Used by the inbound Kafka adapter so it agrees
// with fulfillment-execution's publisher on the wire format without
// depending on that repo's Go code.
package envelope

import (
	"encoding/json"
	"time"
)

// Envelope is the identical outer shape published/consumed across all
// warehouse-systems services.
type Envelope struct {
	EventId    string          `json:"event_id"`
	EventType  string          `json:"event_type"`
	OccurredAt time.Time       `json:"occurred_at"`
	Source     string          `json:"source"`
	Data       json.RawMessage `json:"data"`
}

// AnalyticsEnvelope is the analytics-stream variant of Envelope: the same
// outer shape plus an explicit schema_version, matching the shape every
// sibling service's analytics topic already uses (ADR-0007).
//
// It is a SEPARATE type from Envelope rather than a widened one, because
// Envelope is the shape of the INTEGRATION contract this service consumes
// from fulfillment-execution. Adding a field there to serve the analytics
// stream would couple two contracts that must evolve independently — the
// same reasoning that gives analytics its own topic in the first place.
type AnalyticsEnvelope struct {
	EventId       string          `json:"event_id"`
	EventType     string          `json:"event_type"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Source        string          `json:"source"`
	SchemaVersion int             `json:"schema_version"`
	Data          json.RawMessage `json:"data"`
}

// Source identifies this service in the "source" field of every envelope
// it publishes — today, the analytics envelopes on
// TopicLaborPerformanceAnalytics (ADR-0007). The OLTP integration side
// still publishes nothing to Kafka; see CLAUDE.md's "Domain events"
// section.
const Source = "labor-performance"

// TopicLaborPerformanceAnalytics is the dedicated topic this service's
// analytical data product is fed by. It is SEPARATE from any integration
// topic so the analytical stream and the (currently empty) integration
// contract evolve independently, and it is named with the same
// warehouse.<context>.analytics convention every sibling service uses —
// note the hyphen in the context segment.
const TopicLaborPerformanceAnalytics = "warehouse.labor-performance.analytics"

// AnalyticsSchemaVersion is the schema version stamped onto every
// analytics envelope this service emits.
const AnalyticsSchemaVersion = 1

// The analytics event types carried on TopicLaborPerformanceAnalytics —
// this service's own past-tense domain events, verbatim. Any other
// event_type appearing on that topic is silently skipped by the
// projector.
const (
	EventTypeLaborStandardDefined    = "LaborStandardDefined"
	EventTypeLaborStandardRevised    = "LaborStandardRevised"
	EventTypeTaskPerformanceRecorded = "TaskPerformanceRecorded"
)

// TopicLaborPerformanceEvents is this service's INTEGRATION topic — its
// Open-Host-Service Published Language for other bounded contexts, first
// added by ADR 0013. It is SEPARATE from TopicLaborPerformanceAnalytics
// (which feeds only this repo's own projector) so the two streams evolve
// independently, and it is named with the same warehouse.<context>.events
// convention fulfillment-execution's own integration topic
// (warehouse.fulfillment.events) uses. Before ADR 0013 this service
// published no integration event at all — it was the fleet's only pure
// event sink.
const TopicLaborPerformanceEvents = "warehouse.labor-performance.events"

// TopicFulfillmentEvents is the shared, fan-out topic this service
// consumes from — the SAME topic wes-work-planning already consumes,
// per CLAUDE.md's "Inbound Kafka contract" section.
const TopicFulfillmentEvents = "warehouse.fulfillment.events"

// EventTypeTaskCompleted is the only event type on TopicFulfillmentEvents
// this service acts on; every other event type is silently skipped.
const EventTypeTaskCompleted = "TaskCompleted"
