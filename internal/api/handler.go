package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

// ExtractionService defines the use case boundary consumed by the HTTP transport layer[cite: 2].
type ExtractionService interface {
	Process(ctx context.Context, src domain.DocumentSource) (*domain.ExtractionResult, error)
}

// ExtractionHandler translates HTTP multipart requests into use case invocations[cite: 1, 2].
type ExtractionHandler struct {
	service          ExtractionService
	store            domain.DocumentStore
	maxDocumentBytes int64
}

// NewExtractionHandler wires the HTTP handler to the use case and DocumentStore port.
func NewExtractionHandler(service ExtractionService, store domain.DocumentStore, maxDocumentBytes int64) *ExtractionHandler {
	return &ExtractionHandler{
		service:          service,
		store:            store,
		maxDocumentBytes: maxDocumentBytes,
	}
}

// HandleExtract handles POST /extract[cite: 1, 2].
func (h *ExtractionHandler) HandleExtract(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, h.maxDocumentBytes)

	doc, status, msg := h.parseMultipart(r)
	if status != 0 {
		writeContractError(w, status, "", msg)
		return
	}
	defer doc.Close()

	result, err := h.service.Process(r.Context(), doc)
	if err != nil {
		writeContractErrorFromDomain(w, err)
		return
	}

	writeContractSuccess(w, result)
}

// parseMultipart extracts and validates the multipart fields, streaming the document into DocumentStore.
func (h *ExtractionHandler) parseMultipart(r *http.Request) (domain.StoredDocument, int, string) {
	mr, err := r.MultipartReader()
	if err != nil {
		if isMaxBytesError(err) {
			return nil, http.StatusRequestEntityTooLarge, "document exceeds maximum size"
		}
		return nil, http.StatusBadRequest, "invalid multipart request"
	}

	var doc domain.StoredDocument
	var hasDoc bool
	var mimeType string

	cleanup := func() {
		if doc != nil {
			_ = doc.Close()
		}
	}

	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			cleanup()
			if isMaxBytesError(err) {
				return nil, http.StatusRequestEntityTooLarge, "document exceeds maximum size"
			}
			return nil, http.StatusBadRequest, "failed to read multipart"
		}

		switch part.FormName() {
		case "document":
			if hasDoc {
				_ = part.Close()
				cleanup()
				return nil, http.StatusBadRequest, "duplicate document field"
			}
			counting := &countingReader{r: part}
			saved, saveErr := h.store.Save(counting)
			_ = part.Close()
			if saveErr != nil {
				cleanup()
				if isMaxBytesError(saveErr) {
					return nil, http.StatusRequestEntityTooLarge, "document exceeds maximum size"
				}
				return nil, http.StatusBadRequest, "failed to read document"
			}
			if counting.n == 0 {
				_ = saved.Close()
				cleanup()
				return nil, http.StatusBadRequest, "document field is required"
			}
			doc = saved
			hasDoc = true

		case "mime_type":
			b, readErr := io.ReadAll(part)
			_ = part.Close()
			if readErr != nil {
				cleanup()
				if isMaxBytesError(readErr) {
					return nil, http.StatusRequestEntityTooLarge, "document exceeds maximum size"
				}
				return nil, http.StatusBadRequest, "failed to read mime_type"
			}
			mimeType = strings.TrimSpace(string(b))

		default:
			_ = part.Close()
		}
	}

	if !hasDoc {
		cleanup()
		return nil, http.StatusBadRequest, "document field is required"
	}
	if mimeType == "" {
		cleanup()
		return nil, http.StatusBadRequest, "mime_type field is required"
	}

	return doc, 0, ""
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func isMaxBytesError(err error) bool {
	var mbe *http.MaxBytesError
	return errors.As(err, &mbe)
}

// HandleHealth handles GET /health[cite: 2].
func (h *ExtractionHandler) HandleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(healthResponse{Status: "ok"})
}

func writeContractSuccess(w http.ResponseWriter, result *domain.ExtractionResult) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	warnings := result.Warnings
	if warnings == nil {
		warnings = []domain.Warning{}
	}

	_ = json.NewEncoder(w).Encode(ExtractResponse{
		ExtractedText:    result.Text,
		ExtractionTimeMs: result.ExtractionTimeMs,
		PageCount:        result.PageCount,
		Checksum:         result.Checksum,
		Warnings:         warnings,
	})
}

type errorEntry struct {
	target  error
	status  int
	reason  string
	message string
}

// errorMappings maps domain sentinel errors to HTTP responses in a table (OCP)[cite: 1].
var errorMappings = []errorEntry{
	{domain.ErrEmptyDocument, http.StatusUnprocessableEntity, "empty", "extracted text is empty"},
	{domain.ErrEncryptedDocument, http.StatusUnprocessableEntity, "encrypted_pdf", "document is encrypted"},
	{domain.ErrPasswordProtected, http.StatusUnprocessableEntity, "password_protected", "document requires password"},
	{domain.ErrCorruptDocument, http.StatusUnprocessableEntity, "corrupt", "document is corrupt"},
	{domain.ErrUnsupportedFeatures, http.StatusUnprocessableEntity, "unsupported_features", "document has unsupported features"},
	{domain.ErrExtractorFailure, http.StatusServiceUnavailable, "extractor_unavailable", "extractor internal failure"},
	{domain.ErrExtractionTimeout, http.StatusGatewayTimeout, "", "extraction deadline exceeded"},
	{domain.ErrResourceExhausted, http.StatusTooManyRequests, "", "no workers available"},
	{domain.ErrQueueTimeout, http.StatusServiceUnavailable, "", "queue wait deadline exceeded"},
}

func writeContractErrorFromDomain(w http.ResponseWriter, err error) {
	for _, entry := range errorMappings {
		if errors.Is(err, entry.target) {
			writeContractError(w, entry.status, entry.reason, entry.message)
			return
		}
	}
	writeContractError(w, http.StatusServiceUnavailable, "extractor_unavailable", "extractor internal failure")
}

func writeContractError(w http.ResponseWriter, status int, reason, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if reason != "" && (status == http.StatusUnprocessableEntity || status == http.StatusServiceUnavailable) {
		_ = json.NewEncoder(w).Encode(ExtractErrorResponse{Reason: reason, Message: message})
	} else {
		_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
	}
}

type healthResponse struct {
	Status string `json:"status"`
}
