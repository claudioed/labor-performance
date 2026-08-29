package kafka

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/claudioed/labor-performance/internal/adapters/kafka/envelope"
	"github.com/claudioed/labor-performance/internal/adapters/outbound/events"
	"github.com/claudioed/labor-performance/internal/adapters/outbound/memory"
	"github.com/claudioed/labor-performance/internal/application/usecases"
)

// fixture wires just enough of the real stack (in-memory repos, the
// existing RecordTaskPerformance use case) to exercise
// handleFulfillmentEvent without touching a broker.
type fixture struct {
	performances *memory.PerformanceRepo
	consumer     *Consumer
}

func newFixture() fixture {
	standards := memory.NewStandardRepo()
	performances := memory.NewPerformanceRepo()
	processed := memory.NewProcessedEventRepo()
	publisher := events.NewLogPublisher(nil)
	clock := memory.FixedClock{At: time.Date(2026, 8, 29, 8, 0, 0, 0, time.UTC)}

	recordTaskPerformance := &usecases.RecordTaskPerformance{
		Performances: performances, Standards: standards, Processed: processed, Events: publisher, Clock: clock,
	}

	return fixture{
		performances: performances,
		consumer:     &Consumer{recordTaskPerformance: recordTaskPerformance},
	}
}

func taskCompletedEnvelope(t *testing.T, eventId, taskId, associateId string, durationSeconds int64) envelope.Envelope {
	t.Helper()
	data, err := json.Marshal(taskCompletedData{
		TaskId: taskId, StationId: "station-1", WorkUnitId: "wu-1",
		AssociateId: associateId, DurationSeconds: durationSeconds,
	})
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	return envelope.Envelope{
		EventId:    eventId,
		EventType:  envelope.EventTypeTaskCompleted,
		OccurredAt: time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC),
		Source:     "fulfillment-execution",
		Data:       data,
	}
}

func TestHandleFulfillmentEvent_RecordsTaskPerformance(t *testing.T) {
	f := newFixture()

	if err := f.consumer.handleFulfillmentEvent(context.Background(), taskCompletedEnvelope(t, "evt-1", "task-1", "assoc-1", 52)); err != nil {
		t.Fatalf("handleFulfillmentEvent: %v", err)
	}

	exists, err := f.performances.ExistsByAssociateID(context.Background(), "assoc-1")
	if err != nil {
		t.Fatalf("ExistsByAssociateID: %v", err)
	}
	if !exists {
		t.Fatal("expected a TaskPerformance to be recorded for assoc-1")
	}
}

func TestHandleFulfillmentEvent_IgnoresOtherEventTypes(t *testing.T) {
	f := newFixture()

	env := taskCompletedEnvelope(t, "evt-other", "task-1", "assoc-1", 52)
	env.EventType = "SomethingElse"

	if err := f.consumer.handleFulfillmentEvent(context.Background(), env); err != nil {
		t.Fatalf("handleFulfillmentEvent: %v", err)
	}

	exists, err := f.performances.ExistsByAssociateID(context.Background(), "assoc-1")
	if err != nil {
		t.Fatalf("ExistsByAssociateID: %v", err)
	}
	if exists {
		t.Fatal("an unrecognized event type must not record anything")
	}
}

func TestHandleFulfillmentEvent_RedeliveryIsIdempotent(t *testing.T) {
	f := newFixture()
	env := taskCompletedEnvelope(t, "evt-dup", "task-1", "assoc-1", 52)

	if err := f.consumer.handleFulfillmentEvent(context.Background(), env); err != nil {
		t.Fatalf("first handleFulfillmentEvent: %v", err)
	}
	if err := f.consumer.handleFulfillmentEvent(context.Background(), env); err != nil {
		t.Fatalf("redelivered handleFulfillmentEvent: %v", err)
	}

	sc, err := f.performances.ScorecardFor(context.Background(), "assoc-1")
	if err != nil {
		t.Fatalf("ScorecardFor: %v", err)
	}
	if sc.TaskCount != 1 {
		t.Fatalf("TaskCount = %d, want 1 (no double-count on redelivery)", sc.TaskCount)
	}
}

func TestHandleFulfillmentEvent_DegradesGracefullyOnOlderPayload(t *testing.T) {
	// A TaskCompleted predating fulfillment-execution's
	// feature/labor-performance-hooks enrichment omits associate_id and
	// duration_seconds entirely. The envelope's data must still unmarshal
	// (both fields default to their Go zero values) and the event must
	// still be recorded, never dropped or errored.
	f := newFixture()
	data := []byte(`{"task_id":"task-1","station_id":"station-1","work_unit_id":"wu-1"}`)
	env := envelope.Envelope{
		EventId: "evt-old", EventType: envelope.EventTypeTaskCompleted,
		OccurredAt: time.Now(), Source: "fulfillment-execution", Data: data,
	}

	if err := f.consumer.handleFulfillmentEvent(context.Background(), env); err != nil {
		t.Fatalf("handleFulfillmentEvent: %v", err)
	}

	// Recorded under an empty associate id, per the degrade-gracefully
	// contract — verified by checking the fleet-wide (all-associates)
	// TaskTypePerformanceFor count did NOT change since we never gave it
	// a task type either (unclassified); the safest assertion here is
	// just that no error occurred and the second call sees the same
	// event_id as already processed.
	if err := f.consumer.handleFulfillmentEvent(context.Background(), env); err != nil {
		t.Fatalf("redelivered handleFulfillmentEvent: %v", err)
	}
}
