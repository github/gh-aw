//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

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

func configureMaintenanceCheckoutProcess(cmd *exec.Cmd, rolloutWorker bool) {
	if rolloutWorker {
		return
	}
	configureOrgRolloutProcess(cmd)
	// A separate session has no controlling terminal, so tools cannot block
	// waiting for terminal credentials when stdin is disconnected.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
