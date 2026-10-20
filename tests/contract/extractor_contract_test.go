package contract

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestContract_ValidPDF_Returns200WithContractShape verifica el happy path:
// PDF válido → 200 con shape exacto del contrato workers.md §Extractor.
func TestContract_ValidPDF_Returns200WithContractShape(t *testing.T) {
	handler := newTestHandler(t)
	pdf := readFixture(t, "valido.pdf")
	req := newMultipartRequest(t, pdf, "application/pdf")
	rec := makeRequest(t, handler, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	var body struct {
		ExtractedText    string `json:"extracted_text"`
		ExtractionTimeMs int64  `json:"extraction_time_ms"`
		Warnings         []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"warnings"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}

	if body.ExtractedText == "" {
		t.Error("extracted_text must not be empty")
	}
	if body.ExtractionTimeMs <= 0 {
		t.Errorf("extraction_time_ms = %d; must be > 0", body.ExtractionTimeMs)
	}
	if body.Warnings == nil {
		t.Error("warnings must be an array (not null)")
	}
	// Content-Type debe ser application/json
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q; expected application/json", ct)
	}
}

// TestContract_EmptyPDF_Returns422Empty verifica PDF sin capa de texto → 422 reason=empty.
func TestContract_EmptyPDF_Returns422Empty(t *testing.T) {
	handler := newTestHandler(t)
	pdf := readFixture(t, "sin_texto.pdf")
	req := newMultipartRequest(t, pdf, "application/pdf")
	rec := makeRequest(t, handler, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 Unprocessable Entity, got %d", rec.Code)
	}

	var body struct {
		Reason  string `json:"reason"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}

	if body.Reason != "empty" {
		t.Errorf("reason = %q; expected 'empty'", body.Reason)
	}
	if body.Message == "" {
		t.Error("message must not be empty")
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q; expected application/json", ct)
	}
}

// TestContract_EncryptedPDF_Returns422EncryptedPDF verifica PDF cifrado → 422 reason=encrypted_pdf.
func TestContract_EncryptedPDF_Returns422EncryptedPDF(t *testing.T) {
	handler := newTestHandler(t)
	pdf := readFixture(t, "cifrado.pdf")
	req := newMultipartRequest(t, pdf, "application/pdf")
	rec := makeRequest(t, handler, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 Unprocessable Entity, got %d", rec.Code)
	}

	var body struct {
		Reason  string `json:"reason"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}

	if body.Reason != "encrypted_pdf" {
		t.Errorf("reason = %q; expected 'encrypted_pdf'", body.Reason)
	}
	if body.Message == "" {
		t.Error("message must not be empty")
	}
}

// TestContract_CorruptPDF_Returns422Corrupt verifica PDF corrupto → 422 reason=corrupt.
func TestContract_CorruptPDF_Returns422Corrupt(t *testing.T) {
	handler := newTestHandler(t)
	pdf := readFixture(t, "corrupto.pdf")
	req := newMultipartRequest(t, pdf, "application/pdf")
	rec := makeRequest(t, handler, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 Unprocessable Entity, got %d", rec.Code)
	}

	var body struct {
		Reason  string `json:"reason"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}

	if body.Reason != "corrupt" {
		t.Errorf("reason = %q; expected 'corrupt'", body.Reason)
	}
	if body.Message == "" {
		t.Error("message must not be empty")
	}
}

// TestContract_MissingDocumentField_Returns400 verifica request sin campo document → 400.
func TestContract_MissingDocumentField_Returns400(t *testing.T) {
	handler := newTestHandler(t)

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	w.WriteField("mime_type", "application/pdf")
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/extract", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := makeRequest(t, handler, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
	}

	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	if body["message"] == "" {
		t.Error("body must have message")
	}
}

// TestContract_MissingMimeTypeField_Returns400 verifica request sin campo mime_type → 400.
func TestContract_MissingMimeTypeField_Returns400(t *testing.T) {
	handler := newTestHandler(t)

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	w.CreateFormFile("document", "test.pdf")
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/extract", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := makeRequest(t, handler, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
	}

	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	if body["message"] == "" {
		t.Error("body must have message")
	}
}

// TestContract_NonMultipartRequest_Returns400 verifica request sin multipart → 400.
func TestContract_NonMultipartRequest_Returns400(t *testing.T) {
	handler := newTestHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/extract", bytes.NewReader([]byte("not multipart")))
	req.Header.Set("Content-Type", "application/pdf")
	rec := makeRequest(t, handler, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
	}
}