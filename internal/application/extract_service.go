package application

import (
	"context"
	"time"

	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

// ExtractionService is the use case for extraction: admits the request if a
// slot is available, delegates extraction to the engine, and releases the slot.
type ExtractionService struct {
	extractor       domain.TextExtractor
	admission       *AdmissionControl
	queueWaitTimeout time.Duration
	extractTimeout   time.Duration
}

// NewExtractionService wires the use case with its extractor and admission control.
func NewExtractionService(extractor domain.TextExtractor, admission *AdmissionControl, queueWaitTimeout, extractTimeout time.Duration) *ExtractionService {
	return &ExtractionService{
		extractor:        extractor,
		admission:        admission,
		queueWaitTimeout: queueWaitTimeout,
		extractTimeout:   extractTimeout,
	}
}

// Process extracts text from src with a total time budget split into two
// phases: queue wait (queueWaitTimeout) and extraction (extractTimeout).
//
// If no slot is available, the request waits in queue until queueWaitTimeout.
// Once a slot is acquired, extraction has extractTimeout to complete.
// Both deadlines derive from the original context.
func (s *ExtractionService) Process(ctx context.Context, src domain.DocumentSource) (*domain.ExtractionResult, error) {
	queueCtx, cancelQueue := context.WithTimeout(ctx, s.queueWaitTimeout)
	defer cancelQueue()

	if err := s.admission.AcquireSlot(queueCtx); err != nil {
		return nil, err
	}
	defer s.admission.ReleaseSlot()

	extractCtx, cancelExtract := context.WithTimeout(ctx, s.extractTimeout)
	defer cancelExtract()

	return s.extractor.Extract(extractCtx, src)
}
