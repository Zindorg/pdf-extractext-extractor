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

// El enrutador sirve las rutas de §5: GET /health y POST /extract.
func TestNewRouter_RegistraLasRutasDeLaSeccion5(t *testing.T) {
	stub := &stubService{result: domain.NewExtractionResult("Texto extraído", 2).
		WithExtractionTimeMs(42).
		WithWarnings(nil)}
	mux := NewRouter(NewExtractionHandler(stub, 20*1024*1024))

	recibida := httptest.NewRecorder()
	mux.ServeHTTP(recibida, httptest.NewRequest(http.MethodGet, "/health", nil))
	if recibida.Code != http.StatusOK {
		t.Errorf("GET /health devolvió %d; se esperaba %d", recibida.Code, http.StatusOK)
	}
	var salud struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(recibida.Body).Decode(&salud); err != nil {
		t.Fatalf("el cuerpo de /health no es JSON válido: %v", err)
	}
	if salud.Status != "ok" {
		t.Errorf("status = %q; se esperaba ok", salud.Status)
	}

	// POST /extract requiere multipart/form-data
	recibida = httptest.NewRecorder()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, _ := w.CreateFormFile("document", "test.pdf")
	part.Write([]byte("%PDF-1.4"))
	w.WriteField("mime_type", "application/pdf")
	w.Close()

	solicitud := httptest.NewRequest(http.MethodPost, "/extract", &buf)
	solicitud.Header.Set("Content-Type", w.FormDataContentType())
	mux.ServeHTTP(recibida, solicitud)

	if recibida.Code != http.StatusOK {
		t.Errorf("POST /extract devolvió %d; se esperaba %d", recibida.Code, http.StatusOK)
	}
	var cuerpo ExtractResponse
	if err := json.NewDecoder(recibida.Body).Decode(&cuerpo); err != nil {
		t.Fatalf("el cuerpo de /extract no es JSON válido: %v", err)
	}
	if cuerpo.ExtractedText != "Texto extraído" {
		t.Errorf("extracted_text = %q; se esperaba el texto del caso de uso", cuerpo.ExtractedText)
	}
	if cuerpo.ExtractionTimeMs != 42 {
		t.Errorf("extraction_time_ms = %d; se esperaba 42", cuerpo.ExtractionTimeMs)
	}
	if stub.llamadas != 1 {
		t.Errorf("el caso de uso se invocó %d veces; se esperaba 1", stub.llamadas)
	}
}

// Los patrones con método del ServeMux de Go 1.22 rechazan un método no
// registrado en una ruta conocida.
func TestNewRouter_RechazaMetodoNoRegistrado(t *testing.T) {
	mux := NewRouter(NewExtractionHandler(&stubService{}, 20*1024*1024))

	recibida := httptest.NewRecorder()
	mux.ServeHTTP(recibida, httptest.NewRequest(http.MethodPost, "/health", nil))
	if recibida.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /health devolvió %d; se esperaba %d", recibida.Code, http.StatusMethodNotAllowed)
	}
}