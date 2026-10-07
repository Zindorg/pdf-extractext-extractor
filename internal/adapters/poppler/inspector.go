package poppler

import (
	"bytes"
	"context"
	"strconv"
	"strings"

	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

// Inspector implementa domain.TextExtractor sobre el motor poppler.
type Inspector struct{}

// New construye el Inspector.
func New() *Inspector {
	return &Inspector{}
}

// Extract consulta a poppler dos veces —una para las páginas y otra para el
// texto— y le pasa el documento por la entrada estándar en ambas.
//
// El stream que entrega el DocumentSource va al hijo sin pasar por el heap de
// Go y sin tocar el disco: no se guarda ninguna copia, de modo que el tamaño
// del documento no cuenta contra el presupuesto de memoria de la aplicación.
func (i *Inspector) Extract(ctx context.Context, src domain.DocumentSource) (*domain.ExtractionResult, error) {
	paginas, err := pageCount(ctx, src)
	if err != nil {
		return nil, err
	}

	texto, err := text(ctx, src)
	if err != nil {
		return nil, err
	}

	// pdftotext separa las páginas con un salto de página (form feed). El
	// contrato del dominio es texto plano, así que aquí es donde se traduce.
	texto = strings.TrimSpace(strings.ReplaceAll(texto, "\f", "\n\n"))
	if texto == "" {
		return nil, domain.ErrNoTextLayer
	}

	return domain.NewExtractionResult(texto, paginas), nil
}

// pageCount ejecuta pdfinfo y lee el campo "Pages:" de su informe.
func pageCount(ctx context.Context, src domain.DocumentSource) (int, error) {
	salida, err := runPoppler(ctx, src, "pdfinfo", "-")
	if err != nil {
		return 0, err
	}

	for _, linea := range strings.Split(string(salida), "\n") {
		campo, valor, hayValor := strings.Cut(linea, ":")
		if !hayValor || campo != "Pages" {
			continue
		}

		paginas, err := strconv.Atoi(strings.TrimSpace(valor))
		if err != nil {
			return 0, domain.ErrCorruptDocument
		}

		return paginas, nil
	}

	return 0, domain.ErrCorruptDocument
}

// text ejecuta pdftotext: el primer guion lee de la entrada estándar y el
// segundo escribe en la salida estándar.
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
