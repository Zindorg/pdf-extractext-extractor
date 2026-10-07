package poppler

import (
	"context"
	"errors"
	"testing"

	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

// fallos is a table of stderr fingerprints injected into classifyReport,
// without spawning real processes.
var fallos = []struct {
	name     string
	stderr   string
	esperado error
}{
	{name: "password", stderr: "Incorrect password", esperado: domain.ErrEncryptedDocument},
	{name: "encrypted", stderr: "Document is encrypted", esperado: domain.ErrEncryptedDocument},
	{name: "drm", stderr: "DRM restrictions in place", esperado: domain.ErrEncryptedDocument},
	{name: "syntax-error", stderr: "Syntax Error: Invalid XRef entry", esperado: domain.ErrCorruptDocument},
	{name: "damaged", stderr: "PDF damaged, trailer not found", esperado: domain.ErrCorruptDocument},
	{name: "corrupted", stderr: "file is corrupted", esperado: domain.ErrCorruptDocument},
	{name: "desconocido", stderr: "random garbage from the engine", esperado: domain.ErrCorruptDocument},
}

func TestClassifyReport_StderrTraduceAlMotivoDeDominio(t *testing.T) {
	for _, caso := range fallos {
		t.Run(caso.name, func(t *testing.T) {
			err := classifyReport(context.Background(), runDiagnostics{ExitCode: 1, Stderr: caso.stderr})
			if !errors.Is(err, caso.esperado) {
				t.Errorf("stderr %q -> %v; esperaba %v", caso.stderr, err, caso.esperado)
			}
		})
	}
}

func TestClassifyReport_AvisosConSalidaNulaNoSonErrores(t *testing.T) {
	err := classifyReport(context.Background(), runDiagnostics{
		ExitCode: 0,
		Stderr:   "Syntax Error: a warning that does not fail the run",
	})
	if err != nil {
		t.Errorf("esperaba nil con exit 0 y avisos, recibí %v", err)
	}
}

func TestClassifyReport_ContextoCanceladoGanaSobreLasClaves(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := classifyReport(ctx, runDiagnostics{ExitCode: 1, Stderr: "Incorrect password"})
	if !errors.Is(err, domain.ErrExtractionTimeout) {
		t.Errorf("err = %v; esperaba ErrExtractionTimeout aunque el stderr hable de cifrado", err)
	}
}
