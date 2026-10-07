package config

import (
	"testing"
)

// Sin variables en el entorno, LoadFromEnv devuelve los defectos de §9:
// HTTP_PORT=8081 y MAX_IN_FLIGHT=0 (que el composition root convierte en
// runtime.NumCPU()).
func TestLoadFromEnv_UsaLosDefectosDeLaSeccion9(t *testing.T) {
	t.Setenv("HTTP_PORT", "")
	t.Setenv("MAX_IN_FLIGHT", "")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() con el entorno vacío falló: %v", err)
	}
	if cfg.Port != "8081" {
		t.Errorf("Port = %q; se esperaba el defecto 8081", cfg.Port)
	}
	if cfg.WorkerPoolSize != 0 {
		t.Errorf("WorkerPoolSize = %d; se esperaba 0 (NumCPU)", cfg.WorkerPoolSize)
	}
}

func TestLoadFromEnv_LeeLasVariables(t *testing.T) {
	t.Setenv("HTTP_PORT", "9090")
	t.Setenv("MAX_IN_FLIGHT", "4")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() con valores válidos falló: %v", err)
	}
	if cfg.Port != "9090" {
		t.Errorf("Port = %q; se esperaba 9090", cfg.Port)
	}
	if cfg.WorkerPoolSize != 4 {
		t.Errorf("WorkerPoolSize = %d; se esperaba 4", cfg.WorkerPoolSize)
	}
}

// Un pool negativo no tiene sentido y debe impedir arrancar (§9).
func TestLoadFromEnv_RechazaUnPoolNegativo(t *testing.T) {
	t.Setenv("HTTP_PORT", "")
	t.Setenv("MAX_IN_FLIGHT", "-1")

	if _, err := LoadFromEnv(); err == nil {
		t.Error("LoadFromEnv() aceptó un pool negativo; se esperaba un error")
	}
}

// Un puerto que no es un número válido del rango [1,65535] impide arrancar (§9).
func TestLoadFromEnv_RechazaUnPuertoInvalido(t *testing.T) {
	for _, puerto := range []string{"abc", "70000"} {
		t.Run(puerto, func(t *testing.T) {
			t.Setenv("HTTP_PORT", puerto)
			t.Setenv("MAX_IN_FLIGHT", "")

			if _, err := LoadFromEnv(); err == nil {
				t.Errorf("LoadFromEnv() aceptó el puerto %q; se esperaba un error", puerto)
			}
		})
	}
}
