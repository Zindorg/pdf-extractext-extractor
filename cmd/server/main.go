package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Zindorg/pdf-extractext-extractor/internal/adapters/poppler"
	"github.com/Zindorg/pdf-extractext-extractor/internal/adapters/ram"
	"github.com/Zindorg/pdf-extractext-extractor/internal/api"
	"github.com/Zindorg/pdf-extractext-extractor/internal/application"
	"github.com/Zindorg/pdf-extractext-extractor/internal/config"
)

func main() {
	healthcheck := flag.Bool("healthcheck", false, "probe for Docker HEALTHCHECK")
	flag.Parse()
	if *healthcheck {
		os.Exit(probe(os.Getenv("HTTP_PORT")))
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("server_failed", "error", err.Error())
		os.Exit(1)
	}
}

// run is the composition root: loads config, wires layers inside-out,
// drains traffic on SIGTERM, and shuts down gracefully.
func run(logger *slog.Logger) error {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return err
	}

	maxInFlight := cfg.MaxInFlight
	if maxInFlight == 0 {
		maxInFlight = runtime.NumCPU()
	}

	admission := application.NewAdmissionControl(maxInFlight, cfg.MaxQueue)
	service := application.NewExtractionService(
		poppler.New(),
		admission,
		cfg.QueueWaitTimeout,
		cfg.ExtractTimeout,
	)
	handler := api.NewExtractionHandler(service, ram.NewStore(), cfg.MaxDocumentBytes)

	var draining atomic.Bool
	ready := func() bool {
		return !draining.Load() && !admission.Saturated()
	}

	mux := api.NewRouter(handler, ready)

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           api.Observe(logger, mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- err
		} else {
			errChan <- nil
		}
	}()
	logger.Info("extractor_listening", "addr", server.Addr, "max_in_flight", maxInFlight, "max_queue", cfg.MaxQueue)

	select {
	case err := <-errChan:
		if err != nil {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	case <-ctx.Done():
	}

	// 12-Factor IX (Disposability): mark unready and drain before closing listeners.
	logger.Info("extractor_draining")
	draining.Store(true)
	time.Sleep(5 * time.Second)

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cfg.ShutdownGrace)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server shutdown: %w", err)
	}
	logger.Info("extractor_stopped")
	return nil
}

func probe(port string) int {
	if port == "" {
		port = "8081"
	}
	c := http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get("http://127.0.0.1:" + port + "/health")
	if err != nil || resp.StatusCode != http.StatusOK {
		return 1
	}
	_ = resp.Body.Close()
	return 0
}
