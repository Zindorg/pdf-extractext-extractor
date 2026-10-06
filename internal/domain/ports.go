package domain

import (
	"context"
	"io"
)

// DocumentSource es un documento legible más de una vez, siempre desde el
// principio y sin tocar el disco. La implementación real es un descriptor
// anónimo en RAM (internal/adapters/ram): el mismo descriptor alimenta a
// pdfinfo y a pdftotext, que son dos lecturas del mismo documento.
type DocumentSource interface {
	Open() (io.ReadCloser, error)
}

// TextExtractor extrae el texto de un documento legible.
type TextExtractor interface {
	Extract(ctx context.Context, src DocumentSource) (*ExtractionResult, error)
}
