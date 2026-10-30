//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package cli

import "os/exec"

func configureOrgRolloutProcess(cmd *exec.Cmd) {
	// CommandContext terminates the worker on platforms without Unix process groups.
}
