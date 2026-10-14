package poppler

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"

	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

const (
	FormFeed  = "\f"
	PageBreak = "\n\n"
)

// Inspector implementa domain.TextExtractor sobre el motor poppler.
type Inspector struct{}

func New() *Inspector {
	return &Inspector{}
}

func (i *Inspector) Extract(ctx context.Context, src domain.DocumentSource) (*domain.ExtractionResult, error) {
	rawText, err := extractWithPdftotext(ctx, src)
	if err != nil {
		return nil, err
	}

	// Contar páginas: cada \f = salto de página en pdftotext
	pages := strings.Count(rawText, FormFeed)
	if pages == 0 && len(rawText) > 0 {
		pages = 1 // fallback: PDF de una página sin \f final
	}

	// Normalizar saltos de página a párrafos
	text := strings.TrimSpace(strings.ReplaceAll(rawText, FormFeed, PageBreak))
	if text == "" {
		return nil, domain.ErrNoTextLayer
	}

	return domain.NewExtractionResult(text, pages), nil
}

// extractWithPdftotext ejecuta pdftotext y devuelve el texto crudo (con \f).
func extractWithPdftotext(ctx context.Context, src domain.DocumentSource) (string, error) {
	entrada, err := src.Open()
	if err != nil {
		return "", fmt.Errorf("open source: %w", err)
	}
	defer entrada.Close()

	cmd := exec.CommandContext(ctx, "pdftotext", "-", "-")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return matarGrupo(cmd) }

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Stdin = entrada

	if err := cmd.Run(); err != nil {
		exitCode := 0
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}
		return "", classifyReport(ctx, runDiagnostics{ExitCode: exitCode, Stderr: stderr.String()})
	}

	return stdout.String(), nil
}

var _ domain.TextExtractor = (*Inspector)(nil)