package memory

import (
	"context"
	"sync"
)

// ProcessedEventRepo is an in-memory implementation of
// ports.ProcessedEvents, the idempotency gate keyed on Kafka event_id.
type ProcessedEventRepo struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

// NewProcessedEventRepo constructs an empty ProcessedEventRepo.
func NewProcessedEventRepo() *ProcessedEventRepo {
	return &ProcessedEventRepo{seen: make(map[string]struct{})}
}

func (r *ProcessedEventRepo) MarkProcessed(_ context.Context, eventId string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.seen[eventId]; ok {
		return false, nil
	}
	r.seen[eventId] = struct{}{}
	return true, nil
}
