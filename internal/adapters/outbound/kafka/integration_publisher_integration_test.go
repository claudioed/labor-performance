//go:build integration

package kafka_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	kafkago "github.com/segmentio/kafka-go"
	"github.com/testcontainers/testcontainers-go"
	tckafka "github.com/testcontainers/testcontainers-go/modules/kafka"

	"github.com/claudioed/labor-performance/internal/adapters/kafka/envelope"
	outboundkafka "github.com/claudioed/labor-performance/internal/adapters/outbound/kafka"
	"github.com/claudioed/labor-performance/internal/domain/shared"
)

// TestIntegrationPublisher_PublishesTaskPerformanceRecordedToRealBroker
// proves that, with EVENT_PUBLISHER=kafka wiring (the direct, no-DB path
// this test exercises via outboundkafka.NewIntegrationPublisher directly,
// mirroring how cmd/labor/main.go's buildEventPublisher constructs it), a
// TaskPerformanceRecorded domain event actually lands on
// warehouse.labor-performance.events — the integration topic added by
// ADR 0013 — on a real Kafka broker, with the exact wire envelope a
// downstream consumer (e.g. workforce-management) would decode.
func TestIntegrationPublisher_PublishesTaskPerformanceRecordedToRealBroker(t *testing.T) {
	testCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	container, err := tckafka.Run(testCtx, "confluentinc/confluent-local:7.6.1",
		tckafka.WithClusterID(fmt.Sprintf("labor-performance-integration-itest-%d", time.Now().UnixNano())))
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

	// Use a throwaway, uniquely-named topic rather than the real
	// envelope.TopicLaborPerformanceEvents constant, so this test never
	// collides with another run against the same container/topic name
	// convention. The publisher under test is otherwise wired exactly
	// as production (NewIntegrationPublisher), just pointed at this
	// topic via a fresh Writer below to keep topic name test-local.
	topic := fmt.Sprintf("labor-performance-events-itest-%d", time.Now().UnixNano())
	createTopicAndWaitForLeaderIntegration(t, testCtx, brokers[0], topic)

	publisher := &outboundkafka.IntegrationPublisher{
		Writer: &kafkago.Writer{
			Addr:                   kafkago.TCP(brokers...),
			Topic:                  topic,
			Balancer:               &kafkago.LeastBytes{},
			AllowAutoTopicCreation: true,
		},
		NewID: uuid.NewString,
	}
	defer publisher.Close()

	pct := 91.2
	taskID := fmt.Sprintf("integration-task-%d", time.Now().UnixNano())
	associateID := fmt.Sprintf("integration-assoc-%d", time.Now().UnixNano())
	completedAt := time.Now().UTC().Truncate(time.Second)
	event := shared.NewTaskPerformanceRecorded(
		time.Now().UTC(), taskID, shared.AssociateId(associateID), shared.Pick, 41, &pct, nil, completedAt)

	if err := publisher.Publish(testCtx, event); err != nil {
		t.Fatalf("publish TaskPerformanceRecorded: %v", err)
	}

	reader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:   brokers,
		Topic:     topic,
		Partition: 0,
		MinBytes:  1,
		MaxBytes:  10e6,
	})
	defer reader.Close()
	if err := reader.SetOffset(0); err != nil {
		t.Fatalf("set reader offset: %v", err)
	}

	readCtx, readCancel := context.WithTimeout(testCtx, 30*time.Second)
	defer readCancel()
	msg, err := reader.ReadMessage(readCtx)
	if err != nil {
		t.Fatalf("read message from %s: %v", topic, err)
	}

	if string(msg.Key) != associateID {
		t.Errorf("partition key = %q, want %q (AssociateId)", msg.Key, associateID)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(msg.Value, &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.EventType != envelope.EventTypeTaskPerformanceRecorded {
		t.Errorf("event_type = %q, want %q", env.EventType, envelope.EventTypeTaskPerformanceRecorded)
	}
	if env.Source != envelope.Source {
		t.Errorf("source = %q, want %q", env.Source, envelope.Source)
	}
	if env.EventId == "" {
		t.Error("event_id is empty")
	}

	var data map[string]any
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if data["task_id"] != taskID {
		t.Errorf("task_id = %v, want %v", data["task_id"], taskID)
	}
	if data["associate_id"] != associateID {
		t.Errorf("associate_id = %v, want %v", data["associate_id"], associateID)
	}
	if data["task_type"] != "PICK" {
		t.Errorf("task_type = %v, want PICK", data["task_type"])
	}
	if data["efficiency_pct"] != 91.2 {
		t.Errorf("efficiency_pct = %v, want 91.2", data["efficiency_pct"])
	}
	if data["actual_seconds"] != float64(41) {
		t.Errorf("actual_seconds = %v, want 41", data["actual_seconds"])
	}
}

func createTopicAndWaitForLeaderIntegration(t *testing.T, ctx context.Context, broker, topic string) {
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
