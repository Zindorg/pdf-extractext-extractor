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

var failureCases = []struct {
	name     string
	stderr   string
	expected error
}{
	{name: "password", stderr: "Incorrect password", expected: domain.ErrEncryptedDocument},
	{name: "encrypted", stderr: "Document is encrypted", expected: domain.ErrEncryptedDocument},
	{name: "drm", stderr: "DRM restrictions in place", expected: domain.ErrEncryptedDocument},
	{name: "user-password", stderr: "Error: User password required", expected: domain.ErrPasswordProtected},
	{name: "owner-password", stderr: "Owner password needed", expected: domain.ErrPasswordProtected},
	{name: "permission-password", stderr: "Permission password incorrect", expected: domain.ErrPasswordProtected},
	{name: "password-required", stderr: "Password required to open document", expected: domain.ErrPasswordProtected},
	{name: "unimplemented", stderr: "Unimplemented feature", expected: domain.ErrUnsupportedFeatures},
	{name: "unsupported", stderr: "Unsupported filter", expected: domain.ErrUnsupportedFeatures},
	{name: "type3-font", stderr: "Unsupported Type 3 font", expected: domain.ErrUnsupportedFeatures},
	{name: "type3-lowercase", stderr: "type 3 font not supported", expected: domain.ErrUnsupportedFeatures},
	{name: "syntax-error", stderr: "Syntax Error: Invalid XRef entry", expected: domain.ErrCorruptDocument},
	{name: "damaged", stderr: "PDF damaged, trailer not found", expected: domain.ErrCorruptDocument},
	{name: "corrupted", stderr: "file is corrupted", expected: domain.ErrCorruptDocument},
	{name: "unknown-failure", stderr: "Segmentation fault", expected: domain.ErrExtractorFailure},
	{name: "random-garbage", stderr: "random garbage from the engine", expected: domain.ErrExtractorFailure},
}

func TestClassifyReport_MapsStderrToDomainError(t *testing.T) {
	for _, tc := range failureCases {
		t.Run(tc.name, func(t *testing.T) {
			err := classifyReport(context.Background(), runDiagnostics{ExitCode: 1, Stderr: tc.stderr})
			if !errors.Is(err, tc.expected) {
				t.Errorf("stderr %q -> %v; want %v", tc.stderr, err, tc.expected)
			}
		})
	}
}

func TestClassifyReport_ZeroExitCodeIgnoresWarnings(t *testing.T) {
	err := classifyReport(context.Background(), runDiagnostics{
		ExitCode: 0,
		Stderr:   "Syntax Error: a warning that does not fail the run",
	})
	if err != nil {
		t.Errorf("want nil on exit 0 with stderr warnings, got %v", err)
	}
}

func TestClassifyReport_ContextCanceledTakesPrecedence(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := classifyReport(ctx, runDiagnostics{ExitCode: 1, Stderr: "Incorrect password"})
	if !errors.Is(err, domain.ErrExtractionTimeout) {
		t.Errorf("err = %v; want ErrExtractionTimeout", err)
	}
}

func TestExtractWithTiming(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext is not installed")
	}

	ctx := context.Background()
	src := &ramBuffer{data: []byte("%PDF-1.4\n%EOF\n")}
	rawText, elapsedMs, err := ExtractWithTiming(ctx, src)
	if err != nil {
		t.Logf("ExtractWithTiming failed as expected on minimal PDF: %v", err)
	} else {
		if elapsedMs < 0 {
			t.Errorf("elapsedMs = %d; want >= 0", elapsedMs)
		}
		_ = rawText
	}
}

type ramBuffer struct {
	data []byte
}

func (r *ramBuffer) Open() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(r.data)), nil
}
