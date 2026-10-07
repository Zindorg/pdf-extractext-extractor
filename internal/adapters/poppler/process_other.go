//go:build !linux

package poppler

import (
	"context"
	"os/exec"
)

// popplerCommand es el equivalente no-linux: sin grupo de procesos propio, así
// que la cancelación del contexto mata al hijo directo (el comportamiento por
// defecto de CommandContext). Los builds de producción son linux (§7).
func popplerCommand(ctx context.Context, programa string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, programa, args...)
}
