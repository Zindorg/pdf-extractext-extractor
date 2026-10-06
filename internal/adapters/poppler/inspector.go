package poppler

import (
	"context"
	"errors"
	"os/exec"
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
func runPoppler(ctx context.Context, src domain.DocumentSource, programa string, args ...string) ([]byte, error) {
	entrada, err := src.Open()
	if err != nil {
		return nil, err
	}
	defer entrada.Close()

	mando := exec.CommandContext(ctx, programa, args...)
	mando.Stdin = entrada

	salida, err := mando.Output()
	if err == nil {
		return salida, nil
	}

	return nil, classifyError(ctx, err)
}

// classifyError traduce un fallo de poppler a uno de los motivos de fallo del
// dominio. Es el único punto del adaptador que mira el stderr, y lo hace en
// este orden:
//
//  1. el contexto ya no está vivo: el proceso murió por nuestra cuenta;
//  2. poppler menciona una contraseña: el documento está cifrado;
//  3. cualquier otro fallo: el documento está roto.
//
// Solo se clasifican los procesos que terminan con error. poppler escribe
// avisos de sintaxis en el stderr incluso cuando el resultado es utilizable,
// y esos no invalidan la extracción.
//
// Deuda conocida: el punto 1 funde la cancelación del cliente con el plazo
// agotado, que en el contrato HTTP son dos cosas distintas —EXTRACTION_TIMEOUT
// responde 504 y CANCELED no escribe respuesta. Hoy no existe sentinela ni
// test que las distinga, así que se comportan igual; separarlas es un ciclo
// con su propia prueba.
func classifyError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return domain.ErrExtractionTimeout
	}

	var salida *exec.ExitError
	if errors.As(err, &salida) &&
		strings.Contains(strings.ToLower(string(salida.Stderr)), "password") {
		return domain.ErrEncryptedDocument
	}

	return domain.ErrCorruptDocument
}

var _ domain.TextExtractor = (*Inspector)(nil)
