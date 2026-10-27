//go:build !unix

package cli

import "os/exec"

func configureOrgRolloutProcess(cmd *exec.Cmd) {
	// CommandContext terminates the worker on platforms without Unix process groups.
}
