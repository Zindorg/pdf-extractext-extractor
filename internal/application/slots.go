package application

import (
	"context"

	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

// AdmissionControl manages concurrent extraction slots with a bounded queue.
// It uses a single buffered channel (capacity + maxQueue) to model both
// available slots and waiting positions, eliminating the need for atomic counters.
type AdmissionControl struct {
	slots    chan struct{}
	capacity int
	maxQueue int
}

// NewAdmissionControl creates a controller that allows up to 'capacity'
// concurrent extractions, with a waiting queue of up to 'maxQueue' requests.
func NewAdmissionControl(capacity, maxQueue int) *AdmissionControl {
	return &AdmissionControl{
		slots:    make(chan struct{}, capacity+maxQueue),
		capacity: capacity,
		maxQueue: maxQueue,
	}
}

// AcquireSlot attempts to reserve an extraction slot.
// Fast path: if slots are available (len < capacity), returns immediately.
// Slow path: if at capacity but queue has space, waits for a slot or context timeout.
// Returns ErrResourceExhausted if queue is full, ErrQueueTimeout if context expires.
func (a *AdmissionControl) AcquireSlot(ctx context.Context) error {
	// Fast path: slot available
	select {
	case a.slots <- struct{}{}:
		return nil
	default:
	}

	// If no queue space, reject immediately
	if a.maxQueue == 0 {
		return domain.ErrResourceExhausted
	}

	// Slow path: at capacity, wait for release or timeout
	select {
	case a.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return domain.ErrQueueTimeout
	}
}

// ReleaseSlot releases a previously acquired slot.
// Safe to call without a prior AcquireSlot (no-op).
func (a *AdmissionControl) ReleaseSlot() {
	select {
	case <-a.slots:
	default:
	}
}
