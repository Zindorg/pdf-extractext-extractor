package domain

import (
	"context"
	"io"
)

// DocumentSource is a document readable multiple times, always from the
// beginning and without touching disk. The real implementation is an
// anonymous RAM descriptor (internal/adapters/ram): Open() returns a
// duplicated descriptor that allows concurrent reads without interference.
type DocumentSource interface {
	Open() (io.ReadCloser, error)
}

// TextExtractor extracts text from a readable document.
type TextExtractor interface {
	Extract(ctx context.Context, src DocumentSource) (*ExtractionResult, error)
}
