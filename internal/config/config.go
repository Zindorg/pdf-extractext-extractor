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
	Port             string
	WorkerPoolSize   int
	MaxQueue         int
	QueueWaitTimeout time.Duration
	ExtractTimeout   time.Duration
}

// Nombres de las variables de entorno y defectos que fija §9.
const (
	envPort             = "HTTP_PORT"
	envMaxInFlight      = "MAX_IN_FLIGHT"
	envMaxQueue         = "MAX_QUEUE"
	envQueueWaitTimeout = "QUEUE_WAIT_TIMEOUT"
	envExtractTimeout   = "EXTRACT_TIMEOUT"
	defaultPort         = "8081"
	defaultPoolSize     = 0 // 0 significa runtime.NumCPU(), que resuelve el main
	defaultMaxQueue     = 128
	defaultQueueWait    = 10 * time.Second
	defaultExtractWait  = 6 * time.Second
)

// LoadFromEnv lee y valida la configuración del entorno.
func LoadFromEnv() (*Config, error) {
	puerto := os.Getenv(envPort)
	if puerto == "" {
		puerto = defaultPort
	}
	if err := validarPuerto(puerto); err != nil {
		return nil, err
	}

	tamano := defaultPoolSize
	if crudo := os.Getenv(envMaxInFlight); crudo != "" {
		n, err := strconv.Atoi(crudo)
		if err != nil {
			return nil, fmt.Errorf("config: %s debe ser un entero >= 0, se recibió %q", envMaxInFlight, crudo)
		}
		tamano = n
	}
	if tamano < 0 {
		return nil, fmt.Errorf("config: %s no puede ser negativo: %d", envMaxInFlight, tamano)
	}

	maxQueue := defaultMaxQueue
	if crudo := os.Getenv(envMaxQueue); crudo != "" {
		n, err := strconv.Atoi(crudo)
		if err != nil {
			return nil, fmt.Errorf("config: %s debe ser un entero >= 0, se recibió %q", envMaxQueue, crudo)
		}
		maxQueue = n
	}
	if maxQueue < 0 {
		return nil, fmt.Errorf("config: %s no puede ser negativo: %d", envMaxQueue, maxQueue)
	}

	queueWait := defaultQueueWait
	if crudo := os.Getenv(envQueueWaitTimeout); crudo != "" {
		d, err := time.ParseDuration(crudo)
		if err != nil {
			return nil, fmt.Errorf("config: %s debe ser una duración válida, se recibió %q", envQueueWaitTimeout, crudo)
		}
		queueWait = d
	}

	extractTimeout := defaultExtractWait
	if crudo := os.Getenv(envExtractTimeout); crudo != "" {
		d, err := time.ParseDuration(crudo)
		if err != nil {
			return nil, fmt.Errorf("config: %s debe ser una duración válida, se recibió %q", envExtractTimeout, crudo)
		}
		extractTimeout = d
	}

	return &Config{
		Port:             puerto,
		WorkerPoolSize:   tamano,
		MaxQueue:         maxQueue,
		QueueWaitTimeout: queueWait,
		ExtractTimeout:   extractTimeout,
	}, nil
}

// validarPuerto rechaza todo valor que no sea un puerto TCP del rango válido.
func validarPuerto(puerto string) error {
	n, err := strconv.Atoi(puerto)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("config: %s debe ser un puerto entre 1 y 65535, se recibió %q", envPort, puerto)
	}
	return nil
}
