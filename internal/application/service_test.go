package application

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

// fixedExtractor es un TextExtractor que siempre devuelve el mismo resultado y
// no llega a mirar la fuente: este issue admite y delega, no extrae.
type fixedExtractor struct{}

func (fixedExtractor) Extract(context.Context, domain.DocumentSource) (*domain.ExtractionResult, error) {
	return domain.NewExtractionResult("texto extraido", 1), nil
}

// blockingExtractor no sale de Extract hasta que le abran, para que un test
// pueda mantener un hueco ocupado mientras vuelve a llamar a Process.
type blockingExtractor struct {
	entered chan struct{} // avisa, sin bloquear, de que ya está dentro
	unblock chan struct{} // el test lo cierra para dejarlo terminar
}

func (b *blockingExtractor) Extract(context.Context, domain.DocumentSource) (*domain.ExtractionResult, error) {
	select {
	case b.entered <- struct{}{}:
	default:
	}
	<-b.unblock

	return domain.NewExtractionResult("texto extraido", 1), nil
}

// emptySource es una fuente que los extractores de este test no llegan a abrir.
type emptySource struct{}

func (emptySource) Open() (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

// Si el hueco no se liberara, esta segunda llamada en serie devolvería
// ErrResourceExhausted y el servicio se quedaría sin capacidad para siempre.
func TestProcess_LiberaElHuecoTrasLaExtraccion(t *testing.T) {
	pool := NewWorkerPool(1)
	servicio := NewExtractionService(fixedExtractor{}, pool)

	resultado, err := servicio.Process(context.Background(), emptySource{})
	if err != nil {
		t.Fatalf("la primera extracción debía funcionar, pero falló con: %v", err)
	}
	if resultado.Text != "texto extraido" {
		t.Errorf("Process() no devolvió el texto que extrajo el adaptador: %q", resultado.Text)
	}

	if _, err := servicio.Process(context.Background(), emptySource{}); err != nil {
		t.Errorf("el hueco no se liberó tras la primera extracción: %v", err)
	}
}

// Sin hueco libre la admisión es inmediata: no se espera a que nadie termine.
func TestProcess_DevuelveErrResourceExhaustedSinHuecoLibre(t *testing.T) {
	pool := NewWorkerPool(1)
	extractor := &blockingExtractor{
		entered: make(chan struct{}, 1),
		unblock: make(chan struct{}),
	}
	servicio := NewExtractionService(extractor, pool)

	var errOcupada error
	terminada := make(chan struct{})
	go func() {
		defer close(terminada)
		_, errOcupada = servicio.Process(context.Background(), emptySource{})
	}()

	<-extractor.entered // la otra petición está dentro de Extract con el hueco tomado

	if _, err := servicio.Process(context.Background(), emptySource{}); !errors.Is(err, domain.ErrResourceExhausted) {
		t.Errorf("Process() con la capacidad agotada devolvió %v; se esperaba %v", err, domain.ErrResourceExhausted)
	}

	close(extractor.unblock)
	<-terminada // el receive del canal sincroniza la lectura de errOcupada
	if errOcupada != nil {
		t.Errorf("la extracción que ocupaba el hueco debía terminar bien, pero falló con: %v", errOcupada)
	}

	if _, err := servicio.Process(context.Background(), emptySource{}); err != nil {
		t.Errorf("el hueco no volvió a quedar libre al terminar la otra extracción: %v", err)
	}
}

// Un Release sin ningún hueco tomado no debe bloquear la goroutine que lo llama
// ni, tampoco, fabricar un cupo por encima de la capacidad del pool.
func TestWorkerPool_ReleaseAdicionalNoSaturaElPool(t *testing.T) {
	pool := NewWorkerPool(1)

	pool.Release()

	if !pool.Acquire() {
		t.Error("Acquire() debía conceder el único hueco libre")
	}
	if pool.Acquire() {
		t.Error("Acquire() concedió un segundo hueco con un pool de capacidad 1")
	}
}
