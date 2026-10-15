package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

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
	service           ExtractionService
	maxDocumentBytes  int64
}

// NewExtractionHandler wires the API layer to its use case.
func NewExtractionHandler(service ExtractionService, maxDocumentBytes int64) *ExtractionHandler {
	return &ExtractionHandler{service: service, maxDocumentBytes: maxDocumentBytes}
}

// HandleExtract handles POST /extract: limits the body, dumps it to a memfd
// (§8: zero disk and zero Go heap), delegates to the use case, and translates
// the result or error to the contract. The memfd dies with this call.
func (h *ExtractionHandler) HandleExtract(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	r.Body = http.MaxBytesReader(w, r.Body, h.maxDocumentBytes)

	file, err := ram.NewBuffer(r.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeProblem(w, http.StatusRequestEntityTooLarge, "Payload Too Large", "document-too-large", err.Error(), r.URL.Path)
			return
		}
		writeError(w, err, r.URL.Path)
		return
	}
	defer file.Close()

	result, err := h.service.Process(r.Context(), ram.Source(file))
	if err != nil {
		writeError(w, err, r.URL.Path)
		return
	}

	writeResult(w, r, started, result)
}

// HandleHealth handles GET /health: the process heartbeat (§5).
func (h *ExtractionHandler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(healthResponse{Status: "ok"})
}

// extractionResponse is the 200 body (§5), with document_id echoed back.
type extractionResponse struct {
	DocumentID       string `json:"document_id"`
	ExtractedText    string `json:"extracted_text"`
	PageCount        int    `json:"page_count"`
	Checksum         string `json:"checksum"`
	ProcessingTimeMS int64  `json:"processing_time_ms"`
}

// writeResult responds with 200, echoing the request's X-Document-ID and
// the measurements from §5.
func writeResult(w http.ResponseWriter, r *http.Request, started time.Time, result *domain.ExtractionResult) {
	if id := r.Header.Get("X-Document-ID"); id != "" {
		w.Header().Set("X-Document-ID", id)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(extractionResponse{
		DocumentID:       r.Header.Get("X-Document-ID"),
		ExtractedText:    result.Text,
		PageCount:        result.PageCount,
		Checksum:         result.Checksum,
		ProcessingTimeMS: time.Since(started).Milliseconds(),
	})
}

// healthResponse is the 200 body for /health (§5).
type healthResponse struct {
	Status string `json:"status"`
}

// errorMapping describes a domain failure reason: its HTTP status, stable
// `type`, and an extra header if it carries one.
type errorMapping struct {
	err        error
	status     int
	title      string
	suffix     string
	retryAfter string
}

// errorMappings is the equivalence table between domain and RFC 9457.
var errorMappings = []errorMapping{
	{err: domain.ErrDocumentTooLarge, status: http.StatusRequestEntityTooLarge, title: "Payload Too Large", suffix: "document-too-large"},
	{err: domain.ErrEncryptedDocument, status: http.StatusUnprocessableEntity, title: "Unprocessable Entity", suffix: "encrypted-document"},
	{err: domain.ErrCorruptDocument, status: http.StatusUnprocessableEntity, title: "Unprocessable Entity", suffix: "corrupt-document"},
	{err: domain.ErrResourceExhausted, status: http.StatusTooManyRequests, title: "Too Many Requests", suffix: "resource-exhausted", retryAfter: "1"},
	{err: domain.ErrQueueTimeout, status: http.StatusServiceUnavailable, title: "Service Unavailable", suffix: "queue-timeout", retryAfter: "5"},
	{err: domain.ErrExtractionTimeout, status: http.StatusGatewayTimeout, title: "Gateway Timeout", suffix: "extraction-timeout"},
}

// writeError responds to a use case failure as ProblemDetails. Unknown reasons
// are hidden behind /problems/internal: the detail only matters to the operator.
func writeError(w http.ResponseWriter, err error, instance string) {
	for _, mapping := range errorMappings {
		if errors.Is(err, mapping.err) {
			if mapping.retryAfter != "" {
				w.Header().Set("Retry-After", mapping.retryAfter)
			}
			writeProblem(w, mapping.status, mapping.title, mapping.suffix, err.Error(), instance)
			return
		}
	}

	writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "internal", err.Error(), instance)
}

// writeProblem writes a ProblemDetails (RFC 9457).
func writeProblem(w http.ResponseWriter, status int, title, suffix, detail, instance string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ProblemDetails{
		Type:     "/problems/" + suffix,
		Title:    title,
		Status:   status,
		Detail:   detail,
		Instance: instance,
	})
}
