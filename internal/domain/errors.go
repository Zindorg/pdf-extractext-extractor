package domain

import "errors"

// Motivos de fallo del dominio. Cada uno se traduce a un status HTTP y a un
// `type` estable en internal/api/problem.go (RFC 9457).
var (
	ErrNoTextLayer       = errors.New("document has no extractable text layer")
	ErrCorruptDocument   = errors.New("document is corrupt")
	ErrEncryptedDocument = errors.New("document is encrypted")
	ErrDocumentTooLarge  = errors.New("document exceeds the maximum allowed size")
	ErrExtractionTimeout = errors.New("extraction deadline exceeded")
)
