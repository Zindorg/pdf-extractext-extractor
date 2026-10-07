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

// ExtractionService es lo que la capa de API necesita del caso de uso. El
// *application.ExtractionService real la cumple sin declararla.
type ExtractionService interface {
	Process(ctx context.Context, src domain.DocumentSource) (*domain.ExtractionResult, error)
}

// maxDocumentBytes es la cota de entrada actual (§9). Pasará a la
// configuración en cuanto exista internal/config.
const maxDocumentBytes = 20 * 1024 * 1024

// ExtractionHandler recibe las peticiones del enrutador y las traduce a
// llamadas al caso de uso.
type ExtractionHandler struct {
	service ExtractionService
}

// NewExtractionHandler enlaza la capa de API con su caso de uso.
func NewExtractionHandler(service ExtractionService) *ExtractionHandler {
	return &ExtractionHandler{service: service}
}

// HandleExtract atiende POST /extract: acota el cuerpo, lo vuelca a un memfd
// (§8: cero disco y cero heap de Go), delega en el caso de uso y traduce el
// resultado o el fallo al contrato. El memfd muere con esta llamada.
func (h *ExtractionHandler) HandleExtract(w http.ResponseWriter, r *http.Request) {
	empezado := time.Now()
	r.Body = http.MaxBytesReader(w, r.Body, maxDocumentBytes)

	fichero, err := ram.NewBuffer(r.Body)
	if err != nil {
		var cuerpoExcesivo *http.MaxBytesError
		if errors.As(err, &cuerpoExcesivo) {
			escribirProblema(w, http.StatusRequestEntityTooLarge, "Payload Too Large", "document-too-large", err.Error(), r.URL.Path)
			return
		}
		escribirError(w, err, r.URL.Path)
		return
	}
	defer fichero.Close()

	resultado, err := h.service.Process(r.Context(), ram.Source(fichero))
	if err != nil {
		escribirError(w, err, r.URL.Path)
		return
	}

	escribirResultado(w, r, empezado, resultado)
}

// HandleHealth atiende GET /health: el latido del proceso (§5).
func (h *ExtractionHandler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(saludResponse{Status: "ok"})
}

// extraccionResponse es el cuerpo del 200 (§5), con document_id en eco.
type extraccionResponse struct {
	DocumentID       string `json:"document_id"`
	ExtractedText    string `json:"extracted_text"`
	PageCount        int    `json:"page_count"`
	Checksum         string `json:"checksum"`
	ProcessingTimeMS int64  `json:"processing_time_ms"`
}

// escribirResultado responde el 200 con el eco y las mediciones del §5.
func escribirResultado(w http.ResponseWriter, r *http.Request, empezado time.Time, resultado *domain.ExtractionResult) {
	if id := r.Header.Get("X-Document-ID"); id != "" {
		w.Header().Set("X-Document-ID", id)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(extraccionResponse{
		DocumentID:       r.Header.Get("X-Document-ID"),
		ExtractedText:    resultado.Text,
		PageCount:        resultado.PageCount,
		Checksum:         resultado.Checksum,
		ProcessingTimeMS: time.Since(empezado).Milliseconds(),
	})
}

// saludResponse es el cuerpo del 200 de /health (§5).
type saludResponse struct {
	Status string `json:"status"`
}

// causa describe un motivo de fallo del contrato (§5): su status, su `type`
// estable y la cabecera extra que lleva, si lleva.
type causa struct {
	err        error
	status     int
	title      string
	sufijo     string
	retryAfter string
}

// motivos es la tabla de equivalencias entre el dominio y RFC 9457.
var motivos = []causa{
	{err: domain.ErrDocumentTooLarge, status: http.StatusRequestEntityTooLarge, title: "Payload Too Large", sufijo: "document-too-large"},
	{err: domain.ErrEncryptedDocument, status: http.StatusUnprocessableEntity, title: "Unprocessable Entity", sufijo: "encrypted-document"},
	{err: domain.ErrCorruptDocument, status: http.StatusUnprocessableEntity, title: "Unprocessable Entity", sufijo: "corrupt-document"},
	{err: domain.ErrResourceExhausted, status: http.StatusTooManyRequests, title: "Too Many Requests", sufijo: "resource-exhausted", retryAfter: "1"},
	{err: domain.ErrExtractionTimeout, status: http.StatusGatewayTimeout, title: "Gateway Timeout", sufijo: "extraction-timeout"},
}

// escribirError responde el fallo del caso de uso como ProblemDetails. Lo que
// no es un motivo conocido se oculta tras /problems/internal: el detalle solo
// le importa a quien ejecuta el servicio.
func escribirError(w http.ResponseWriter, err error, instance string) {
	for _, motivo := range motivos {
		if errors.Is(err, motivo.err) {
			if motivo.retryAfter != "" {
				w.Header().Set("Retry-After", motivo.retryAfter)
			}
			escribirProblema(w, motivo.status, motivo.title, motivo.sufijo, err.Error(), instance)
			return
		}
	}

	escribirProblema(w, http.StatusInternalServerError, "Internal Server Error", "internal", err.Error(), instance)
}

// escribirProblema escribe un ProblemDetails (RFC 9457).
func escribirProblema(w http.ResponseWriter, status int, title, sufijo, detail, instance string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ProblemDetails{
		Type:     "/problems/" + sufijo,
		Title:    title,
		Status:   status,
		Detail:   detail,
		Instance: instance,
	})
}
