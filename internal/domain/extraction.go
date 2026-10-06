package domain

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
