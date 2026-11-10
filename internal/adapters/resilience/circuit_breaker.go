package resilience

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

// State represents the state of the Circuit Breaker.
type State int

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

// ResilientExtractor decorates a domain.TextExtractor with Retry and Circuit Breaker patterns.
type ResilientExtractor struct {
	next             domain.TextExtractor
	mu               sync.Mutex
	state            State
	failures         int
	failureThreshold int
	resetTimeout     time.Duration
	lastFailureTime  time.Time
	maxRetries       int
	retryBackoff     time.Duration
}

// NewResilientExtractor wires a TextExtractor with circuit breaking and retry policies.
func NewResilientExtractor(
	next domain.TextExtractor,
	failureThreshold int,
	resetTimeout time.Duration,
	maxRetries int,
	retryBackoff time.Duration,
) *ResilientExtractor {
	if failureThreshold <= 0 {
		failureThreshold = 5
	}
	if resetTimeout <= 0 {
		resetTimeout = 10 * time.Second
	}
	return &ResilientExtractor{
		next:             next,
		state:            StateClosed,
		failureThreshold: failureThreshold,
		resetTimeout:     resetTimeout,
		maxRetries:       maxRetries,
		retryBackoff:     retryBackoff,
	}
}

// Extract executes the underlying extractor with retries on transient engine failures
// and short-circuits with ErrExtractorFailure when the circuit is Open.
func (r *ResilientExtractor) Extract(ctx context.Context, src domain.DocumentSource) (*domain.ExtractionResult, error) {
	if !r.allowRequest() {
		return nil, domain.ErrExtractorFailure
	}

	var lastErr error
	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(r.retryBackoff * time.Duration(attempt)):
			case <-ctx.Done():
				return nil, domain.ErrExtractionTimeout
			}
		}

		res, err := r.next.Extract(ctx, src)
		if err == nil {
			r.recordSuccess()
			return res, nil
		}

		lastErr = err

		// Si el contexto expiró, abortamos sin contabilizar como caída de motor
		if errors.Is(err, domain.ErrExtractionTimeout) || ctx.Err() != nil {
			return nil, domain.ErrExtractionTimeout
		}

		// Errores de dominio del documento (422: vacío, corrupto, password, etc.)
		// no son fallos del motor Poppler: no se reintentan ni abren el Circuit Breaker.
		if !errors.Is(err, domain.ErrExtractorFailure) {
			r.recordSuccess()
			return nil, err
		}
	}

	r.recordFailure()
	return nil, lastErr
}

func (r *ResilientExtractor) allowRequest() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	switch r.state {
	case StateClosed:
		return true
	case StateOpen:
		if time.Since(r.lastFailureTime) >= r.resetTimeout {
			r.state = StateHalfOpen
			return true
		}
		return false
	case StateHalfOpen:
		return false
	default:
		return true
	}
}

func (r *ResilientExtractor) recordSuccess() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failures = 0
	r.state = StateClosed
}

func (r *ResilientExtractor) recordFailure() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failures++
	r.lastFailureTime = time.Now()
	if r.failures >= r.failureThreshold || r.state == StateHalfOpen {
		r.state = StateOpen
	}
}

var _ domain.TextExtractor = (*ResilientExtractor)(nil)
