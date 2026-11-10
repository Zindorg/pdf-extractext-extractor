package domain

import (
	"context"
	"io"
)

// DocumentSource represents a readable document that can be opened multiple times from the start.
type DocumentSource interface {
	Open() (io.ReadCloser, error)
}

// StoredDocument is a DocumentSource bound to the lifecycle of the current request.
type StoredDocument interface {
	DocumentSource
	io.Closer
}

// DocumentStore persists an incoming stream into an ephemeral StoredDocument.
type DocumentStore interface {
	Save(r io.Reader) (StoredDocument, error)
}

// TextExtractor extracts plain text from a DocumentSource.
type TextExtractor interface {
	Extract(ctx context.Context, src DocumentSource) (*ExtractionResult, error)
}
