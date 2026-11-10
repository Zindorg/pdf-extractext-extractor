package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func TestRateLimit_BlocksBurstAndSetsRetryAfterHeader(t *testing.T) {
	stub := &stubService{
		result: domain.NewExtractionResult("ok", 1).WithExtractionTimeMs(10),
	}
	mux := NewRouter(NewExtractionHandler(stub, memoryStore{}, 20*1024*1024), nil)
	limited := RateLimit(1, 1, mux)

	// 1ra petición consume el único token del bucket -> 200 OK
	rec1 := httptest.NewRecorder()
	limited.ServeHTTP(rec1, newMultipartRequest([]byte("%PDF-1.4"), "application/pdf"))
	if rec1.Code != http.StatusOK {
		t.Fatalf("first request returned %d; want 200", rec1.Code)
	}

	// 2da petición inmediata -> 429 Too Many Requests con Retry-After
	rec2 := httptest.NewRecorder()
	limited.ServeHTTP(rec2, newMultipartRequest([]byte("%PDF-1.4"), "application/pdf"))
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request returned %d; want 429", rec2.Code)
	}
	if rec2.Header().Get("Retry-After") == "" {
		t.Error("expected Retry-After header on 429 response")
	}
}

func TestIdempotency_ReplaysCachedResponseWithoutCallingServiceTwice(t *testing.T) {
	stub := &stubService{
		result: domain.NewExtractionResult("idempotente", 1).WithExtractionTimeMs(15),
	}
	mux := NewRouter(NewExtractionHandler(stub, memoryStore{}, 20*1024*1024), nil)
	handler := Idempotency(time.Minute, mux)

	req1 := newMultipartRequest([]byte("%PDF-1.4"), "application/pdf")
	req1.Header.Set("Idempotency-Key", "saga-tx-123")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("first request returned %d; want 200", rec1.Code)
	}

	// Segunda petición con la misma Idempotency-Key
	req2 := newMultipartRequest([]byte("%PDF-1.4"), "application/pdf")
	req2.Header.Set("Idempotency-Key", "saga-tx-123")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("second request returned %d; want 200", rec2.Code)
	}
	if rec2.Header().Get("X-Idempotent-Replay") != "true" {
		t.Error("expected X-Idempotent-Replay: true header on second request")
	}
	if stub.calls != 1 {
		t.Errorf("service called %d times; want 1", stub.calls)
	}
}
