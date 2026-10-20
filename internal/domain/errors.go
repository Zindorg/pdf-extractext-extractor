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
	ErrResourceExhausted = errors.New("no workers available")
	ErrQueueTimeout      = errors.New("queue wait deadline exceeded")

	// NUEVOS — mapeo 1:1 al enum workers.md §Extractor
	ErrEmptyDocument       = errors.New("extracted text is empty")           // → reason: "empty"
	ErrPasswordProtected   = errors.New("document requires password")       // → reason: "password_protected"
	ErrUnsupportedFeatures = errors.New("document has unsupported features") // → reason: "unsupported_features"
	ErrExtractorFailure    = errors.New("extractor internal failure")       // → reason desconocido → 503
)
