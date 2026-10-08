package poppler

import (
	"bytes"
	"context"
	"strings"

	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

// Inspector implementa domain.TextExtractor sobre el motor poppler.
type Inspector struct{}

func New() *Inspector {
	return &Inspector{}
}

func (i *Inspector) Extract(ctx context.Context, src domain.DocumentSource) (*domain.ExtractionResult, error) {
	textoCrudo, err := text(ctx, src)
	if err != nil {
		return nil, err
	}

	// Contar páginas: cada \f = salto de página en pdftotext
	paginas := strings.Count(textoCrudo, "\f")
	if paginas == 0 && len(textoCrudo) > 0 {
		paginas = 1 // fallback: PDF de una página sin \f final
	}

	// Normalizar saltos de página a párrafos
	texto := strings.TrimSpace(strings.ReplaceAll(textoCrudo, "\f", "\n\n"))
	if texto == "" {
		return nil, domain.ErrNoTextLayer
	}

	return domain.NewExtractionResult(texto, paginas), nil
}

// text ejecuta pdftotext y devuelve el texto crudo (con \f).
func text(ctx context.Context, src domain.DocumentSource) (string, error) {
	salida, err := runPoppler(ctx, src, "pdftotext", "-", "-")
	if err != nil {
		return "", err
	}
	return string(salida), nil
}

// runPoppler lanza una utilidad de poppler con el documento en la entrada
// estándar y devuelve su salida estándar.
//
// El stream se entrega a exec tal cual: cuando es un fichero, el hijo recibe
// el descriptor sin intermediarios, con tamaño conocido y con acceso aleatorio,
// que es lo que pdfinfo y pdftotext necesitan para localizar la tabla xref.
// Ante un fallo se condensa en un runDiagnostics (código de salida + stderr) y
// se delega en classifyReport la traducción al motivo de dominio.
func runPoppler(ctx context.Context, src domain.DocumentSource, programa string, args ...string) ([]byte, error) {
	entrada, err := src.Open()
	if err != nil {
		return nil, err
	}
	defer entrada.Close()

	mando := popplerCommand(ctx, programa, args...)
	mando.Stdin = entrada

	var salida, errores bytes.Buffer
	mando.Stdout = &salida
	mando.Stderr = &errores

	if err := mando.Run(); err != nil {
		reporte := runDiagnostics{ExitCode: 0, Stderr: errores.String()}
		if mando.ProcessState != nil {
			reporte.ExitCode = mando.ProcessState.ExitCode()
		}
		return nil, classifyReport(ctx, reporte)
	}

	return salida.Bytes(), nil
}

var _ domain.TextExtractor = (*Inspector)(nil)