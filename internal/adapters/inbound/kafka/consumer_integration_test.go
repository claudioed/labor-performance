//go:build integration

package kafka_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	inboundkafka "github.com/claudioed/labor-performance/internal/adapters/inbound/kafka"
	"github.com/claudioed/labor-performance/internal/adapters/kafka/envelope"
	"github.com/claudioed/labor-performance/internal/adapters/outbound/events"
	"github.com/claudioed/labor-performance/internal/adapters/outbound/memory"
	"github.com/claudioed/labor-performance/internal/application/usecases"
	"github.com/claudioed/labor-performance/internal/domain/shared"
)

// TestConsumer_ProjectsRealBrokerMessages requires KAFKA_BROKERS to point
// at a running broker (the shared broker at
// ~/warehouse-systems/docker-compose.kafka.yml on localhost:9092). It
// publishes a TaskCompleted-shaped message onto
// warehouse.fulfillment.events, runs the real Consumer against it, and
// asserts a TaskPerformance was recorded. Run with:
//
//	KAFKA_BROKERS=localhost:9092 go test -tags=integration ./internal/adapters/inbound/kafka/...
func TestConsumer_ProjectsRealBrokerMessages(t *testing.T) {
	brokersCSV := os.Getenv("KAFKA_BROKERS")
	if brokersCSV == "" {
		t.Skip("KAFKA_BROKERS not set; skipping kafka integration test")
	}
	brokers := strings.Split(brokersCSV, ",")

	taskId := fmt.Sprintf("integration-kafka-task-%d", time.Now().UnixNano())
	associateId := fmt.Sprintf("integration-kafka-assoc-%d", time.Now().UnixNano())
	eventId := fmt.Sprintf("integration-kafka-evt-%d", time.Now().UnixNano())

	publishCtx, publishCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer publishCancel()

	writer := &kafkago.Writer{Addr: kafkago.TCP(brokers...), Topic: envelope.TopicFulfillmentEvents, AllowAutoTopicCreation: true}
	defer writer.Close()

	if err := writer.WriteMessages(publishCtx, kafkago.Message{
		Key: []byte(eventId),
		Value: mustEnvelopeJSON(t, eventId, envelope.EventTypeTaskCompleted, "fulfillment-execution", map[string]any{
			"task_id": taskId, "station_id": "station-1", "work_unit_id": "wu-1",
			"associate_id": associateId, "duration_seconds": 52,
		}),
	}); err != nil {
		t.Fatalf("publish TaskCompleted: %v", err)
	}

	standards := memory.NewStandardRepo()
	performances := memory.NewPerformanceRepo()
	processed := memory.NewProcessedEventRepo()
	publisher := events.NewLogPublisher(nil)
	clock := memory.SystemClock{}

	recordTaskPerformance := &usecases.RecordTaskPerformance{
		Performances: performances, Standards: standards, Processed: processed, Events: publisher, Clock: clock,
	}

	groupID := fmt.Sprintf("labor-performance-integration-test-%d", time.Now().UnixNano())
	consumer := inboundkafka.NewConsumer(brokers, groupID, recordTaskPerformance, nil)
	defer consumer.Close()

	consumeCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = consumer.Run(consumeCtx) }()

	deadline := time.Now().Add(30 * time.Second)
	for {
		exists, err := performances.ExistsByAssociateID(context.Background(), shared.AssociateId(associateId))
		if err == nil && exists {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("task performance never recorded from TaskCompleted event")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func mustEnvelopeJSON(t *testing.T, eventId, eventType, source string, data map[string]any) []byte {
	t.Helper()
	rawData, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	body, err := json.Marshal(envelope.Envelope{
		EventId: eventId, EventType: eventType, OccurredAt: time.Now().UTC(), Source: source, Data: rawData,
	})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return body
}
