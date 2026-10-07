package application

import (
	"context"

	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

// ExtractionService es el caso de uso de una extracción: admite la petición si
// hay hueco, delega la extracción en el motor y le devuelve el hueco.
type ExtractionService struct {
	extractor domain.TextExtractor
	pool      *WorkerPool
}

// NewExtractionService enlaza el caso de uso con su motor y su admisión.
func NewExtractionService(extractor domain.TextExtractor, pool *WorkerPool) *ExtractionService {
	return &ExtractionService{extractor: extractor, pool: pool}
}

// Process extrae el texto de src si en este momento hay hueco libre.
//
// La admisión no espera: sin hueco devuelve domain.ErrResourceExhausted. El
// hueco tomado se devuelve siempre, se termine bien o mal.
func (s *ExtractionService) Process(ctx context.Context, src domain.DocumentSource) (*domain.ExtractionResult, error) {
	if !s.pool.Acquire() {
		return nil, domain.ErrResourceExhausted
	}
	defer s.pool.Release()

	return s.extractor.Extract(ctx, src)
}
