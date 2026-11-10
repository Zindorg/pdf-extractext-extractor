package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

func TestNewRouter_RegistersRoutes(t *testing.T) {
	stub := &stubService{
		result: domain.NewExtractionResult("Texto extraído", 2).
			WithExtractionTimeMs(42).
			WithWarnings(nil),
	}
	readyState := true
	mux := NewRouter(
		NewExtractionHandler(stub, memoryStore{}, 20*1024*1024),
		func() bool { return readyState },
	)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("GET /health returned %d; want %d", rec.Code, http.StatusOK)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("GET /ready returned %d; want %d", rec.Code, http.StatusOK)
	}

	readyState = false
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("GET /ready when not ready returned %d; want %d", rec.Code, http.StatusServiceUnavailable)
	}

	rec = httptest.NewRecorder()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, _ := w.CreateFormFile("document", "test.pdf")
	_, _ = part.Write([]byte("%PDF-1.4"))
	_ = w.WriteField("mime_type", "application/pdf")
	_ = w.Close()

	req := httptest.NewRequest(http.MethodPost, "/extract", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("POST /extract returned %d; want %d", rec.Code, http.StatusOK)
	}
	var body ExtractResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	if body.ExtractedText != "Texto extraído" {
		t.Errorf("extracted_text = %q; want Texto extraído", body.ExtractedText)
	}
}

func TestNewRouter_RejectsUnregisteredMethod(t *testing.T) {
	mux := NewRouter(NewExtractionHandler(&stubService{}, memoryStore{}, 20*1024*1024), nil)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/health", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /health returned %d; want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}
