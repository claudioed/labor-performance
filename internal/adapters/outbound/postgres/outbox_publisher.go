package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	outboundkafka "github.com/claudioed/labor-performance/internal/adapters/outbound/kafka"
	"github.com/claudioed/labor-performance/internal/application/ports"
	"github.com/claudioed/labor-performance/internal/domain/shared"
)

// outboxHeader is the JSON shape one Kafka header takes inside
// outbox_events.headers: [{"key":"traceparent","value":"00-..."}].
type outboxHeader struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// OutboxPublisher implements ports.EventPublisher by writing each event's
// Kafka wire form into outbox_events instead of the broker (ADR 0010).
// When called inside UnitOfWork.Execute the insert joins the use case's
// transaction, so the aggregate change and its event commit together or
// not at all. OutboxRelay later drains the table onto Kafka.
//
// Encoding happens here, inside the transaction, through the very same
// Encoder the direct publish path uses, so the outbox and the direct
// path can never disagree on wire format. One outbox row is written per
// (event x encoder-produced message); an encoder that has no mapping for
// an event (the analytics contract skips unknown types) simply produces
// no row for it.
type OutboxPublisher struct {
	pool     *pgxpool.Pool
	encoders []outboundkafka.Encoder
}

// NewOutboxPublisher constructs an OutboxPublisher over pool that fans
// each event through every encoder given, in order.
func NewOutboxPublisher(pool *pgxpool.Pool, encoders ...outboundkafka.Encoder) *OutboxPublisher {
	return &OutboxPublisher{pool: pool, encoders: encoders}
}

// Publish stores every event's encoded message(s) in the outbox. It never
// touches Kafka.
func (p *OutboxPublisher) Publish(ctx context.Context, events ...shared.DomainEvent) error {
	if len(events) == 0 {
		return nil
	}
	q := querierFrom(ctx, p.pool)
	for _, enc := range p.encoders {
		msgs, err := enc.Encode(ctx, events...)
		if err != nil {
			return fmt.Errorf("postgres: encode outbox event: %w", err)
		}
		for _, m := range msgs {
			headers, err := marshalHeaders(m)
			if err != nil {
				return err
			}
			if _, err := q.Exec(ctx, `
				INSERT INTO outbox_events (topic, event_type, key, value, headers)
				VALUES ($1, $2, $3, $4, $5)
			`, m.Topic, m.EventType, m.Key, m.Value, headers); err != nil {
				return fmt.Errorf("postgres: enqueue outbox event %s for %s: %w", m.EventType, m.Topic, err)
			}
		}
	}
	return nil
}

func marshalHeaders(m outboundkafka.Encoded) ([]byte, error) {
	hs := make([]outboxHeader, 0, len(m.Headers))
	for _, h := range m.Headers {
		hs = append(hs, outboxHeader{Key: h.Key, Value: string(h.Value)})
	}
	b, err := json.Marshal(hs)
	if err != nil {
		return nil, fmt.Errorf("postgres: marshal outbox headers for %s: %w", m.EventType, err)
	}
	return b, nil
}

// Compile-time assertion that OutboxPublisher satisfies the outbound
// event-publishing port.
var _ ports.EventPublisher = (*OutboxPublisher)(nil)
