package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config agrupa la configuración de arranque (§9). Todo llega por variables de
// entorno y se valida en la frontera: un valor inválido impide arrancar.
type Config struct {
	Port           string
	WorkerPoolSize int
}

// Nombres de las variables de entorno y defectos que fija §9.
const (
	envPort         = "HTTP_PORT"
	envMaxInFlight  = "MAX_IN_FLIGHT"
	defaultPort     = "8081"
	defaultPoolSize = 0 // 0 significa runtime.NumCPU(), que resuelve el main
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

	return &Config{Port: puerto, WorkerPoolSize: tamano}, nil
}

// validarPuerto rechaza todo valor que no sea un puerto TCP del rango válido.
func validarPuerto(puerto string) error {
	n, err := strconv.Atoi(puerto)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("config: %s debe ser un puerto entre 1 y 65535, se recibió %q", envPort, puerto)
	}
	return nil
}
