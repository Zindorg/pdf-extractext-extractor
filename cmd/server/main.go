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

	maxInFlight := cfg.MaxInFlight
	if maxInFlight == 0 {
		maxInFlight = runtime.NumCPU() // MAX_IN_FLIGHT=0 means NumCPU
	}

	servidor := &http.Server{
		Addr: ":" + cfg.Port,
		Handler: api.NewRouter(
			api.NewExtractionHandler(
				application.NewExtractionService(
					poppler.New(),
					application.NewWorkerPool(maxInFlight, cfg.MaxQueue),
					cfg.QueueWaitTimeout,
					cfg.ExtractTimeout,
				),
				cfg.MaxDocumentBytes,
			),
		),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- servidor.ListenAndServe()
	}()
	log.Printf("extractor listening on %s", servidor.Addr)

	select {
	case err := <-errChan:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cfg.ShutdownGrace)
	defer cancelShutdown()
	if err := servidor.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server shutdown: %w", err)
	}
	return nil
}
