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

// Inspector implements domain.TextExtractor using the poppler engine[cite: 2].
type Inspector struct{}

func New() *Inspector {
	return &Inspector{}
}

func (i *Inspector) Extract(ctx context.Context, src domain.DocumentSource) (*domain.ExtractionResult, error) {
	rawText, elapsedMs, err := ExtractWithTiming(ctx, src)
	if err != nil {
		return nil, err
	}

	pages := strings.Count(rawText, FormFeed)
	if pages == 0 && len(rawText) > 0 {
		pages = 1
	}

	text := strings.TrimSpace(strings.ReplaceAll(rawText, FormFeed, PageBreak))
	if text == "" {
		return nil, domain.ErrEmptyDocument
	}

	var warnings []domain.Warning
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
