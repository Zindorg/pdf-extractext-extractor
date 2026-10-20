package poppler

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

// runDiagnostics condensa lo que dejó un proceso de poppler que terminó con
// error: su código de salida y el stderr completo.
type runDiagnostics struct {
	ExitCode int
	Stderr   string
}

// clavesCifrado: PDF cifrado/DRM genérico → reason: "encrypted_pdf"
var clavesCifrado = []string{"password", "encrypted", "drm"}

// clavesPassword: solicita contraseña de usuario/propietario → reason: "password_protected"
var clavesPassword = []string{
	"user password",
	"owner password",
	"permission password",
	"password required",
}

// clavesUnsupported: características no soportadas → reason: "unsupported_features"
var clavesUnsupported = []string{
	"unimplemented",
	"unsupported",
	"type 3",
	"type3",
}

// clavesCorrupcion: PDF dañado → reason: "corrupt"
var clavesCorrupcion = []string{"syntax error", "damaged", "corrupted"}

// classifyReport traduce un fallo de poppler a un motivo del dominio, en este
// orden de precedencia:
//
//  1. contexto cancelado/vencido → ErrExtractionTimeout
//  2. exit code 0 → éxito (nil); inspector manejará empty
//  3. password de usuario/propietario → ErrPasswordProtected
//  4. cifrado/DRM genérico → ErrEncryptedDocument
//  5. características no soportadas → ErrUnsupportedFeatures
//  6. corrupción → ErrCorruptDocument
//  7. cualquier otro fallo no clasificado → ErrExtractorFailure
//
// Solo se clasifican fallos con código de salida no nulo: poppler escribe
// avisos de sintaxis en el stderr incluso cuando el resultado es utilizable,
// y esos no invalidan la extracción.
func classifyReport(ctx context.Context, reporte runDiagnostics) error {
	if ctx.Err() != nil {
		return domain.ErrExtractionTimeout
	}

	if reporte.ExitCode == 0 {
		return nil
	}

	detalle := strings.ToLower(reporte.Stderr)

	// 1. password_protected (más específico que encrypted genérico)
	for _, clave := range clavesPassword {
		if strings.Contains(detalle, clave) {
			return domain.ErrPasswordProtected
		}
	}

	// 2. encrypted_pdf
	for _, clave := range clavesCifrado {
		if strings.Contains(detalle, clave) {
			return domain.ErrEncryptedDocument
		}
	}

	// 3. unsupported_features
	for _, clave := range clavesUnsupported {
		if strings.Contains(detalle, clave) {
			return domain.ErrUnsupportedFeatures
		}
	}

	// 4. corrupt
	for _, clave := range clavesCorrupcion {
		if strings.Contains(detalle, clave) {
			return domain.ErrCorruptDocument
		}
	}

	// 5. unknown → extractor failure (503 en handler)
	return domain.ErrExtractorFailure
}

// ExtractWithTiming ejecuta pdftotext midiendo el tiempo de extracción puro
// y devolviendo el texto crudo junto con el stderr para clasificación posterior.
func ExtractWithTiming(ctx context.Context, src domain.DocumentSource) (rawText string, elapsedMs int64, stderr string, err error) {
	start := time.Now()

	entrada, err := src.Open()
	if err != nil {
		return "", 0, "", fmt.Errorf("open source: %w", err)
	}
	defer entrada.Close()

	cmd := exec.CommandContext(ctx, "pdftotext", "-", "-")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return matarGrupo(cmd) }

	var stdout, stderrBuf bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderrBuf
	cmd.Stdin = entrada

	if err := cmd.Run(); err != nil {
		elapsedMs = time.Since(start).Milliseconds()
		exitCode := 0
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}
		return "", elapsedMs, stderrBuf.String(), classifyReport(ctx, runDiagnostics{ExitCode: exitCode, Stderr: stderrBuf.String()})
	}

	elapsedMs = time.Since(start).Milliseconds()
	return stdout.String(), elapsedMs, stderrBuf.String(), nil
}
