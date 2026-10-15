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

// run is the composition root (§7): loads config, wires layers inside-out,
// and shuts down the server gracefully.
func run() error {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return err
	}

	maxInFlight := cfg.MaxInFlight
	if maxInFlight == 0 {
		maxInFlight = runtime.NumCPU() // MAX_IN_FLIGHT=0 means NumCPU
	}

	server := &http.Server{
		Addr: ":" + cfg.Port,
		Handler: api.NewRouter(
			api.NewExtractionHandler(
				application.NewExtractionService(
					poppler.New(),
					application.NewAdmissionControl(maxInFlight, cfg.MaxQueue),
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
		errChan <- server.ListenAndServe()
	}()
	log.Printf("extractor listening on %s", server.Addr)

	select {
	case err := <-errChan:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cfg.ShutdownGrace)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server shutdown: %w", err)
	}
	return nil
}
