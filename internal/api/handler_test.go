package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

type stubService struct {
	result *domain.ExtractionResult
	err    error
	calls  int
}

func (s *stubService) Process(context.Context, domain.DocumentSource) (*domain.ExtractionResult, error) {
	s.calls++
	return s.result, s.err
}

type memoryStore struct{}

type memDoc struct{ data []byte }

func (m memDoc) Open() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(m.data)), nil }
func (m memDoc) Close() error                 { return nil }

func (memoryStore) Save(r io.Reader) (domain.StoredDocument, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return memDoc{data: b}, nil
}

func buildMultipartBody(document []byte, mimeType string) (*bytes.Buffer, string) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, _ := w.CreateFormFile("document", "test.pdf")
	_, _ = part.Write(document)
	_ = w.WriteField("mime_type", mimeType)
	_ = w.Close()
	return &buf, w.FormDataContentType()
}

func newMultipartRequest(document []byte, mimeType string) *http.Request {
	buf, ct := buildMultipartBody(document, mimeType)
	req := httptest.NewRequest(http.MethodPost, "/extract", buf)
	req.Header.Set("Content-Type", ct)
	return req
}

func TestHandleExtract_Returns200WithContractShape(t *testing.T) {
	warnings := []domain.Warning{{Code: "low_text_content", Message: "short text"}}
	stub := &stubService{
		result: domain.NewExtractionResult("Texto extraído", 2).
			WithExtractionTimeMs(42).
			WithWarnings(warnings),
	}
	handler := NewExtractionHandler(stub, memoryStore{}, 20*1024*1024)

	req := newMultipartRequest([]byte("%PDF-1.4"), "application/pdf")
	rec := httptest.NewRecorder()

	handler.HandleExtract(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("HandleExtract() returned %d; want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q; want application/json", ct)
	}

	var body ExtractResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}

	if body.ExtractedText != "Texto extraído" {
		t.Errorf("extracted_text = %q; want Texto extraído", body.ExtractedText)
	}
	if body.ExtractionTimeMs != 42 {
		t.Errorf("extraction_time_ms = %d; want 42", body.ExtractionTimeMs)
	}
	if body.PageCount != 2 {
		t.Errorf("page_count = %d; want 2", body.PageCount)
	}
	if len(body.Checksum) != 64 {
		t.Errorf("checksum = %q; want 64-char hex SHA-256", body.Checksum)
	}
	if len(body.Warnings) != 1 || body.Warnings[0].Code != "low_text_content" {
		t.Errorf("warnings = %+v; want [low_text_content]", body.Warnings)
	}
	if stub.calls != 1 {
		t.Errorf("use case called %d times; want 1", stub.calls)
	}
}

func TestHandleExtract_ReturnsEmptyWarningsArrayWhenNil(t *testing.T) {
	stub := &stubService{
		result: domain.NewExtractionResult("Texto", 1).
			WithExtractionTimeMs(10).
			WithWarnings(nil),
	}
	handler := NewExtractionHandler(stub, memoryStore{}, 20*1024*1024)

	req := newMultipartRequest([]byte("%PDF-1.4"), "application/pdf")
	rec := httptest.NewRecorder()

	handler.HandleExtract(rec, req)

	var body ExtractResponse
	_ = json.NewDecoder(rec.Body).Decode(&body)

	if body.Warnings == nil {
		t.Fatal("warnings is nil; must be empty slice []")
	}
	if len(body.Warnings) != 0 {
		t.Errorf("warnings = %+v; want []", body.Warnings)
	}
}

func TestHandleExtract_RejectsOversizedPayloadWith413(t *testing.T) {
	stub := &stubService{}
	handler := NewExtractionHandler(stub, memoryStore{}, 100)
	srv := httptest.NewServer(NewRouter(handler, nil))
	defer srv.Close()

	buf, ct := buildMultipartBody(bytes.Repeat([]byte("x"), 300), "application/pdf")
	resp, err := http.Post(srv.URL+"/extract", ct, buf)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("HandleExtract() returned %d; want exactly %d (413)", resp.StatusCode, http.StatusRequestEntityTooLarge)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q; want application/json", ct)
	}

	var body map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["message"] != "document exceeds maximum size" {
		t.Errorf("message = %q; want 'document exceeds maximum size'", body["message"])
	}
}

func TestHandleExtract_RejectsNonMultipart(t *testing.T) {
	stub := &stubService{}
	handler := NewExtractionHandler(stub, memoryStore{}, 20*1024*1024)

	req := httptest.NewRequest(http.MethodPost, "/extract", strings.NewReader("not multipart"))
	req.Header.Set("Content-Type", "application/pdf")
	rec := httptest.NewRecorder()

	handler.HandleExtract(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("HandleExtract() returned %d; want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleExtract_RejectsMissingDocumentField(t *testing.T) {
	stub := &stubService{}
	handler := NewExtractionHandler(stub, memoryStore{}, 20*1024*1024)

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("mime_type", "application/pdf")
	_ = w.Close()

	req := httptest.NewRequest(http.MethodPost, "/extract", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()

	handler.HandleExtract(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("HandleExtract() returned %d; want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleExtract_RejectsMissingMimeTypeField(t *testing.T) {
	stub := &stubService{}
	handler := NewExtractionHandler(stub, memoryStore{}, 20*1024*1024)

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, _ := w.CreateFormFile("document", "test.pdf")
	_, _ = part.Write([]byte("%PDF-1.4"))
	_ = w.Close()

	req := httptest.NewRequest(http.MethodPost, "/extract", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()

	handler.HandleExtract(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("HandleExtract() returned %d; want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleExtract_MapsDomainErrorsTo422WithReason(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		reason  string
		message string
	}{
		{"empty", domain.ErrEmptyDocument, "empty", "extracted text is empty"},
		{"encrypted_pdf", domain.ErrEncryptedDocument, "encrypted_pdf", "document is encrypted"},
		{"password_protected", domain.ErrPasswordProtected, "password_protected", "document requires password"},
		{"corrupt", domain.ErrCorruptDocument, "corrupt", "document is corrupt"},
		{"unsupported_features", domain.ErrUnsupportedFeatures, "unsupported_features", "document has unsupported features"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubService{err: tc.err}
			handler := NewExtractionHandler(stub, memoryStore{}, 20*1024*1024)

			req := newMultipartRequest([]byte("%PDF-1.4"), "application/pdf")
			rec := httptest.NewRecorder()

			handler.HandleExtract(rec, req)

			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("HandleExtract() returned %d; want %d", rec.Code, http.StatusUnprocessableEntity)
			}

			var body ExtractErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("invalid JSON body: %v", err)
			}
			if body.Reason != tc.reason {
				t.Errorf("reason = %q; want %q", body.Reason, tc.reason)
			}
			if body.Message != tc.message {
				t.Errorf("message = %q; want %q", body.Message, tc.message)
			}
		})
	}
}

func TestHandleExtract_ErrExtractorFailureReturns503WithReason(t *testing.T) {
	stub := &stubService{err: domain.ErrExtractorFailure}
	handler := NewExtractionHandler(stub, memoryStore{}, 20*1024*1024)

	req := newMultipartRequest([]byte("%PDF-1.4"), "application/pdf")
	rec := httptest.NewRecorder()

	handler.HandleExtract(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("HandleExtract() returned %d; want %d", rec.Code, http.StatusServiceUnavailable)
	}

	var body ExtractErrorResponse
	_ = json.NewDecoder(rec.Body).Decode(&body)

	if body.Reason != "extractor_unavailable" {
		t.Errorf("reason = %q; want extractor_unavailable", body.Reason)
	}
}

func TestHandleExtract_OtherInternalErrors(t *testing.T) {
	cases := []struct {
		name           string
		err            error
		status         int
		contains       string
		hasReason      bool
		expectedReason string
	}{
		{"extraction_timeout", domain.ErrExtractionTimeout, http.StatusGatewayTimeout, "deadline exceeded", false, ""},
		{"resource_exhausted", domain.ErrResourceExhausted, http.StatusTooManyRequests, "no workers available", false, ""},
		{"queue_timeout", domain.ErrQueueTimeout, http.StatusServiceUnavailable, "queue wait deadline exceeded", false, ""},
		{"unknown_error", errors.New("random internal error"), http.StatusServiceUnavailable, "extractor internal failure", true, "extractor_unavailable"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubService{err: tc.err}
			handler := NewExtractionHandler(stub, memoryStore{}, 20*1024*1024)

			req := newMultipartRequest([]byte("%PDF-1.4"), "application/pdf")
			rec := httptest.NewRecorder()

			handler.HandleExtract(rec, req)

			if rec.Code != tc.status {
				t.Fatalf("HandleExtract() returned %d; want %d", rec.Code, tc.status)
			}

			var body map[string]string
			_ = json.NewDecoder(rec.Body).Decode(&body)

			if !strings.Contains(body["message"], tc.contains) {
				t.Errorf("message = %q; want substring %q", body["message"], tc.contains)
			}
			if tc.hasReason {
				if body["reason"] != tc.expectedReason {
					t.Errorf("reason = %q; want %q", body["reason"], tc.expectedReason)
				}
			} else if _, ok := body["reason"]; ok {
				t.Error("unexpected reason field for status")
			}
		})
	}
}

func TestHandleHealth_Returns200(t *testing.T) {
	handler := NewExtractionHandler(&stubService{}, memoryStore{}, 20*1024*1024)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.HandleHealth(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("HandleHealth() returned %d; want %d", rec.Code, http.StatusOK)
	}
}
