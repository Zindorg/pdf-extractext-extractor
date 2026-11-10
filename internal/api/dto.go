package api

import "github.com/Zindorg/pdf-extractext-extractor/internal/domain"

// ExtractResponse represents the 200 OK JSON payload for POST /extract[cite: 1, 2].
type ExtractResponse struct {
	ExtractedText    string           `json:"extracted_text"`
	ExtractionTimeMs int64            `json:"extraction_time_ms"`
	PageCount        int              `json:"page_count"`
	Checksum         string           `json:"checksum"`
	Warnings         []domain.Warning `json:"warnings"`
}

// ExtractErrorResponse represents the 422/503 error payload with a contract reason[cite: 2].
type ExtractErrorResponse struct {
	Reason  string `json:"reason"`
	Message string `json:"message"`
}
