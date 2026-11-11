//go:build js || wasm

package workflow

func validateDockerImageWithMode(image string, verbose bool, requireDocker bool, readOnly bool) error {
	return nil
}

func isDockerDaemonRunning() bool {
	return false
}

func markDockerDaemonUnavailable() {}
