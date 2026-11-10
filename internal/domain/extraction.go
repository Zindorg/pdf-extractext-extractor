package domain

import (
	"crypto/sha256"
	"encoding/hex"
)

// Warning represents a non-fatal issue encountered during extraction[cite: 2].
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ExtractionResult holds the extracted text, page count, SHA-256 checksum, timing, and warnings[cite: 2].
type ExtractionResult struct {
	Text             string
	PageCount        int
	Checksum         string
	ExtractionTimeMs int64
	Warnings         []Warning
}

// NewExtractionResult builds an ExtractionResult and computes the SHA-256 checksum of text[cite: 2].
func NewExtractionResult(text string, pageCount int) *ExtractionResult {
	sum := sha256.Sum256([]byte(text))
	return &ExtractionResult{
		Text:      text,
		PageCount: pageCount,
		Checksum:  hex.EncodeToString(sum[:]),
	}
}

// WithExtractionTimeMs returns a new ExtractionResult copy with the elapsed extraction time set[cite: 1, 2].
func (r *ExtractionResult) WithExtractionTimeMs(ms int64) *ExtractionResult {
	cp := *r
	cp.ExtractionTimeMs = ms
	return &cp
}

// WithWarnings returns a new ExtractionResult copy with non-fatal warnings attached[cite: 1, 2].
func (r *ExtractionResult) WithWarnings(w []Warning) *ExtractionResult {
	cp := *r
	if w != nil {
		cp.Warnings = append([]Warning(nil), w...)
	}
	return &cp
}
