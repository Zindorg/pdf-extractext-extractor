package application

import (
	"context"

	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

// AdmissionController abstracts concurrency and queue management (DIP).
type AdmissionController interface {
	AcquireSlot(ctx context.Context) error
	ReleaseSlot()
	Saturated() bool
}

// AdmissionControl bounds active workers (running) and queued requests (waiting) separately.
type AdmissionControl struct {
	running chan struct{}
	waiting chan struct{}
}

// NewAdmissionControl creates a controller with 'capacity' active slots and 'maxQueue' waiting slots[cite: 1, 2].
func NewAdmissionControl(capacity, maxQueue int) *AdmissionControl {
	return &AdmissionControl{
		running: make(chan struct{}, capacity),
		waiting: make(chan struct{}, maxQueue),
	}
}

// AcquireSlot reserves an execution slot or waits in the bounded queue[cite: 1, 2].
func (a *AdmissionControl) AcquireSlot(ctx context.Context) error {
	select {
	case a.running <- struct{}{}:
		return nil
	default:
	}

	select {
	case a.waiting <- struct{}{}:
	default:
		return domain.ErrResourceExhausted
	}
	defer func() { <-a.waiting }()

	select {
	case a.running <- struct{}{}:
		return nil
	case <-ctx.Done():
		return domain.ErrQueueTimeout
	}
}

// ReleaseSlot frees an active execution slot[cite: 1, 2].
func (a *AdmissionControl) ReleaseSlot() {
	select {
	case <-a.running:
	default:
	}
}

// Saturated reports whether both active slots and the waiting queue are full.
func (a *AdmissionControl) Saturated() bool {
	return len(a.running) == cap(a.running) && len(a.waiting) == cap(a.waiting)
}
