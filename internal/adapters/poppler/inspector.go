package poppler

import (
	"context"
	"strings"

	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

const (
	FormFeed  = "\f"
	PageBreak = "\n\n"
)

// Inspector implementa domain.TextExtractor sobre el motor poppler.
type Inspector struct{}

func New() *Inspector {
	return &Inspector{}
}

func (i *Inspector) Extract(ctx context.Context, src domain.DocumentSource) (*domain.ExtractionResult, error) {
	rawText, elapsedMs, _, err := ExtractWithTiming(ctx, src)
	if err != nil {
		return nil, err
	}

	// Contar páginas: cada \f = salto de página en pdftotext
	pages := strings.Count(rawText, FormFeed)
	if pages == 0 && len(rawText) > 0 {
		pages = 1 // fallback: PDF de una página sin \f final
	}

	// Normalizar saltos de página a párrafos
	text := strings.TrimSpace(strings.ReplaceAll(rawText, FormFeed, PageBreak))

	var warnings []domain.Warning
	if text == "" {
		// Texto vacío → reason "empty" (puerta 2 devuelve 422)
		return nil, domain.ErrEmptyDocument
	}

	// Warning: texto inusualmente corto para la cantidad de páginas
	if pages > 0 && len(text) < 50 {
		warnings = append(warnings, domain.Warning{
			Code:    "low_text_content",
			Message: "extracted text unusually short for page count",
		})
	}

	return domain.NewExtractionResult(text, pages).
		WithExtractionTimeMs(elapsedMs).
		WithWarnings(warnings), nil
}

var _ domain.TextExtractor = (*Inspector)(nil)