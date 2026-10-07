package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/Zindorg/pdf-extractext-extractor/internal/adapters/poppler"
	"github.com/Zindorg/pdf-extractext-extractor/internal/api"
	"github.com/Zindorg/pdf-extractext-extractor/internal/application"
	"github.com/Zindorg/pdf-extractext-extractor/internal/config"
)

// shutdownGrace es el margen para terminar las extracciones en curso al
// apagarse, según §9 (SHUTDOWN_GRACE).
const shutdownGrace = 25 * time.Second

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

// run es el composition root (§7): carga la configuración, cablea las capas
// de dentro hacia afuera y apaga el servidor ordenadamente.
func run() error {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return err
	}

	capacidad := cfg.WorkerPoolSize
	if capacidad == 0 {
		capacidad = runtime.NumCPU() // §9: MAX_IN_FLIGHT=0 significa NumCPU
	}

	servidor := &http.Server{
		Addr: ":" + cfg.Port,
		Handler: api.NewRouter(
			api.NewExtractionHandler(
				application.NewExtractionService(
					poppler.New(),
					application.NewWorkerPool(capacidad),
				),
			),
		),
	}

	ctx, cancelar := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancelar()

	errores := make(chan error, 1)
	go func() {
		errores <- servidor.ListenAndServe()
	}()
	log.Printf("extractor escuchando en %s", servidor.Addr)

	select {
	case err := <-errores:
		return fmt.Errorf("servidor http: %w", err)
	case <-ctx.Done():
	}

	apagado, cancelarApagado := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancelarApagado()
	if err := servidor.Shutdown(apagado); err != nil {
		return fmt.Errorf("apagado del servidor: %w", err)
	}
	return nil
}
