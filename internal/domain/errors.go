package domain

import "errors"

// Domain failure reasons mapped to HTTP status codes in internal/api[cite: 2].
var (
	ErrCorruptDocument     = errors.New("document is corrupt")
	ErrEncryptedDocument   = errors.New("document is encrypted")
	ErrExtractionTimeout   = errors.New("extraction deadline exceeded")
	ErrResourceExhausted   = errors.New("no workers available")
	ErrQueueTimeout        = errors.New("queue wait deadline exceeded")
	ErrEmptyDocument       = errors.New("extracted text is empty")
	ErrPasswordProtected   = errors.New("document requires password")
	ErrUnsupportedFeatures = errors.New("document has unsupported features")
	ErrExtractorFailure    = errors.New("extractor internal failure")
)
