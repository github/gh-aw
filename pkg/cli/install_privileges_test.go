//go:build !integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallerPrivilegeSelection(t *testing.T) {
	t.Parallel()
	wd, err := os.Getwd()
	require.NoError(t, err)
	script := filepath.Join(wd, "..", "..", "actions", "setup", "sh", "install_privileges.sh")

	for _, tc := range []struct {
		name, uid, rootless, sudo, wantRootless, wantSudo, wantCalls string
	}{
		{"no sudo native runner", "12345", "false", "", "true", "false", ""},
		{"sudo denied", "12345", "false", "exit 1", "true", "false", "-n true\n"},
		{"passwordless sudo", "1001", "false", `shift; "$@"`, "false", "true", "-n true\n-n printf installed\n"},
		{"root without sudo", "0", "false", "", "false", "false", ""},
		{"root with sudo", "0", "false", "exit 99", "false", "false", ""},
		{"explicit rootless overrides sudo", "1001", "true", "exit 99", "true", "false", ""},
		{"explicit rootless overrides root", "0", "true", "exit 99", "true", "false", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			// Limit PATH to the fixture: the no-sudo case cannot find host sudo.
			require.NoError(t, os.WriteFile(filepath.Join(dir, "id"), []byte("#!/bin/bash\necho "+tc.uid+"\n"), 0o755))
			if tc.sudo != "" {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "sudo"), []byte("#!/bin/bash\nprintf '%s\\n' \"$*\" >> \"$SUDO_LOG\"\n"+tc.sudo+"\n"), 0o755))
			}
			logPath := filepath.Join(dir, "sudo.log")
			cmd := exec.Command("/bin/bash", "-ec", `source "$1"; configure_install_privileges; printf '%s %s\n' "$ROOTLESS" "$INSTALL_USE_SUDO"; maybe_sudo printf installed`, "test", script)
			cmd.Env = append(os.Environ(), "PATH="+dir, "ROOTLESS="+tc.rootless, "SUDO_LOG="+logPath)
			output, err := cmd.Output()
			require.NoError(t, err)
			assert.Equal(t, tc.wantRootless+" "+tc.wantSudo+"\ninstalled", string(output))
			if tc.wantCalls == "" {
				assert.NoFileExists(t, logPath)
			} else {
				calls, err := os.ReadFile(logPath)
				require.NoError(t, err)
				assert.Equal(t, tc.wantCalls, string(calls))
			}
		})
	}
}
