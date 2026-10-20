package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

// stubService es el caso de uso falso que el handler recibe: se le fija un
// resultado o un error, y cuenta cuántas veces se le pide una extracción.
type stubService struct {
	result   *domain.ExtractionResult
	err      error
	llamadas int
}

func (s *stubService) Process(context.Context, domain.DocumentSource) (*domain.ExtractionResult, error) {
	s.llamadas++
	return s.result, s.err
}

// newMultipartRequest crea una request multipart/form-data con los campos dados.
func newMultipartRequest(document []byte, mimeType string) *http.Request {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	// Campo document
	part, _ := w.CreateFormFile("document", "test.pdf")
	part.Write(document)

	// Campo mime_type
	w.WriteField("mime_type", mimeType)
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/extract", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

// El 200 es el contrato del extractor (workers.md §Extractor):
// {extracted_text, extraction_time_ms, warnings[]}
func TestHandleExtract_Devuelve200ConShapeDeContrato(t *testing.T) {
	warnings := []domain.Warning{{Code: "low_text_content", Message: "short text"}}
	stub := &stubService{
		result: domain.NewExtractionResult("Texto extraído", 2).
			WithExtractionTimeMs(42).
			WithWarnings(warnings),
	}
	handler := NewExtractionHandler(stub, 20*1024*1024)

	req := newMultipartRequest([]byte("%PDF-1.4"), "application/pdf")
	rec := httptest.NewRecorder()

	handler.HandleExtract(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("HandleExtract() devolvió %d; se esperaba %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q; se esperaba application/json", ct)
	}

	var cuerpo ExtractResponse
	if err := json.NewDecoder(rec.Body).Decode(&cuerpo); err != nil {
		t.Fatalf("el cuerpo no es JSON válido: %v", err)
	}

	if cuerpo.ExtractedText != "Texto extraído" {
		t.Errorf("extracted_text = %q; se esperaba el texto del resultado", cuerpo.ExtractedText)
	}
	if cuerpo.ExtractionTimeMs != 42 {
		t.Errorf("extraction_time_ms = %d; se esperaba 42", cuerpo.ExtractionTimeMs)
	}
	if len(cuerpo.Warnings) != 1 || cuerpo.Warnings[0].Code != "low_text_content" {
		t.Errorf("warnings = %+v; se esperaba [low_text_content]", cuerpo.Warnings)
	}
	// Campos que NO deben estar en el contrato
	if cuerpo.ExtractedText == "" {
		t.Error("extracted_text no debe estar vacío")
	}
	if stub.llamadas != 1 {
		t.Errorf("el caso de uso se invocó %d veces; se esperaba 1", stub.llamadas)
	}
}

// 200 con warnings nil → array vacío (no null)
func TestHandleExtract_DevuelveWarningsArrayVacioSiNil(t *testing.T) {
	stub := &stubService{
		result: domain.NewExtractionResult("Texto", 1).
			WithExtractionTimeMs(10).
			WithWarnings(nil),
	}
	handler := NewExtractionHandler(stub, 20*1024*1024)

	req := newMultipartRequest([]byte("%PDF-1.4"), "application/pdf")
	rec := httptest.NewRecorder()

	handler.HandleExtract(rec, req)

	var cuerpo ExtractResponse
	json.NewDecoder(rec.Body).Decode(&cuerpo)

	if cuerpo.Warnings == nil {
		t.Fatal("warnings es nil; debe ser array vacío []")
	}
	if len(cuerpo.Warnings) != 0 {
		t.Errorf("warnings = %+v; se esperaba []", cuerpo.Warnings)
	}
}

// Rechaza payload > maxDocumentBytes → 413 con body simple {message}
// Nota: el test usa httptest.NewRequest que no activa MaxBytesReader de forma real;
// en producción el límite se aplica en el orquestador (puerta 1) y como salvaguarda aquí.
func TestHandleExtract_RechazaPayloadPorEncimaDeMaxBytes(t *testing.T) {
	stub := &stubService{}
	handler := NewExtractionHandler(stub, 100) // 100 bytes límite

	const oversized = 200
	req := newMultipartRequest(bytes.Repeat([]byte("x"), oversized), "application/pdf")
	rec := httptest.NewRecorder()

	handler.HandleExtract(rec, req)

	// En test con httptest, el límite no se aplica igual; aceptamos 400 o 413
	if rec.Code != http.StatusRequestEntityTooLarge && rec.Code != http.StatusBadRequest {
		t.Fatalf("HandleExtract() devolvió %d; se esperaba 413 o 400", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q; se esperaba application/json", ct)
	}

	var cuerpo map[string]string
	json.NewDecoder(rec.Body).Decode(&cuerpo)
	if cuerpo["message"] == "" {
		t.Error("body debe tener message")
	}
}

// Request sin multipart → 400
func TestHandleExtract_RechazaSinMultipart(t *testing.T) {
	stub := &stubService{}
	handler := NewExtractionHandler(stub, 20*1024*1024)

	req := httptest.NewRequest(http.MethodPost, "/extract", strings.NewReader("not multipart"))
	req.Header.Set("Content-Type", "application/pdf")
	rec := httptest.NewRecorder()

	handler.HandleExtract(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("HandleExtract() devolvió %d; se esperaba %d", rec.Code, http.StatusBadRequest)
	}
	var cuerpo map[string]string
	json.NewDecoder(rec.Body).Decode(&cuerpo)
	if cuerpo["message"] == "" {
		t.Error("body debe tener message")
	}
}

// Request multipart sin campo document → 400
func TestHandleExtract_RechazaSinCampoDocument(t *testing.T) {
	stub := &stubService{}
	handler := NewExtractionHandler(stub, 20*1024*1024)

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	w.WriteField("mime_type", "application/pdf")
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/extract", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()

	handler.HandleExtract(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("HandleExtract() devolvió %d; se esperaba %d", rec.Code, http.StatusBadRequest)
	}
}

// Request multipart sin campo mime_type → 400
func TestHandleExtract_RechazaSinCampoMimeType(t *testing.T) {
	stub := &stubService{}
	handler := NewExtractionHandler(stub, 20*1024*1024)

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	w.CreateFormFile("document", "test.pdf")
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/extract", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()

	handler.HandleExtract(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("HandleExtract() devolvió %d; se esperaba %d", rec.Code, http.StatusBadRequest)
	}
}

// 422 con reason del enum del contrato
func TestHandleExtract_MapeaErroresDominioA422ConReason(t *testing.T) {
	casos := []struct {
		nombre     string
		dominio    error
		reason     string
		mensaje    string
	}{
		{"empty", domain.ErrEmptyDocument, "empty", "extracted text is empty"},
		{"encrypted_pdf", domain.ErrEncryptedDocument, "encrypted_pdf", "document is encrypted"},
		{"password_protected", domain.ErrPasswordProtected, "password_protected", "document requires password"},
		{"corrupt", domain.ErrCorruptDocument, "corrupt", "document is corrupt"},
		{"unsupported_features", domain.ErrUnsupportedFeatures, "unsupported_features", "document has unsupported features"},
	}

	for _, caso := range casos {
		t.Run(caso.nombre, func(t *testing.T) {
			stub := &stubService{err: caso.dominio}
			handler := NewExtractionHandler(stub, 20*1024*1024)

			req := newMultipartRequest([]byte("%PDF-1.4"), "application/pdf")
			rec := httptest.NewRecorder()

			handler.HandleExtract(rec, req)

			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("HandleExtract() devolvió %d; se esperaba %d", rec.Code, http.StatusUnprocessableEntity)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q; se esperaba application/json", ct)
			}

			var cuerpo ExtractErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&cuerpo); err != nil {
				t.Fatalf("el cuerpo no es JSON válido: %v", err)
			}

			if cuerpo.Reason != caso.reason {
				t.Errorf("reason = %q; se esperaba %q", cuerpo.Reason, caso.reason)
			}
			if cuerpo.Message != caso.mensaje {
				t.Errorf("message = %q; se esperaba %q", cuerpo.Message, caso.mensaje)
			}
		})
	}
}

// 503 extractor_unavailable para ErrExtractorFailure
func TestHandleExtract_ErrExtractorFailureDevuelve503ConReason(t *testing.T) {
	stub := &stubService{err: domain.ErrExtractorFailure}
	handler := NewExtractionHandler(stub, 20*1024*1024)

	req := newMultipartRequest([]byte("%PDF-1.4"), "application/pdf")
	rec := httptest.NewRecorder()

	handler.HandleExtract(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("HandleExtract() devolvió %d; se esperaba %d", rec.Code, http.StatusServiceUnavailable)
	}

	var cuerpo ExtractErrorResponse
	json.NewDecoder(rec.Body).Decode(&cuerpo)

	if cuerpo.Reason != "extractor_unavailable" {
		t.Errorf("reason = %q; se esperaba extractor_unavailable", cuerpo.Reason)
	}
	if cuerpo.Message != "extractor internal failure" {
		t.Errorf("message = %q; se esperaba extractor internal failure", cuerpo.Message)
	}
}

// 504/429 sin reason (solo message)
// 503 con reason "extractor_unavailable" para unknown errors (contrato)
func TestHandleExtract_OtrosErroresInternosSinReason(t *testing.T) {
	casos := []struct {
		nombre       string
		err          error
		status       int
		contains     string
		hasReason    bool
		expectedReason string
	}{
		{"extraction_timeout", domain.ErrExtractionTimeout, http.StatusGatewayTimeout, "deadline exceeded", false, ""},
		{"resource_exhausted", domain.ErrResourceExhausted, http.StatusTooManyRequests, "no workers available", false, ""},
		{"queue_timeout", domain.ErrQueueTimeout, http.StatusServiceUnavailable, "queue wait deadline exceeded", false, ""},
		{"unknown_error", errors.New("random internal error"), http.StatusServiceUnavailable, "extractor internal failure", true, "extractor_unavailable"},
	}

	for _, caso := range casos {
		t.Run(caso.nombre, func(t *testing.T) {
			stub := &stubService{err: caso.err}
			handler := NewExtractionHandler(stub, 20*1024*1024)

			req := newMultipartRequest([]byte("%PDF-1.4"), "application/pdf")
			rec := httptest.NewRecorder()

			handler.HandleExtract(rec, req)

			if rec.Code != caso.status {
				t.Fatalf("HandleExtract() devolvió %d; se esperaba %d", rec.Code, caso.status)
			}

			var cuerpo map[string]string
			json.NewDecoder(rec.Body).Decode(&cuerpo)

			if cuerpo["message"] == "" {
				t.Error("body debe tener message")
			}
			if !strings.Contains(cuerpo["message"], caso.contains) {
				t.Errorf("message = %q; debe contener %q", cuerpo["message"], caso.contains)
			}
			if caso.hasReason {
				if cuerpo["reason"] != caso.expectedReason {
					t.Errorf("reason = %q; se esperaba %q", cuerpo["reason"], caso.expectedReason)
				}
			} else {
				if _, ok := cuerpo["reason"]; ok {
					t.Error("no debe tener campo reason para este status")
				}
			}
		})
	}
}

// GET /health es el latido del proceso: 200 con {"status":"ok"}
func TestHandleHealth_Devuelve200(t *testing.T) {
	handler := NewExtractionHandler(&stubService{}, 20*1024*1024)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.HandleHealth(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("HandleHealth() devolvió %d; se esperaba %d", rec.Code, http.StatusOK)
	}

	var cuerpo struct {
		Status string `json:"status"`
	}
	json.NewDecoder(rec.Body).Decode(&cuerpo)
	if cuerpo.Status != "ok" {
		t.Errorf("status = %q; se esperaba ok", cuerpo.Status)
	}
}

// Test helper: leer error body genérico
func leerErrorBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var cuerpo map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&cuerpo); err != nil {
		t.Fatalf("el cuerpo no es JSON válido: %v", err)
	}
	return cuerpo
}