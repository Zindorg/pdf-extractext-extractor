package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Zindorg/pdf-extractext-extractor/internal/adapters/ram"
	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

// ExtractionService is what the API layer needs from the use case.
// The real *application.ExtractionService implements it implicitly.
type ExtractionService interface {
	Process(ctx context.Context, src domain.DocumentSource) (*domain.ExtractionResult, error)
}

// ExtractionHandler receives requests from the router and translates them
// into use case calls.
type ExtractionHandler struct {
	service          ExtractionService
	maxDocumentBytes int64
}

// NewExtractionHandler wires the API layer to its use case.
func NewExtractionHandler(service ExtractionService, maxDocumentBytes int64) *ExtractionHandler {
	return &ExtractionHandler{service: service, maxDocumentBytes: maxDocumentBytes}
}

// HandleExtract handles POST /extract: parses multipart/form-data with
// document (binary) and mime_type (string), delegates to the use case,
// and translates the result or error to the workers.md contract.
func (h *ExtractionHandler) HandleExtract(w http.ResponseWriter, r *http.Request) {
	// Limit body size
	r.Body = http.MaxBytesReader(w, r.Body, h.maxDocumentBytes)

	// Parse multipart/form-data
	mr, err := r.MultipartReader()
	if err != nil {
		// Check if it's a MaxBytesError (body too large)
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeContractError(w, http.StatusRequestEntityTooLarge, "", "document exceeds maximum size")
			return
		}
		writeContractError(w, http.StatusBadRequest, "", "invalid multipart request")
		return
	}

	var document []byte
	var mimeType string

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeContractError(w, http.StatusBadRequest, "", "failed to read multipart")
			return
		}

		switch part.FormName() {
		case "document":
			document, err = io.ReadAll(part)
			if err != nil {
				writeContractError(w, http.StatusBadRequest, "", "failed to read document")
				return
			}
		case "mime_type":
			b, _ := io.ReadAll(part)
			mimeType = strings.TrimSpace(string(b))
		}
		part.Close()
	}

	if len(document) == 0 {
		writeContractError(w, http.StatusBadRequest, "", "document field is required")
		return
	}
	if mimeType == "" {
		writeContractError(w, http.StatusBadRequest, "", "mime_type field is required")
		return
	}

	// Delegate to use case
	// Create memfd from document bytes (zero-disk, zero-heap)
	file, err := ram.NewBuffer(strings.NewReader(string(document)))
	if err != nil {
		writeContractError(w, http.StatusInternalServerError, "", "failed to create document buffer")
		return
	}
	defer file.Close()

	result, err := h.service.Process(r.Context(), ram.Source(file))
	if err != nil {
		writeContractErrorFromDomain(w, err)
		return
	}

	writeContractSuccess(w, result)
}

// HandleHealth handles GET /health: the process heartbeat.
func (h *ExtractionHandler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(healthResponse{Status: "ok"})
}

// writeContractSuccess emits 200 with exact contract shape.
func writeContractSuccess(w http.ResponseWriter, result *domain.ExtractionResult) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	warnings := result.Warnings
	if warnings == nil {
		warnings = []domain.Warning{}
	}

	json.NewEncoder(w).Encode(ExtractResponse{
		ExtractedText:    result.Text,
		ExtractionTimeMs: result.ExtractionTimeMs,
		Warnings:         warnings,
	})
}

// writeContractErrorFromDomain maps domain errors to contract responses.
func writeContractErrorFromDomain(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrEmptyDocument):
		writeContractError(w, http.StatusUnprocessableEntity, "empty", "extracted text is empty")
	case errors.Is(err, domain.ErrEncryptedDocument):
		writeContractError(w, http.StatusUnprocessableEntity, "encrypted_pdf", "document is encrypted")
	case errors.Is(err, domain.ErrPasswordProtected):
		writeContractError(w, http.StatusUnprocessableEntity, "password_protected", "document requires password")
	case errors.Is(err, domain.ErrCorruptDocument):
		writeContractError(w, http.StatusUnprocessableEntity, "corrupt", "document is corrupt")
	case errors.Is(err, domain.ErrUnsupportedFeatures):
		writeContractError(w, http.StatusUnprocessableEntity, "unsupported_features", "document has unsupported features")

	// Extractor internal failure → 503 with reason (contract: unknown reason → 503 extractor_unavailable)
	case errors.Is(err, domain.ErrExtractorFailure):
		writeContractError(w, http.StatusServiceUnavailable, "extractor_unavailable", "extractor internal failure")

	// Other internal errors → 503/504/429 without contract reason
	case errors.Is(err, domain.ErrExtractionTimeout):
		writeContractError(w, http.StatusGatewayTimeout, "", "extraction deadline exceeded")
	case errors.Is(err, domain.ErrResourceExhausted):
		writeContractError(w, http.StatusTooManyRequests, "", "no workers available")
	case errors.Is(err, domain.ErrQueueTimeout):
		writeContractError(w, http.StatusServiceUnavailable, "", "queue wait deadline exceeded")

	// Unknown → 503 extractor_unavailable (contract: "Un reason fuera del enum no se traduce en 422")
	default:
		writeContractError(w, http.StatusServiceUnavailable, "extractor_unavailable", "extractor internal failure")
	}
}

// writeContractError emits error response.
// For 422: {reason, message} per contract (enum reasons).
// For 503 extractor_unavailable: {reason, message} per contract.
// For other statuses: {message} only (no reason field).
func writeContractError(w http.ResponseWriter, status int, reason, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if reason != "" && (status == http.StatusUnprocessableEntity || status == http.StatusServiceUnavailable) {
		json.NewEncoder(w).Encode(ExtractErrorResponse{Reason: reason, Message: message})
	} else {
		json.NewEncoder(w).Encode(map[string]string{"message": message})
	}
}

// healthResponse is the 200 body for /health.
type healthResponse struct {
	Status string `json:"status"`
}