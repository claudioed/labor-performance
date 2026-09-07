//go:build integration

package kafka_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"

	kafkago "github.com/segmentio/kafka-go"
	"github.com/testcontainers/testcontainers-go"
	tckafka "github.com/testcontainers/testcontainers-go/modules/kafka"

	inboundkafka "github.com/claudioed/labor-performance/internal/adapters/inbound/kafka"
	"github.com/claudioed/labor-performance/internal/adapters/kafka/envelope"
	"github.com/claudioed/labor-performance/internal/adapters/outbound/events"
	"github.com/claudioed/labor-performance/internal/adapters/outbound/memory"
	"github.com/claudioed/labor-performance/internal/application/usecases"
	"github.com/claudioed/labor-performance/internal/domain/shared"
)

// TestConsumer_ProjectsRealBrokerMessages runs the real consumer against an
// isolated Kafka Testcontainers broker, then verifies a TaskCompleted envelope
// records task performance.
func TestConsumer_ProjectsRealBrokerMessages(t *testing.T) {
	testCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	container, err := tckafka.Run(testCtx, "confluentinc/confluent-local:7.6.1",
		tckafka.WithClusterID(fmt.Sprintf("labor-performance-itest-%d", time.Now().UnixNano())))
	if err != nil {
		t.Fatalf("start Kafka container: %v", err)
	}
	defer func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Errorf("terminate Kafka container: %v", err)
		}
	}()

	brokers, err := container.Brokers(testCtx)
	if err != nil {
		t.Fatalf("get Kafka brokers: %v", err)
	}

	topic := fmt.Sprintf("labor-performance-itest-%d", time.Now().UnixNano())
	createTopicAndWaitForLeader(t, testCtx, brokers[0], topic)

	taskID := fmt.Sprintf("integration-kafka-task-%d", time.Now().UnixNano())
	associateID := fmt.Sprintf("integration-kafka-assoc-%d", time.Now().UnixNano())
	eventID := fmt.Sprintf("integration-kafka-evt-%d", time.Now().UnixNano())

	standards := memory.NewStandardRepo()
	performances := memory.NewPerformanceRepo()
	processed := memory.NewProcessedEventRepo()
	recordTaskPerformance := &usecases.RecordTaskPerformance{
		Performances: performances,
		Standards:    standards,
		Processed:    processed,
		Events:       events.NewLogPublisher(nil),
		Clock:        memory.SystemClock{},
	}

	consumer := inboundkafka.NewConsumerForTopic(
		brokers,
		fmt.Sprintf("labor-performance-integration-test-%d", time.Now().UnixNano()),
		topic,
		recordTaskPerformance,
		nil,
	)
	defer consumer.Close()

	consumeCtx, consumeCancel := context.WithCancel(testCtx)
	runErr := make(chan error, 1)
	go func() { runErr <- consumer.Run(consumeCtx) }()

	writer := &kafkago.Writer{Addr: kafkago.TCP(brokers...), Topic: topic}
	defer writer.Close()
	if err := writer.WriteMessages(testCtx, kafkago.Message{
		Key: []byte(eventID),
		Value: mustEnvelopeJSON(t, eventID, envelope.EventTypeTaskCompleted, "fulfillment-execution", map[string]any{
			"task_id": taskID, "station_id": "station-1", "work_unit_id": "wu-1",
			"associate_id": associateID, "duration_seconds": 52,
		}),
	}); err != nil {
		t.Fatalf("publish TaskCompleted: %v", err)
	}

	waitForPerformance(t, testCtx, performances, shared.AssociateId(associateID))
	consumeCancel()
	select {
	case err := <-runErr:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("run consumer: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Error("consumer did not stop after context cancellation")
	}
}

func createTopicAndWaitForLeader(t *testing.T, ctx context.Context, broker, topic string) {
	t.Helper()

	conn, err := kafkago.DialContext(ctx, "tcp", broker)
	if err != nil {
		t.Fatalf("dial Kafka broker: %v", err)
	}
	defer conn.Close()

	controller, err := conn.Controller()
	if err != nil {
		t.Fatalf("get Kafka controller: %v", err)
	}
	controllerConn, err := kafkago.DialContext(ctx, "tcp", net.JoinHostPort(controller.Host, strconv.Itoa(controller.Port)))
	if err != nil {
		t.Fatalf("dial Kafka controller: %v", err)
	}
	defer controllerConn.Close()

	if err := controllerConn.CreateTopics(kafkago.TopicConfig{Topic: topic, NumPartitions: 1, ReplicationFactor: 1}); err != nil {
		t.Fatalf("create Kafka topic %q: %v", topic, err)
	}

	deadline := time.Now().Add(20 * time.Second)
	for {
		partitions, err := conn.ReadPartitions(topic)
		if err == nil && len(partitions) > 0 && partitions[0].Leader.ID >= 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Kafka topic %q did not receive a partition leader: %v", topic, err)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for Kafka topic leader: %v", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func waitForPerformance(t *testing.T, ctx context.Context, performances *memory.PerformanceRepo, associateID shared.AssociateId) {
	t.Helper()

	for {
		exists, err := performances.ExistsByAssociateID(ctx, associateID)
		if err == nil && exists {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("task performance was not recorded from TaskCompleted event: %v", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func mustEnvelopeJSON(t *testing.T, eventID, eventType, source string, data map[string]any) []byte {
	t.Helper()
	rawData, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	body, err := json.Marshal(envelope.Envelope{
		EventId: eventID, EventType: eventType, OccurredAt: time.Now().UTC(), Source: source, Data: rawData,
	})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return body
}
