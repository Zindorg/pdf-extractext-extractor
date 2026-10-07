//go:build linux

package ram

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// lectorFallido devuelve un error a mitad de lectura para probar el cierre
// sobre-error de NewBuffer.
type lectorFallido struct{}

func (lectorFallido) Read([]byte) (int, error) {
	return 0, errors.New("lectura interrumpida")
}

func TestNewBuffer_VuelcaElContenidoDelLector(t *testing.T) {
	payload := "contenido de prueba del memfd"

	fichero, err := NewBuffer(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("NewBuffer devolvió error: %v", err)
	}
	defer fichero.Close()

	volcado, err := io.ReadAll(fichero)
	if err != nil {
		t.Fatalf("leer el buffer falló: %v", err)
	}
	if string(volcado) != payload {
		t.Fatalf("contenido = %q, esperaba %q", volcado, payload)
	}
}

func TestNewBuffer_PermiteReleerDesdeElPrincipio(t *testing.T) {
	payload := "segundo uso del mismo buffer"

	fichero, err := NewBuffer(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("NewBuffer devolvió error: %v", err)
	}
	defer fichero.Close()

	primera, err := io.ReadAll(fichero)
	if err != nil {
		t.Fatalf("primera lectura falló: %v", err)
	}
	if string(primera) != payload {
		t.Fatalf("primera lectura = %q, esperaba %q", primera, payload)
	}

	reabible := Source(fichero)
	releido, err := reabible.Open()
	if err != nil {
		t.Fatalf("Open() devolvió error: %v", err)
	}
	defer releido.Close()

	segunda, err := io.ReadAll(releido)
	if err != nil {
		t.Fatalf("relectura falló: %v", err)
	}
	if string(segunda) != payload {
		t.Fatalf("relectura = %q, esperaba %q", segunda, payload)
	}
}

func TestNewBuffer_DevuelveErrorSiLaLecturaFalla(t *testing.T) {
	fichero, err := NewBuffer(lectorFallido{})
	if err == nil {
		t.Fatal("esperaba un error de lectura, no llegó")
	}
	if fichero != nil {
		t.Fatalf("esperaba fichero nil tras el fallo, recibí %v", fichero)
	}
}
