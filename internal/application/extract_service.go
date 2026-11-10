package application

import (
	"context"
	"time"

	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

// ExtractionService coordinates slot admission, phase timeouts, and text extraction[cite: 2].
type ExtractionService struct {
	extractor        domain.TextExtractor
	admission        AdmissionController
	queueWaitTimeout time.Duration
	extractTimeout   time.Duration
}

// NewExtractionService wires the use case with its extractor and admission controller[cite: 1, 2].
func NewExtractionService(
	extractor domain.TextExtractor,
	admission AdmissionController,
	queueWaitTimeout, extractTimeout time.Duration,
) *ExtractionService {
	return &ExtractionService{
		extractor:        extractor,
		admission:        admission,
		queueWaitTimeout: queueWaitTimeout,
		extractTimeout:   extractTimeout,
	}
}

// Process acquires a slot within queueWaitTimeout and executes extraction within extractTimeout[cite: 2].
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
