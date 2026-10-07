package api

import "net/http"

// NewRouter monta las rutas de §5 sobre el ServeMux de Go 1.22. Los patrones
// con método devuelven 405 Method Not Allowed ante un método no registrado.
//
// GET /ready y GET /metrics de §5 quedan fuera: el primero obligaría a exponer
// el estado del pool y el segundo sirve Prometheus en red interna.
func NewRouter(h *ExtractionHandler) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", h.HandleHealth)
	mux.HandleFunc("POST /extract", h.HandleExtract)

	return mux
}
