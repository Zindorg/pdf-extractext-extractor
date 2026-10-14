package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config agrupa la configuración de arranque (§9). Todo llega por variables de
// entorno y se valida en la frontera: un valor inválido impide arrancar.
type Config struct {
	// Server
	Port string

	// Admission control (worker pool)
	MaxInFlight int
	MaxQueue    int

	// Timeouts
	QueueWaitTimeout time.Duration
	ExtractTimeout   time.Duration
	ShutdownGrace    time.Duration

	// Limits
	MaxDocumentBytes int64
}

// Nombres de las variables de entorno y defectos que fija §9.
const (
	// Server
	envPort = "HTTP_PORT"
	defaultPort = "8081"

	// Admission control
	envMaxInFlight = "MAX_IN_FLIGHT"
	envMaxQueue    = "MAX_QUEUE"
	defaultMaxInFlight = 0 // 0 means runtime.NumCPU()
	defaultMaxQueue = 128

	// Timeouts
	envQueueWaitTimeout = "QUEUE_WAIT_TIMEOUT"
	envExtractTimeout   = "EXTRACT_TIMEOUT"
	envShutdownGrace    = "SHUTDOWN_GRACE"
	defaultQueueWait    = 10 * time.Second
	defaultExtractWait  = 6 * time.Second
	defaultShutdownGrace = 25 * time.Second

	// Limits
	envMaxDocumentBytes = "MAX_DOCUMENT_BYTES"
	defaultMaxDocumentBytes = 20 * 1024 * 1024 // 20 MiB
)

// LoadFromEnv lee y valida la configuración del entorno.
func LoadFromEnv() (*Config, error) {
	port := os.Getenv(envPort)
	if port == "" {
		port = defaultPort
	}
	if err := validatePort(port); err != nil {
		return nil, err
	}

	maxInFlight, err := parseIntEnv(envMaxInFlight, defaultMaxInFlight, 0, 0)
	if err != nil {
		return nil, err
	}

	maxQueue, err := parseIntEnv(envMaxQueue, defaultMaxQueue, 0, 0)
	if err != nil {
		return nil, err
	}

	queueWait, err := parseDurationEnv(envQueueWaitTimeout, defaultQueueWait)
	if err != nil {
		return nil, err
	}

	extractTimeout, err := parseDurationEnv(envExtractTimeout, defaultExtractWait)
	if err != nil {
		return nil, err
	}

	shutdownGrace, err := parseDurationEnv(envShutdownGrace, defaultShutdownGrace)
	if err != nil {
		return nil, err
	}

	maxDocumentBytes, err := parseInt64Env(envMaxDocumentBytes, defaultMaxDocumentBytes, 0)
	if err != nil {
		return nil, err
	}

	return &Config{
		Port:              port,
		MaxInFlight:       maxInFlight,
		MaxQueue:          maxQueue,
		QueueWaitTimeout:  queueWait,
		ExtractTimeout:    extractTimeout,
		ShutdownGrace:     shutdownGrace,
		MaxDocumentBytes:  maxDocumentBytes,
	}, nil
}

func parseIntEnv(key string, defaultVal, min, max int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return defaultVal, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("config: %s must be an integer >= %d, got %q: %w", key, min, raw, err)
	}
	if n < min {
		return 0, fmt.Errorf("config: %s cannot be negative: %d", key, n)
	}
	if max > 0 && n > max {
		return 0, fmt.Errorf("config: %s cannot exceed %d: %d", key, max, n)
	}
	return n, nil
}

func parseInt64Env(key string, defaultVal int64, min int64) (int64, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return defaultVal, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("config: %s must be an integer >= %d, got %q: %w", key, min, raw, err)
	}
	if n < min {
		return 0, fmt.Errorf("config: %s cannot be negative: %d", key, n)
	}
	return n, nil
}

func parseDurationEnv(key string, defaultVal time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return defaultVal, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("config: %s must be a valid duration, got %q: %w", key, raw, err)
	}
	return d, nil
}

func validatePort(port string) error {
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("config: %s must be a port between 1 and 65535, got %q", envPort, port)
	}
	return nil
}
