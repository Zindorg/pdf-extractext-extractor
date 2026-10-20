package api

import "github.com/Zindorg/pdf-extractext-extractor/internal/domain"

// ExtractRequest es el cuerpo de POST /extract (multipart/form-data).
// El orquestador ya validó mime_type contra "application/pdf" (puerta 1).
type ExtractRequest struct {
	Document []byte // campo "document" (binary)
	MimeType string // campo "mime_type"
}

// ExtractResponse es el cuerpo 200 exacto al contrato workers.md §Extractor.
type ExtractResponse struct {
	ExtractedText    string          `json:"extracted_text"`
	ExtractionTimeMs int64           `json:"extraction_time_ms"`
	Warnings         []domain.Warning `json:"warnings"`
}

// ExtractErrorResponse es el cuerpo 422 exacto al contrato.
type ExtractErrorResponse struct {
	Reason  string `json:"reason"`  // enum: empty|encrypted_pdf|password_protected|corrupt|unsupported_features
	Message string `json:"message"`
}