package poppler

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

// fileSource es el DocumentSource de los tests: un fichero de testdata/ que se
// puede abrir tantas veces como haga falta.
type fileSource struct{ ruta string }

func (s fileSource) Open() (io.ReadCloser, error) { return os.Open(s.ruta) }

func sourceFor(t *testing.T, nombre string) domain.DocumentSource {
	t.Helper()
	ruta := filepath.Join("testdata", nombre)
	if _, err := os.Stat(ruta); err != nil {
		t.Fatalf("fixture ausente: %s", ruta)
	}
	return fileSource{ruta: ruta}
}

func TestExtract_ValidoDevuelveTextoNoVacio(t *testing.T) {
	inspector := New()
	resultado, err := inspector.Extract(context.Background(), sourceFor(t, "valido.pdf"))

	if err != nil {
		t.Fatalf("se esperaba extracción correcta, err = %v", err)
	}
	if resultado == nil {
		t.Fatal("se esperaba ExtractionResult, se obtuvo nil: el adaptador sigue en rojo")
	}
	if resultado.Text == "" {
		t.Error("el texto extraído no debe estar vacío")
	}
}

func TestExtract_ValidoDevuelvePageCountMayorQueCero(t *testing.T) {
	inspector := New()
	resultado, err := inspector.Extract(context.Background(), sourceFor(t, "valido.pdf"))

	if err != nil {
		t.Fatalf("se esperaba extracción correcta, err = %v", err)
	}
	if resultado == nil {
		t.Fatal("se esperaba ExtractionResult, se obtuvo nil: el adaptador sigue en rojo")
	}
	if resultado.PageCount <= 0 {
		t.Errorf("PageCount = %d; se esperaba > 0", resultado.PageCount)
	}
}

func TestExtract_ValidoDevuelveChecksumDe64Hex(t *testing.T) {
	inspector := New()
	resultado, err := inspector.Extract(context.Background(), sourceFor(t, "valido.pdf"))

	if err != nil {
		t.Fatalf("se esperaba extracción correcta, err = %v", err)
	}
	if resultado == nil {
		t.Fatal("se esperaba ExtractionResult, se obtuvo nil: el adaptador sigue en rojo")
	}
	if len(resultado.Checksum) != 64 {
		t.Fatalf("Checksum = %q (longitud %d); se esperaban 64 caracteres", resultado.Checksum, len(resultado.Checksum))
	}
	for i, c := range resultado.Checksum {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("Checksum = %q: el carácter %d (%q) no es hexadecimal en minúsculas", resultado.Checksum, i, c)
		}
	}
}

func TestExtract_SinCapaDeTextoDevuelveErrNoTextLayer(t *testing.T) {
	inspector := New()
	resultado, err := inspector.Extract(context.Background(), sourceFor(t, "sin_texto.pdf"))

	if !errors.Is(err, domain.ErrNoTextLayer) {
		t.Fatalf("err = %v; se esperaba ErrNoTextLayer", err)
	}
	if resultado != nil {
		t.Errorf("resultado = %+v; se esperaba nil cuando la extracción falla", resultado)
	}
}

func TestExtract_PlazoVencidoDevuelveErrExtractionTimeout(t *testing.T) {
	inspector := New()
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	resultado, err := inspector.Extract(ctx, sourceFor(t, "valido.pdf"))

	if !errors.Is(err, domain.ErrExtractionTimeout) {
		t.Fatalf("err = %v; se esperaba ErrExtractionTimeout", err)
	}
	if resultado != nil {
		t.Errorf("resultado = %+v; se esperaba nil", resultado)
	}
}

func TestExtract_CifradoDevuelveErrEncryptedDocument(t *testing.T) {
	inspector := New()
	resultado, err := inspector.Extract(context.Background(), sourceFor(t, "cifrado.pdf"))

	if !errors.Is(err, domain.ErrEncryptedDocument) {
		t.Fatalf("err = %v; se esperaba ErrEncryptedDocument: pdfinfo se niega a "+
			"abrir el documento sin su contraseña", err)
	}
	if resultado != nil {
		t.Errorf("resultado = %+v; se esperaba nil cuando la extracción falla", resultado)
	}
}

func TestExtract_CorruptoDevuelveErrCorruptDocument(t *testing.T) {
	inspector := New()
	resultado, err := inspector.Extract(context.Background(), sourceFor(t, "corrupto.pdf"))

	if !errors.Is(err, domain.ErrCorruptDocument) {
		t.Fatalf("err = %v; se esperaba ErrCorruptDocument: el documento no tiene "+
			"trailer ni tabla xref, y el fallo no menciona ninguna contraseña", err)
	}
	if resultado != nil {
		t.Errorf("resultado = %+v; se esperaba nil cuando la extracción falla", resultado)
	}
}

// pdftotext separa las páginas con un salto de página (form feed). El contrato
// con el dominio es texto plano: ese carácter tiene que llegar convertido en un
// doble salto de línea, ni desaparecer ni quedar tal cual.
func TestExtract_NormalizaElSaltoDePaginaADobleSalto(t *testing.T) {
	inspector := New()
	resultado, err := inspector.Extract(context.Background(), sourceFor(t, "valido.pdf"))

	if err != nil {
		t.Fatalf("se esperaba extracción correcta, err = %v", err)
	}

	const esperado = "valido.pdf pagina 1\nfixture de extraccion de texto\n\n\n\n" +
		"valido.pdf pagina 2\nla segunda pagina tambien tiene capa de texto"

	if resultado.Text != esperado {
		t.Errorf("Text = %q;\nse esperaba %q", resultado.Text, esperado)
	}
}

// TestExtract_ConteoDePaginasPorFormFeed valida que el conteo de páginas
// proviene del carácter \f en la salida cruda de pdftotext.
func TestExtract_ConteoDePaginasPorFormFeed(t *testing.T) {
	// Este test usa el fixture "valido.pdf" que sabemos tiene 2 páginas
	// (contiene un \f entre página 1 y 2).
	inspector := New()
	resultado, err := inspector.Extract(context.Background(), sourceFor(t, "valido.pdf"))

	if err != nil {
		t.Fatalf("se esperaba extracción correcta, err = %v", err)
	}

	// valido.pdf tiene 2 páginas → 1 salto de página \f
	if resultado.PageCount != 2 {
		t.Errorf("PageCount = %d; se esperaba 2 (basado en conteo de \\f)", resultado.PageCount)
	}
}
