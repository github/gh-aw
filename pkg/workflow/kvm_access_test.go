//go:build !integration

package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKVMAccessExecutesAgainstFixtures(t *testing.T) {
	helper, err := os.ReadFile("../../actions/setup/sh/kvm_access.sh")
	require.NoError(t, err)

	for _, tc := range []struct {
		name    string
		setup   string
		tools   string
		message string
	}{
		{"missing device", "", "/usr/bin/id /usr/bin/sudo /usr/bin/setfacl /usr/bin/getfacl", "is missing"},
		{"wrong device type", `touch "$device"`, "/usr/bin/id /usr/bin/sudo /usr/bin/setfacl /usr/bin/getfacl", "must be a character device"},
		{"missing ACL tool", `ln -s /dev/null "$device"`, "/usr/bin/id /usr/bin/sudo /missing/setfacl /usr/bin/getfacl", "setfacl is required"},
		{"ACL setup failure", `ln -s /dev/null "$device"`, "/usr/bin/id /usr/bin/false /usr/bin/setfacl /usr/bin/getfacl", "failed to configure scoped access"},
		{"ACL read failure", `ln -s /dev/null "$device"`, "/usr/bin/id /usr/bin/true /usr/bin/setfacl /usr/bin/false", "failed to read"},
		{"ACL entry verification failure", `ln -s /dev/null "$device"`, "/usr/bin/id /usr/bin/true /usr/bin/setfacl /usr/bin/getfacl", "failed to verify scoped ACL entry"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			device := filepath.Join(root, "kvm")
			script := filepath.Join(root, "test.sh")
			body := "set -euo pipefail\ndevice=" + device + "\n" + tc.setup + "\n" +
				strings.ReplaceAll(string(helper), "/dev/kvm", device) + "\nprepare_kvm_access " + tc.tools
			require.NoError(t, os.WriteFile(script, []byte(body), 0o600))
			output, err := exec.Command("bash", script).CombinedOutput()
			require.Error(t, err, string(output))
			assert.Contains(t, string(output), tc.message)
		})
	}
}

func TestKVMAccessGrantsScopedPermissionsOnRootOwnedDevice(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires an unprivileged runner to verify effective device permissions")
	}
	for _, tool := range []string{"sudo", "mknod", "setfacl", "getfacl"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("requires %s: %v", tool, err)
		}
	}

	root := t.TempDir()
	device := filepath.Join(root, "kvm")
	output, err := exec.Command("sudo", "-n", "mknod", "-m", "600", device, "c", "1", "3").CombinedOutput()
	if err != nil {
		t.Skipf("requires non-interactive sudo and character device creation: %s", output)
	}
	info, err := os.Stat(device)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeCharDevice)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	helper, err := os.ReadFile("../../actions/setup/sh/kvm_access.sh")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "kvm_access.sh"),
		[]byte(strings.ReplaceAll(string(helper), "/dev/kvm", device)), 0o600))
	wrapper, err := os.ReadFile("../../actions/setup/sh/cloud_hypervisor_kvm_access.sh")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "cloud_hypervisor_kvm_access.sh"), wrapper, 0o600))
	script := filepath.Join(root, "test.sh")
	body := "set -euo pipefail\n" + strings.ReplaceAll(string(helper), "/dev/kvm", device) + `
[[ ! -r ` + device + ` && ! -w ` + device + ` ]]
[[ "$(/usr/bin/stat -c %u ` + device + `)" == 0 ]]
# A successful ACL command that grants no access must still fail verification.
if (prepare_kvm_access /usr/bin/id /usr/bin/true /usr/bin/setfacl /usr/bin/getfacl); then
  exit 1
fi
prepare_kvm_access /usr/bin/id /usr/bin/sudo /usr/bin/setfacl /usr/bin/getfacl
[[ -r ` + device + ` && -w ` + device + ` ]]
[[ "$(/usr/bin/stat -c %u ` + device + `)" == 0 ]]
/usr/bin/sudo -n /usr/bin/setfacl -b ` + device + `
[[ ! -r ` + device + ` && ! -w ` + device + ` ]]
RUNNER_ENVIRONMENT=github-hosted RUNNER_OS=Linux RUNNER_ARCH=X64 ImageOS=ubuntu24 \
  bash ` + filepath.Join(root, "cloud_hypervisor_kvm_access.sh") + `
[[ -r ` + device + ` && -w ` + device + ` ]]
`
	require.NoError(t, os.WriteFile(script, []byte(body), 0o600))
	output, err = exec.Command("bash", script).CombinedOutput()
	require.NoError(t, err, string(output))
	assert.Contains(t, string(output), "failed to grant the runner user read/write access")
	assert.Contains(t, string(output), "runner user has scoped read/write access")
}
