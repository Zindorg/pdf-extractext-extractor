package poppler

import (
	"context"

	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

// Inspector implementa domain.TextExtractor sobre el motor poppler.
type Inspector struct{}

// New construye el Inspector.
func New() *Inspector {
	return &Inspector{}
}

// Extract todavía no extrae nada: devuelve nil, nil a propósito, para que la
// suite esté en rojo.
func (i *Inspector) Extract(ctx context.Context, src domain.DocumentSource) (*domain.ExtractionResult, error) {
	return nil, nil
}

var _ domain.TextExtractor = (*Inspector)(nil)
