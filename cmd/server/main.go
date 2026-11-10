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

	"github.com/Zindorg/pdf-extractext-extractor/internal/adapters/cache"
	"github.com/Zindorg/pdf-extractext-extractor/internal/adapters/consul"
	"github.com/Zindorg/pdf-extractext-extractor/internal/adapters/poppler"
	"github.com/Zindorg/pdf-extractext-extractor/internal/adapters/ram"
	"github.com/Zindorg/pdf-extractext-extractor/internal/adapters/resilience"
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
// registers in Consul (if configured), drains traffic on SIGTERM, and shuts down gracefully.
func run(logger *slog.Logger) error {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return err
	}

	maxInFlight := cfg.MaxInFlight
	if maxInFlight == 0 {
		maxInFlight = runtime.NumCPU()
	}

	baseExtractor := poppler.New()
	resilientExtractor := resilience.NewResilientExtractor(
		baseExtractor,
		cfg.CBFailureThresh,
		cfg.CBResetTimeout,
		cfg.MaxRetries,
		cfg.RetryBackoff,
	)
	cachedExtractor := cache.NewCachedExtractor(
		resilientExtractor,
		cfg.CacheTTL,
		cfg.CacheMaxEntries,
	)

	admission := application.NewAdmissionControl(maxInFlight, cfg.MaxQueue)
	service := application.NewExtractionService(
		cachedExtractor,
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

	httpHandler := api.Idempotency(cfg.CacheTTL, mux)
	httpHandler = api.RateLimit(cfg.RateLimitRPS, cfg.RateLimitBurst, httpHandler)
	httpHandler = api.Observe(logger, httpHandler)

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           httpHandler,
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
	logger.Info("extractor_listening",
		"addr", server.Addr,
		"max_in_flight", maxInFlight,
		"max_queue", cfg.MaxQueue,
	)

	var registrar *consul.Registrar
	if cfg.ConsulAddr != "" {
		r, err := consul.NewRegistrar(cfg.ConsulAddr, cfg.ServiceName, cfg.ServiceAddress, cfg.Port)
		if err != nil {
			return err
		}
		registrar = r
		go registerWithRetry(ctx, logger, registrar)
	}

	select {
	case err := <-errChan:
		if err != nil {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	case <-ctx.Done():
	}

	// 12-Factor IX (Disposability): deregister from Consul, mark unready, and drain before closing.
	logger.Info("extractor_draining")
	draining.Store(true)

	if registrar != nil {
		deregCtx, cancelDereg := context.WithTimeout(context.Background(), 5*time.Second)
		if err := registrar.Deregister(deregCtx); err != nil {
			logger.Warn("consul_deregister_failed", "service_id", registrar.ServiceID(), "error", err.Error())
		} else {
			logger.Info("consul_deregistered", "service_id", registrar.ServiceID())
		}
		cancelDereg()
	}

	time.Sleep(5 * time.Second)

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cfg.ShutdownGrace)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server shutdown: %w", err)
	}
	logger.Info("extractor_stopped")
	return nil
}

func registerWithRetry(ctx context.Context, logger *slog.Logger, r *consul.Registrar) {
	for attempt := 1; attempt <= 10; attempt++ {
		if err := r.Register(ctx); err != nil {
			logger.Warn("consul_register_retry",
				"attempt", attempt,
				"service_id", r.ServiceID(),
				"error", err.Error(),
			)
			select {
			case <-time.After(2 * time.Second):
				continue
			case <-ctx.Done():
				return
			}
		}
		logger.Info("consul_registered", "service_id", r.ServiceID())
		return
	}
	logger.Error("consul_register_abandoned", "service_id", r.ServiceID())
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
