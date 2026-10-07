//go:build linux

package poppler

import (
	"context"
	"errors"
	"os/exec"
	"syscall"
)

// popplerCommand construye el mandato sobre CommandContext y lo aísla en un
// grupo de procesos propio (Setpgid): si el contexto expira o se cancela, el
// hook Cancel mata a todo el grupo con SIGKILL mediante matarGrupo, en lugar
// del kill al hijo directo que CommandContext hace por defecto. Así, cualquier
// descendiente que poppler pudiera levantar muere con él y no se filtra CPU.
func popplerCommand(ctx context.Context, programa string, args ...string) *exec.Cmd {
	mando := exec.CommandContext(ctx, programa, args...)
	mando.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	mando.Cancel = func() error { return matarGrupo(mando) }
	return mando
}

// matarGrupo mata el proceso y todos sus descendientes. La señal viaja al PGID,
// que coincide con el pid porque Setpgid convierte al hijo en líder de grupo.
// Si el grupo ya no existe (ESRCH) es porque murió solo, y eso no es un error.
func matarGrupo(mando *exec.Cmd) error {
	if mando.Process == nil {
		return nil
	}

	err := syscall.Kill(-mando.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
