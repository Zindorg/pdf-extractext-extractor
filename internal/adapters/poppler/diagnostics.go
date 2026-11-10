package poppler

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

type runDiagnostics struct {
	ExitCode int
	Stderr   string
}

var encryptedKeywords = []string{"password", "encrypted", "drm"}

var passwordKeywords = []string{
	"user password",
	"owner password",
	"permission password",
	"password required",
}

var unsupportedKeywords = []string{
	"unimplemented",
	"unsupported",
	"type 3",
	"type3",
}

var corruptKeywords = []string{"syntax error", "damaged", "corrupted"}

// classifyReport translates a poppler process failure into a domain error[cite: 2].
func classifyReport(ctx context.Context, report runDiagnostics) error {
	if ctx.Err() != nil {
		return domain.ErrExtractionTimeout
	}

	if report.ExitCode == 0 {
		return nil
	}

	detail := strings.ToLower(report.Stderr)

	for _, kw := range passwordKeywords {
		if strings.Contains(detail, kw) {
			return domain.ErrPasswordProtected
		}
	}

	for _, kw := range encryptedKeywords {
		if strings.Contains(detail, kw) {
			return domain.ErrEncryptedDocument
		}
	}

	for _, kw := range unsupportedKeywords {
		if strings.Contains(detail, kw) {
			return domain.ErrUnsupportedFeatures
		}
	}

	for _, kw := range corruptKeywords {
		if strings.Contains(detail, kw) {
			return domain.ErrCorruptDocument
		}
	}

	return domain.ErrExtractorFailure
}

// ExtractWithTiming runs pdftotext via popplerCommand and returns raw text and elapsed time[cite: 1, 2].
func ExtractWithTiming(ctx context.Context, src domain.DocumentSource) (rawText string, elapsedMs int64, err error) {
	start := time.Now()

	input, err := src.Open()
	if err != nil {
		return "", 0, fmt.Errorf("open source: %w", err)
	}
	defer input.Close()

	cmd := popplerCommand(ctx, "pdftotext", "-", "-")

	var stdout, stderrBuf bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderrBuf
	cmd.Stdin = input

	if err := cmd.Run(); err != nil {
		elapsedMs = time.Since(start).Milliseconds()
		exitCode := -1
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}
		return "", elapsedMs, classifyReport(ctx, runDiagnostics{
			ExitCode: exitCode,
			Stderr:   stderrBuf.String(),
		})
	}

	elapsedMs = time.Since(start).Milliseconds()
	return stdout.String(), elapsedMs, nil
}
