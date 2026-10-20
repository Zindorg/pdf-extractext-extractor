package domain

import (
	"crypto/sha256"
	"encoding/hex"
)

// Warning representa un problema no fatal durante la extracción.
// Vocabulario abierto por contrato (workers.md §Extractor).
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ExtractionResult es el texto extraído de un documento junto con su recuento
// de páginas, la integridad de ese texto, el tiempo puro de extracción y
// avisos no fatales.
//
// El checksum lo calcula el dominio, no el adaptador: quien extrae el texto no
// debe saber nada de hashes.
type ExtractionResult struct {
	Text             string
	PageCount        int
	Checksum         string
	ExtractionTimeMs int64
	Warnings         []Warning
}

// NewExtractionResult construye el resultado de una extracción y calcula sobre
// la marcha la integridad del texto.
//
// El texto entra tal cual, sin normalizar: si un motor de extracción devuelve
// saltos de página o espacios de sobra, recortarlos es responsabilidad del
// adaptador que lo invoca. Aquí solo se mide lo que se recibe.
func NewExtractionResult(texto string, paginas int) *ExtractionResult {
	suma := sha256.Sum256([]byte(texto))

	return &ExtractionResult{
		Text:      texto,
		PageCount: paginas,
		Checksum:  hex.EncodeToString(suma[:]),
	}
}

// WithExtractionTimeMs inyecta el tiempo medido por el adaptador.
// Devuelve el mismo puntero para encadenamiento fluido.
func (r *ExtractionResult) WithExtractionTimeMs(ms int64) *ExtractionResult {
	r.ExtractionTimeMs = ms
	return r
}

// WithWarnings inyecta avisos no fatales del adaptador.
// Devuelve el mismo puntero para encadenamiento fluido.
func (r *ExtractionResult) WithWarnings(w []Warning) *ExtractionResult {
	r.Warnings = w
	return r
}
