//go:build linux

// Package ram materializa el documento entrante en un buffer anónimo del
// kernel (memfd), sin ruta en el sistema de archivos (§7 y §8 de la
// arquitectura): el cuerpo llega del socket al RAM por io.Copy y nunca pasa
// por el heap de Go, y el descriptor muere con la petición cuando el handler
// lo cierra.
package ram

import (
	"io"
	"os"

	"golang.org/x/sys/unix"

	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

// NewBuffer crea un memfd y vuelca en él lo que produce r: devuelve el
// fichero ya rebobinado al principio, listo para releerse.
//
// Si el volcado falla, el descriptor se cierra antes de retornar y NewBuffer
// devuelve el error que lo causó — p. ej. un *http.MaxBytesError cuando el
// cuerpo rebasa la cota del handler.
func NewBuffer(r io.Reader) (*os.File, error) {
	descriptor, err := unix.MemfdCreate("pdf_buffer", 0)
	if err != nil {
		return nil, err
	}

	fichero := os.NewFile(uintptr(descriptor), "pdf_buffer")
	if _, err := io.Copy(fichero, r); err != nil {
		fichero.Close()
		return nil, err
	}

	if _, err := fichero.Seek(0, io.SeekStart); err != nil {
		fichero.Close()
		return nil, err
	}

	return fichero, nil
}

// fuenteMemfd hace reabrible un memfd para los dos pases de poppler: cada
// Open() rebobina el base y entrega un descriptor duplicado.
//
// El duplicado es imprescindible: poppler cierra el lector que le dan
// (internal/adapters/poppler), y dup comparte el puntero de posición de la
// descripción de archivo — por eso el Seek precede al Dup y queda en el base,
// que es el que cierra el handler con defer file.Close().
type fuenteMemfd struct {
	fichero *os.File
}

func (f fuenteMemfd) Open() (io.ReadCloser, error) {
	if _, err := f.fichero.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	duplicado, err := unix.Dup(int(f.fichero.Fd()))
	if err != nil {
		return nil, err
	}

	return os.NewFile(uintptr(duplicado), f.fichero.Name()), nil
}

// Source envuelve un memfd en un DocumentSource reabrible. El *os.File sigue
// en manos del llamador, que debe cerrarlo al terminar la petición.
func Source(f *os.File) domain.DocumentSource {
	return fuenteMemfd{fichero: f}
}
