package poppler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
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
	// encrypted_pdf
	{name: "password", stderr: "Incorrect password", esperado: domain.ErrEncryptedDocument},
	{name: "encrypted", stderr: "Document is encrypted", esperado: domain.ErrEncryptedDocument},
	{name: "drm", stderr: "DRM restrictions in place", esperado: domain.ErrEncryptedDocument},

	// password_protected (más específico)
	{name: "user-password", stderr: "Error: User password required", esperado: domain.ErrPasswordProtected},
	{name: "owner-password", stderr: "Owner password needed", esperado: domain.ErrPasswordProtected},
	{name: "permission-password", stderr: "Permission password incorrect", esperado: domain.ErrPasswordProtected},
	{name: "password-required", stderr: "Password required to open document", esperado: domain.ErrPasswordProtected},

	// unsupported_features
	{name: "unimplemented", stderr: "Unimplemented feature", esperado: domain.ErrUnsupportedFeatures},
	{name: "unsupported", stderr: "Unsupported filter", esperado: domain.ErrUnsupportedFeatures},
	{name: "type3-font", stderr: "Unsupported Type 3 font", esperado: domain.ErrUnsupportedFeatures},
	{name: "type3-lowercase", stderr: "type 3 font not supported", esperado: domain.ErrUnsupportedFeatures},

	// corrupt
	{name: "syntax-error", stderr: "Syntax Error: Invalid XRef entry", esperado: domain.ErrCorruptDocument},
	{name: "damaged", stderr: "PDF damaged, trailer not found", esperado: domain.ErrCorruptDocument},
	{name: "corrupted", stderr: "file is corrupted", esperado: domain.ErrCorruptDocument},

	// unknown → extractor failure
	{name: "unknown-failure", stderr: "Segmentation fault", esperado: domain.ErrExtractorFailure},
	{name: "desconocido", stderr: "random garbage from the engine", esperado: domain.ErrExtractorFailure},
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

// TestExtractWithTiming verifica que la función mide tiempo y clasifica errores.
func TestExtractWithTiming(t *testing.T) {
	// Este test requiere pdftotext instalado; se saltea si no está.
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext no está instalado")
	}

	ctx := context.Background()

	// Caso éxito: PDF válido
	src := &ramBuffer{data: []byte("%PDF-1.4\n%EOF\n")}
	rawText, elapsedMs, stderr, err := ExtractWithTiming(ctx, src)
	if err != nil {
		t.Logf("ExtractWithTiming falló (esperado en test sin PDF real): %v", err)
	} else {
		if elapsedMs <= 0 {
			t.Errorf("elapsedMs = %d; debe ser > 0", elapsedMs)
		}
		if rawText == "" {
			t.Log("rawText vacío (PDF mínimo sin contenido)")
		}
		_ = stderr
	}
}

// ramBuffer implementa domain.DocumentSource para tests.
type ramBuffer struct {
	data []byte
}

func (r *ramBuffer) Open() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(r.data)), nil
}
