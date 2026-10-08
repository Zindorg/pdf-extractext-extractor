package application

import (
	"context"
	"time"

	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

// ExtractionService es el caso de uso de una extracción: admite la petición si
// hay hueco, delega la extracción en el motor y le devuelve el hueco.
type ExtractionService struct {
	extractor      domain.TextExtractor
	pool           *WorkerPool
	queueWaitTimeout time.Duration
	extractTimeout   time.Duration
}

// NewExtractionService enlaza el caso de uso con su motor y su admisión.
func NewExtractionService(extractor domain.TextExtractor, pool *WorkerPool, queueWaitTimeout, extractTimeout time.Duration) *ExtractionService {
	return &ExtractionService{
		extractor:        extractor,
		pool:             pool,
		queueWaitTimeout: queueWaitTimeout,
		extractTimeout:   extractTimeout,
	}
}

// Process extrae el texto de src con un presupuesto de tiempo total dividido en
// dos fases: espera en cola (queueWaitTimeout) y extracción (extractTimeout).
//
// Si no hay hueco libre, la petición espera en cola hasta queueWaitTimeout.
// Una vez conseguido el hueco, la extracción tiene extractTimeout de margen.
// Ambos plazos se aplican sobre el mismo deadline del contexto original.
func (s *ExtractionService) Process(ctx context.Context, src domain.DocumentSource) (*domain.ExtractionResult, error) {
	queueCtx, cancelQueue := context.WithTimeout(ctx, s.queueWaitTimeout)
	defer cancelQueue()

	if err := s.pool.Acquire(queueCtx); err != nil {
		return nil, err
	}
	defer s.pool.Release()

	extractCtx, cancelExtract := context.WithTimeout(ctx, s.extractTimeout)
	defer cancelExtract()

	return s.extractor.Extract(extractCtx, src)
}
