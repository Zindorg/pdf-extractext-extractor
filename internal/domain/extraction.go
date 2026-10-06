package domain

import (
	"crypto/sha256"
	"encoding/hex"
)

// ExtractionResult es el texto extraído de un documento junto con su recuento
// de páginas y la integridad de ese texto.
//
// El checksum lo calcula el dominio, no el adaptador: quien extrae el texto no
// debe saber nada de hashes.
type ExtractionResult struct {
	Text      string
	PageCount int
	Checksum  string
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
