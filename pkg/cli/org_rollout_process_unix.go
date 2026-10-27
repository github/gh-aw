//go:build unix

package cli

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func configureOrgRolloutProcess(cmd *exec.Cmd) {
	// Terminate git/gh descendants too, not just the worker orchestrating them.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}
