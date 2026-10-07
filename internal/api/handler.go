package api

import (
	"context"
	"net/http"

	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

// ExtractionService es lo que la capa de API necesita del caso de uso. El
// *application.ExtractionService real la cumple sin declararla.
type ExtractionService interface {
	Process(ctx context.Context, src domain.DocumentSource) (*domain.ExtractionResult, error)
}

// ExtractionHandler recibe las peticiones del enrutador y las traduce a
// llamadas al caso de uso.
type ExtractionHandler struct {
	service ExtractionService
}

// NewExtractionHandler enlaza la capa de API con su caso de uso.
func NewExtractionHandler(service ExtractionService) *ExtractionHandler {
	return &ExtractionHandler{service: service}
}

// HandleExtract atiende POST /extract.
func (h *ExtractionHandler) HandleExtract(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotImplemented)
}

// HandleHealth atiende GET /health.
func (h *ExtractionHandler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotImplemented)
}
