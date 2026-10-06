package domain

import "testing"

// Vectores conocidos de SHA-256, comprobados fuera de Go. Fijar el valor
// exacto en vez de recalcularlo con crypto/sha256 dentro del test es lo que
// distingue una prueba del algoritmo de una tautología.
const (
	sha256Hola = "b221d9dbb083a7f33428d7c2a3c3198ae925614d70210e28716ccaa7cd4ddb79"

	textoConSaltos = "primera pagina\nsegunda linea"
	sha256Saltos   = "2af80b293eab93adeab733a41a5822005a80960fb119cb043af3abc85dd2ad4d"
)

func TestNewExtractionResult_ChecksumCoincideConElSHA256DelTexto(t *testing.T) {
	resultado := NewExtractionResult("hola", 12)

	if resultado.Checksum != sha256Hola {
		t.Errorf("Checksum = %q; se esperaba el SHA-256 de %q = %q",
			resultado.Checksum, "hola", sha256Hola)
	}
}

func TestNewExtractionResult_NoNormalizaLosSaltosDeLinea(t *testing.T) {
	resultado := NewExtractionResult(textoConSaltos, 1)

	if resultado.Checksum != sha256Saltos {
		t.Errorf("Checksum = %q; el checksum es del texto tal cual, sin transformar "+
			"(normalizar los saltos de página es del adaptador, no del dominio). "+
			"Se esperaba %q", resultado.Checksum, sha256Saltos)
	}
}

func TestNewExtractionResult_ConservaElTextoYElPageCount(t *testing.T) {
	resultado := NewExtractionResult(textoConSaltos, 12)

	if resultado.Text != textoConSaltos {
		t.Errorf("Text = %q; no debe alterar el texto de entrada", resultado.Text)
	}
	if resultado.PageCount != 12 {
		t.Errorf("PageCount = %d; se esperaba 12", resultado.PageCount)
	}
}
