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

// Source identifies this service in the "source" field of any envelope it
// might publish in the future (v1 does not publish to Kafka — see
// SPEC.md's "Domain events" section).
const Source = "labor-performance"

// TopicFulfillmentEvents is the shared, fan-out topic this service
// consumes from — the SAME topic wes-work-planning already consumes,
// per SPEC.md's "Inbound Kafka contract" section.
const TopicFulfillmentEvents = "warehouse.fulfillment.events"

// EventTypeTaskCompleted is the only event type on TopicFulfillmentEvents
// this service acts on; every other event type is silently skipped.
const EventTypeTaskCompleted = "TaskCompleted"
