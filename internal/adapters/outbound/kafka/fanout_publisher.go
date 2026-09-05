package kafka

import (
	"context"

	"github.com/claudioed/labor-performance/internal/application/ports"
	"github.com/claudioed/labor-performance/internal/domain/shared"
)

// FanOutPublisher forwards each domain event to several EventPublishers
// in turn. It is how the OLTP composition root publishes one event stream
// to both the existing log publisher and the new analytics topic without
// any use case knowing there is more than one publisher: FanOutPublisher
// itself satisfies ports.EventPublisher, so the application layer is
// untouched by the analytics data product existing at all (ADR-0007).
//
// Fan-out is fail-fast in publisher order: the first error stops the
// fan-out and is returned, so an analytics-publish failure is never
// silently swallowed behind an earlier publisher's success.
type FanOutPublisher struct {
	publishers []ports.EventPublisher
}

// NewFanOutPublisher builds a FanOutPublisher over publishers, in the
// order they should receive each event.
func NewFanOutPublisher(publishers ...ports.EventPublisher) *FanOutPublisher {
	return &FanOutPublisher{publishers: publishers}
}

// Publish forwards events to every configured publisher, returning the
// first error encountered.
func (f *FanOutPublisher) Publish(ctx context.Context, events ...shared.DomainEvent) error {
	for _, p := range f.publishers {
		if err := p.Publish(ctx, events...); err != nil {
			return err
		}
	}
	return nil
}

// Compile-time assertion that FanOutPublisher satisfies the outbound
// event-publishing port.
var _ ports.EventPublisher = (*FanOutPublisher)(nil)
