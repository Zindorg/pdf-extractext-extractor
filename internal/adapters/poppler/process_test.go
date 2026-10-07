//go:build linux

package poppler

import (
	"context"
	"errors"
	"io"
	"os"
	"syscall"
	"testing"
	"time"
)

// TestProcess_GroupKilledOnContextExpiry demuestra que al expirar el contexto
// muere el proceso de poppler *y su grupo*: si solo se matara al padre (el
// comportamiento por defecto de CommandContext, sin el hook Cancel), el
// `sleep` lanzado por la shell sobreviviría y la comprobación ESRCH fallaría.
func TestProcess_GroupKilledOnContextExpiry(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("/bin/sh no está disponible en este entorno")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	mando := popplerCommand(ctx, "/bin/sh", "-c", "sleep 60 & wait")
	mando.Stdout = io.Discard
	mando.Stderr = io.Discard

	if err := mando.Start(); err != nil {
		t.Fatalf("arrancar la shell falló: %v", err)
	}

	terminado := make(chan error, 1)
	go func() { terminado <- mando.Wait() }()

	select {
	case err := <-terminado:
		if err == nil {
			t.Fatal("esperaba un error al expirar el contexto; Wait devolvió nil")
		}
		// Cmd.Wait devuelve un *ExitError ("signal: killed") cuando el proceso
		// muere por señal: solo sustituye err por ctx.Err() cuando no hay error
		// de salida. Lo que de verdad prueba el motivo es el estado del proceso.
		if mando.ProcessState == nil || mando.ProcessState.Success() {
			t.Fatalf("ProcessState = %v; esperaba un proceso muerto por señal", mando.ProcessState)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("el proceso no terminó tras expirar el contexto")
	}

	for i := 0; ; i++ {
		err := syscall.Kill(-mando.Process.Pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		if i == 40 {
			t.Fatalf("el grupo sigue vivo: Kill(-%d, 0) = %v; un descendiente no murió", mando.Process.Pid, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
