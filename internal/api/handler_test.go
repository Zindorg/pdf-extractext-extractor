package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
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

// leerProblema decodifica el cuerpo como ProblemDetails, para que los asserts
// hablen del contrato y no de strings crudos.
func leerProblema(t *testing.T, cuerpo io.Reader) ProblemDetails {
	t.Helper()

	var problema ProblemDetails
	if err := json.NewDecoder(cuerpo).Decode(&problema); err != nil {
		t.Fatalf("el cuerpo no es un ProblemDetails válido: %v", err)
	}
	return problema
}

// lazyReader produce n bytes de ceros sobre la marcha: el test de la cota de
// tamaño no monta 21 MiB en el heap (F.I.R.S.T.: los tests van rápido).
type lazyReader struct {
	remaining int64
}

func (l *lazyReader) Read(buf []byte) (int, error) {
	n := int64(len(buf))
	if n > l.remaining {
		n = l.remaining
	}
	if n <= 0 {
		return 0, io.EOF
	}
	for i := int64(0); i < n; i++ {
		buf[i] = 0
	}
	l.remaining -= n

	return int(n), nil
}

func newLazyReader(restantes int64) io.Reader {
	return &lazyReader{remaining: restantes}
}

// El 200 es el contrato del servicio: los cinco campos de §5, con document_id
// en eco de la cabecera y el tiempo medido por el propio handler.
func TestHandleExtract_Devuelve200ConElResultadoCompleto(t *testing.T) {
	stub := &stubService{result: domain.NewExtractionResult("Texto extraído", 2)}
	handler := NewExtractionHandler(stub)

	solicitud := httptest.NewRequest(http.MethodPost, "/extract", strings.NewReader("%PDF-1.4"))
	solicitud.Header.Set("Content-Type", "application/pdf")
	solicitud.Header.Set("X-Document-ID", "d-1234")
	recibida := httptest.NewRecorder()

	handler.HandleExtract(recibida, solicitud)

	if recibida.Code != http.StatusOK {
		t.Fatalf("HandleExtract() devolvió %d; se esperaba %d", recibida.Code, http.StatusOK)
	}
	if ct := recibida.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q; se esperaba application/json", ct)
	}

	var cuerpo struct {
		DocumentID     string `json:"document_id"`
		ExtractedText  string `json:"extracted_text"`
		PageCount      int    `json:"page_count"`
		Checksum       string `json:"checksum"`
		ProcessingTime *int64 `json:"processing_time_ms"`
	}
	if err := json.NewDecoder(recibida.Body).Decode(&cuerpo); err != nil {
		t.Fatalf("el cuerpo no es JSON válido: %v", err)
	}
	if cuerpo.ExtractedText != "Texto extraído" {
		t.Errorf("extracted_text = %q; se esperaba el texto del resultado", cuerpo.ExtractedText)
	}
	if cuerpo.PageCount != 2 {
		t.Errorf("page_count = %d; se esperaba 2", cuerpo.PageCount)
	}
	if cuerpo.Checksum != stub.result.Checksum {
		t.Errorf("checksum = %q; se esperaba %q", cuerpo.Checksum, stub.result.Checksum)
	}
	if cuerpo.DocumentID != "d-1234" {
		t.Errorf("document_id = %q; se esperaba el eco de la cabecera X-Document-ID", cuerpo.DocumentID)
	}
	if cuerpo.ProcessingTime == nil {
		t.Error("processing_time_ms está ausente; §5 lo exige en el 200")
	} else if *cuerpo.ProcessingTime < 0 {
		t.Errorf("processing_time_ms = %d; debe ser >= 0", *cuerpo.ProcessingTime)
	}
	if stub.llamadas != 1 {
		t.Errorf("el caso de uso se invocó %d veces; se esperaba 1", stub.llamadas)
	}
}

// Más allá de la cota, http.MaxBytesReader marca la frontera: 413 con cuerpo
// RFC 9457.
func TestHandleExtract_RechazaElPayloadPorEncimaDe20MiB(t *testing.T) {
	stub := &stubService{}
	handler := NewExtractionHandler(stub)

	const maxDocumentBytes = 20 * 1024 * 1024
	solicitud := httptest.NewRequest(http.MethodPost, "/extract", newLazyReader(maxDocumentBytes+1))
	recibida := httptest.NewRecorder()

	handler.HandleExtract(recibida, solicitud)

	if recibida.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("HandleExtract() devolvió %d; se esperaba %d", recibida.Code, http.StatusRequestEntityTooLarge)
	}
	if ct := recibida.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q; se esperaba application/problem+json", ct)
	}

	problema := leerProblema(t, recibida.Body)
	if !strings.HasSuffix(problema.Type, "document-too-large") {
		t.Errorf("type = %q; se esperaba .../document-too-large", problema.Type)
	}
	if problema.Status != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d; se esperaba %d", problema.Status, http.StatusRequestEntityTooLarge)
	}
	if problema.Instance != "/extract" {
		t.Errorf("instance = %q; se esperaba /extract", problema.Instance)
	}
}

// Cada motivo de fallo del dominio llega al cliente como un ProblemDetails con
// su status y su `type` estable (§5). La saturación añade Retry-After, que
// §13 obliga a probar.
func TestHandleExtract_MapeaLosErroresDeDominio(t *testing.T) {
	casos := []struct {
		nombre     string
		dominio    error
		status     int
		sufijo     string
		retryAfter bool
	}{
		{nombre: "documento cifrado", dominio: domain.ErrEncryptedDocument, status: http.StatusUnprocessableEntity, sufijo: "encrypted-document"},
		{nombre: "documento corrupto", dominio: domain.ErrCorruptDocument, status: http.StatusUnprocessableEntity, sufijo: "corrupt-document"},
		{nombre: "saturación", dominio: domain.ErrResourceExhausted, status: http.StatusTooManyRequests, sufijo: "resource-exhausted", retryAfter: true},
		{nombre: "plazo agotado", dominio: domain.ErrExtractionTimeout, status: http.StatusGatewayTimeout, sufijo: "extraction-timeout"},
	}

	for _, caso := range casos {
		t.Run(caso.nombre, func(t *testing.T) {
			stub := &stubService{err: caso.dominio}
			handler := NewExtractionHandler(stub)

			solicitud := httptest.NewRequest(http.MethodPost, "/extract", strings.NewReader("%PDF-1.4"))
			recibida := httptest.NewRecorder()

			handler.HandleExtract(recibida, solicitud)

			if recibida.Code != caso.status {
				t.Fatalf("HandleExtract() devolvió %d; se esperaba %d", recibida.Code, caso.status)
			}
			if ct := recibida.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Errorf("Content-Type = %q; se esperaba application/problem+json", ct)
			}

			problema := leerProblema(t, recibida.Body)
			if problema.Type != "/problems/"+caso.sufijo {
				t.Errorf("type = %q; se esperaba /problems/%s", problema.Type, caso.sufijo)
			}
			if problema.Title == "" {
				t.Error("title está vacío; §5 lo quiere fijo por código")
			}
			if problema.Detail == "" {
				t.Error("detail está vacío; debe llevar el motivo de esta ocurrencia")
			}
			if problema.Status != caso.status {
				t.Errorf("status = %d; se esperaba %d", problema.Status, caso.status)
			}
			if problema.Instance != "/extract" {
				t.Errorf("instance = %q; se esperaba /extract", problema.Instance)
			}

			espera := recibida.Header().Get("Retry-After")
			if caso.retryAfter {
				if espera == "" {
					t.Error("Retry-After ausente; §9 exige la cabecera en la saturación")
				} else if segundos, err := strconv.Atoi(espera); err != nil || segundos < 1 {
					t.Errorf("Retry-After = %q; se esperaba un entero >= 1", espera)
				}
			}
		})
	}
}

// GET /health es el latido del proceso: 200 con {"status":"ok"} (§5).
func TestHandleHealth_Devuelve200(t *testing.T) {
	handler := NewExtractionHandler(&stubService{})

	solicitud := httptest.NewRequest(http.MethodGet, "/health", nil)
	recibida := httptest.NewRecorder()

	handler.HandleHealth(recibida, solicitud)

	if recibida.Code != http.StatusOK {
		t.Fatalf("HandleHealth() devolvió %d; se esperaba %d", recibida.Code, http.StatusOK)
	}

	var cuerpo struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(recibida.Body).Decode(&cuerpo); err != nil {
		t.Fatalf("el cuerpo no es JSON válido: %v", err)
	}
	if cuerpo.Status != "ok" {
		t.Errorf("status = %q; se esperaba ok", cuerpo.Status)
	}
}
