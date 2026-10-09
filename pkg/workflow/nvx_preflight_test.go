//go:build !integration

package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type nvxFixtureArtifact struct {
	File      string `json:"file"`
	SizeBytes int    `json:"sizeBytes"`
	SHA256    string `json:"sha256"`
}

func nvxPreflightFixtureManifest(artifacts map[string][]byte) string {
	manifestArtifacts := make(map[string]nvxFixtureArtifact, len(artifacts))
	for role, content := range artifacts {
		digest := sha256.Sum256(content)
		manifestArtifacts[role] = nvxFixtureArtifact{
			File:      map[string]string{"openvmm": "openvmm", "kernel": "vmlinux", "initramfs": "initramfs.cpio.gz"}[role],
			SizeBytes: len(content),
			SHA256:    hex.EncodeToString(digest[:]),
		}
	}

	manifest := map[string]any{
		"schemaVersion": 2,
		"architecture":  "x86_64",
		"release": map[string]string{
			"repository":   "github/gh-aw-firewall",
			"sourceCommit": strings.Repeat("a", 40),
			"tag":          "v0.28.49",
			"workflow":     defaultNVXSignerWorkflow,
		},
		"upstream": map[string]string{
			"nvxCommit":     "be859aa77ffa7a20f9ef50f68c5386acdfca9955",
			"openvmmCommit": "762bc1c7a203b16aee752324d6a4ab0bde1a713a",
			"releaseTag":    "v0.1.0-dev.be859aa77ffa",
		},
		"artifacts": manifestArtifacts,
	}
	encoded, _ := json.Marshal(manifest)
	return string(encoded)
}

func setupNVXPreflightFixture(t *testing.T, manifest string) (string, []string) {
	t.Helper()

	root := t.TempDir()
	t.Cleanup(func() {
		matches, _ := filepath.Glob(filepath.Join(root, "tmp", "gh-aw-nvx.*"))
		for _, match := range matches {
			_ = os.Chmod(match, 0o755)
			_ = os.RemoveAll(match)
		}
	})
	binDir := filepath.Join(root, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "dev"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sys", "fs", "cgroup"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "proc", "sys", "kernel", "seccomp"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp"), 0o755))

	artifactsDir := filepath.Join(root, "artifacts")
	require.NoError(t, os.MkdirAll(artifactsDir, 0o755))
	artifactFiles := map[string][]byte{
		"openvmm":           []byte("fixture-openvmm"),
		"vmlinux":           []byte("fixture-kernel"),
		"initramfs.cpio.gz": []byte("fixture-initramfs"),
	}
	for name, content := range artifactFiles {
		require.NoError(t, os.WriteFile(filepath.Join(artifactsDir, name), content, 0o644))
	}
	require.NoError(t, os.WriteFile(filepath.Join(artifactsDir, "nvx-test-x86_64.manifest.json"), []byte(manifest), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(artifactsDir, "nvx-test-x86_64.manifest.sigstore.jsonl"), []byte("{}\n"), 0o644))
	layerDir := filepath.Join(root, "guest-layer")
	require.NoError(t, os.MkdirAll(layerDir, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(root, "dev", "kvm"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "sys", "fs", "cgroup", "cgroup.controllers"), []byte("cpu memory pids\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "proc", "sys", "kernel", "seccomp", "actions_avail"), []byte("kill_process trap allow\n"), 0o644))

	for name := range strings.FieldsSeq("bwrap flock getfacl getent gh groupdel id ip iptables mkfs.erofs mke2fs nft setfacl setpriv sysctl useradd userdel chmod install jq mktemp realpath rm sha256sum stat sudo uname") {
		path := filepath.Join(binDir, name)
		body := "#!/bin/bash\nexec /usr/bin/" + name + ` "$@"` + "\n"
		switch name {
		case "bwrap", "flock", "getent", "groupdel", "ip", "iptables", "mkfs.erofs", "mke2fs", "nft", "setpriv", "sysctl", "useradd", "userdel":
			body = "#!/bin/bash\nexit 0\n"
		case "setfacl":
			body = `#!/bin/bash
[[ "${NVX_TEST_ACL_FAIL:-}" != 1 ]]
`
		case "getfacl":
			body = "#!/bin/bash\necho user:0:rw-\n"
		case "gh":
			body = `#!/bin/bash
set -e
[[ "$1" == attestation && "$2" == verify ]]
manifest="$3"
bundle=""
while (($#)); do
  if [[ "$1" == --bundle ]]; then shift; bundle="$1"; fi
  shift
done
[[ "$manifest" == "$NVX_TEST_TMP"/gh-aw-nvx.*/manifest.json ]]
[[ "$bundle" == "$NVX_TEST_TMP"/gh-aw-nvx.*/manifest.sigstore.jsonl ]]
stage_dir="${manifest%/*}"
[[ "$(stat -c '%a' "$stage_dir")" == 555 ]]
for file in openvmm vmlinux initramfs.cpio.gz manifest.json manifest.sigstore.jsonl; do
  [[ -s "$stage_dir/$file" ]]
done
printf 'verified:%s\n' "$manifest" >> "$NVX_TEST_LOG"
[[ "${NVX_TEST_ATTESTATION_FAIL:-}" != 1 ]]
`
		case "id":
			body = `#!/bin/bash
if [[ "$1" == -u ]]; then echo 0; else exec /usr/bin/id "$@"; fi
`
		case "install":
			body = `#!/bin/bash
printf 'install:%s\n' "$*" >> "$NVX_TEST_LOG"
args=()
while (($#)); do
  case "$1" in
    -o|-g) shift 2 ;;
    *) args+=("$1"); shift ;;
  esac
done
exec /usr/bin/install "${args[@]}"
`
		case "mktemp":
			body = `#!/bin/bash
if [[ "$1" == -d && "$2" == /tmp/gh-aw-nvx.XXXXXXXXXX ]]; then
  exec /usr/bin/mktemp -d "$NVX_TEST_TMP/gh-aw-nvx.XXXXXXXXXX"
fi
exec /usr/bin/mktemp "$@"
`
		case "realpath":
			body = "#!/bin/bash\nexec /usr/bin/realpath \"$@\"\n"
		case "rm":
			body = "#!/bin/bash\nexec /usr/bin/rm \"$@\"\n"
		case "sudo":
			body = `#!/bin/bash
[[ "$1" == -n ]] && shift
[[ "${NVX_TEST_SUDO_FAIL:-}" != 1 ]] || exit 1
if [[ "${1##*/}" == rm ]]; then
  stage_dir="${@: -1}"
  /usr/bin/chmod 755 "$stage_dir"
fi
exec "$@"
`
		case "stat":
			body = `#!/bin/bash
format="$2"
path="${@: -1}"
case "$format" in
  %u) [[ "$path" == "$NVX_TEST_BIN"/* ]] && echo 0 || exec /usr/bin/stat "$@" ;;
  %a) [[ "$path" == "$NVX_TEST_BIN"/* ]] && echo 755 || exec /usr/bin/stat "$@" ;;
  %u:%a)
    case "${path##*/}" in
      openvmm) echo 0:555 ;;
      gh-aw-nvx.*) echo 0:555 ;;
      *) echo 0:444 ;;
    esac
    ;;
  *) exec /usr/bin/stat "$@" ;;
esac
`
		case "uname":
			body = `#!/bin/bash
case "$1" in
  -s) echo Linux ;;
  -m) echo x86_64 ;;
  *) exit 1 ;;
esac
`
		}
		require.NoError(t, os.WriteFile(path, []byte(body), 0o755))
	}

	scriptPath, err := filepath.Abs(filepath.Join("..", "..", "actions", "setup", "sh", "nvx_host_preflight.sh"))
	require.NoError(t, err)
	script, err := os.ReadFile(scriptPath)
	require.NoError(t, err)
	fixtureScript := strings.Replace(string(script), "trusted_dirs=(/usr/sbin /usr/bin /sbin /bin)", "trusted_dirs=("+binDir+")", 1)
	fixtureScript = strings.ReplaceAll(fixtureScript, "/usr/bin/realpath", filepath.Join(binDir, "realpath"))
	fixtureScript = strings.ReplaceAll(fixtureScript, "/usr/bin/stat", filepath.Join(binDir, "stat"))
	fixtureKVM := filepath.Join(root, "dev", "kvm")
	kvmScript, err := os.ReadFile(filepath.Join(filepath.Dir(scriptPath), "kvm_access.sh"))
	require.NoError(t, err)
	fixtureKVMScript := strings.ReplaceAll(string(kvmScript), "/dev/kvm", fixtureKVM)
	fixtureKVMScript = strings.Replace(fixtureKVMScript, "[[ ! -c "+fixtureKVM, "[[ ! -f "+fixtureKVM, 1)
	require.NoError(t, os.WriteFile(filepath.Join(root, "kvm_access.sh"), []byte(fixtureKVMScript), 0o600))
	fixtureScript = strings.ReplaceAll(fixtureScript, "/sys/fs/cgroup/cgroup.controllers", filepath.Join(root, "sys", "fs", "cgroup", "cgroup.controllers"))
	fixtureScript = strings.ReplaceAll(fixtureScript, "/proc/sys/kernel/seccomp/actions_avail", filepath.Join(root, "proc", "sys", "kernel", "seccomp", "actions_avail"))
	fixtureScriptPath := filepath.Join(root, "nvx_host_preflight.sh")
	require.NoError(t, os.WriteFile(fixtureScriptPath, []byte(fixtureScript), 0o600))

	env := []string{
		"GH_AW_AWF_VERSION=v0.28.49",
		"GH_AW_NVX_LAYER_SOURCE=" + layerDir,
		"GH_AW_NVX_OPENVMM_SOURCE=" + filepath.Join(artifactsDir, "openvmm"),
		"GH_AW_NVX_KERNEL_SOURCE=" + filepath.Join(artifactsDir, "vmlinux"),
		"GH_AW_NVX_INITRAMFS_SOURCE=" + filepath.Join(artifactsDir, "initramfs.cpio.gz"),
		"GH_AW_NVX_ARTIFACT_MANIFEST_SOURCE=" + filepath.Join(artifactsDir, "nvx-test-x86_64.manifest.json"),
		"GH_AW_NVX_ARTIFACT_MANIFEST_BUNDLE_SOURCE=" + filepath.Join(artifactsDir, "nvx-test-x86_64.manifest.sigstore.jsonl"),
		"GH_AW_NVX_SIGNER_WORKFLOW=" + defaultNVXSignerWorkflow,
		"GH_AW_NVX_MOUNT_POLICY=workspace-only",
		"GITHUB_ENV=" + filepath.Join(root, "github_env"),
		"NVX_TEST_BIN=" + binDir,
		"NVX_TEST_TMP=" + filepath.Join(root, "tmp"),
		"NVX_TEST_LOG=" + filepath.Join(root, "events"),
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
	}
	return fixtureScriptPath, env
}

func runNVXPreflightFixture(t *testing.T, scriptPath string, env []string) (string, error) {
	t.Helper()
	cmd := exec.Command("bash", scriptPath)
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func TestNVXHostPreflightExecutesAgainstFixtures(t *testing.T) {
	artifactBytes := map[string][]byte{
		"openvmm":   []byte("fixture-openvmm"),
		"kernel":    []byte("fixture-kernel"),
		"initramfs": []byte("fixture-initramfs"),
	}
	manifest := nvxPreflightFixtureManifest(artifactBytes)

	t.Run("verified files are staged before attestation", func(t *testing.T) {
		scriptPath, env := setupNVXPreflightFixture(t, manifest)
		output, err := runNVXPreflightFixture(t, scriptPath, env)
		require.NoError(t, err, output)
		assert.Contains(t, output, "NVX host and attested artifacts validated")

		events, err := os.ReadFile(filepath.Join(filepath.Dir(scriptPath), "events"))
		require.NoError(t, err)
		lines := strings.Split(strings.TrimSpace(string(events)), "\n")
		require.NotEmpty(t, lines)
		assert.True(t, strings.HasPrefix(lines[0], "install:"))
		assert.Contains(t, lines[len(lines)-1], "verified:")

		githubEnv, err := os.ReadFile(filepath.Join(filepath.Dir(scriptPath), "github_env"))
		require.NoError(t, err)
		assert.Contains(t, string(githubEnv), "GH_AW_NVX_STAGE_DIR=")
		assert.Contains(t, string(githubEnv), "GH_AW_NVX_RM=")
	})

	t.Run("missing host controller fails before staging", func(t *testing.T) {
		scriptPath, env := setupNVXPreflightFixture(t, manifest)
		hostControllerFile := filepath.Join(filepath.Dir(scriptPath), "sys", "fs", "cgroup", "cgroup.controllers")
		require.NoError(t, os.WriteFile(hostControllerFile, []byte("cpu memory\n"), 0o644))
		output, err := runNVXPreflightFixture(t, scriptPath, env)
		require.Error(t, err)
		assert.Contains(t, output, "requires the cgroup v2 pids controller")
		_, statErr := os.Stat(filepath.Join(filepath.Dir(scriptPath), "events"))
		assert.ErrorIs(t, statErr, os.ErrNotExist)
	})

	for _, tc := range []struct {
		name      string
		directory bool
		message   string
	}{
		{"missing KVM device", false, "is missing"},
		{"wrong KVM device type", true, "must be a character device"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scriptPath, env := setupNVXPreflightFixture(t, manifest)
			device := filepath.Join(filepath.Dir(scriptPath), "dev", "kvm")
			require.NoError(t, os.Remove(device))
			if tc.directory {
				require.NoError(t, os.Mkdir(device, 0o755))
			}
			output, err := runNVXPreflightFixture(t, scriptPath, env)
			require.Error(t, err)
			assert.Contains(t, output, tc.message)
			matches, globErr := filepath.Glob(filepath.Join(filepath.Dir(scriptPath), "tmp", "gh-aw-nvx.*"))
			require.NoError(t, globErr)
			assert.Empty(t, matches)
		})
	}

	for _, tc := range []struct {
		name    string
		env     string
		message string
	}{
		{"ACL setup failure", "NVX_TEST_ACL_FAIL=1", "failed to configure scoped access"},
		{"non-interactive sudo failure", "NVX_TEST_SUDO_FAIL=1", "requires non-interactive sudo"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scriptPath, env := setupNVXPreflightFixture(t, manifest)
			output, err := runNVXPreflightFixture(t, scriptPath, append(env, tc.env))
			require.Error(t, err)
			assert.Contains(t, output, tc.message)
			matches, globErr := filepath.Glob(filepath.Join(filepath.Dir(scriptPath), "tmp", "gh-aw-nvx.*"))
			require.NoError(t, globErr)
			assert.Empty(t, matches)
		})
	}

	t.Run("attestation failure removes incomplete staging", func(t *testing.T) {
		scriptPath, env := setupNVXPreflightFixture(t, manifest)
		env = append(env, "NVX_TEST_ATTESTATION_FAIL=1")
		output, err := runNVXPreflightFixture(t, scriptPath, env)
		require.Error(t, err)
		assert.Contains(t, output, "offline attestation verification failed")
		matches, globErr := filepath.Glob(filepath.Join(filepath.Dir(scriptPath), "tmp", "gh-aw-nvx.*"))
		require.NoError(t, globErr)
		assert.Empty(t, matches)
	})

	t.Run("manifest contract mismatch fails closed", func(t *testing.T) {
		badManifest := strings.Replace(manifest, `"tag":"v0.28.49"`, `"tag":"v0.28.48"`, 1)
		scriptPath, env := setupNVXPreflightFixture(t, badManifest)
		output, err := runNVXPreflightFixture(t, scriptPath, env)
		require.Error(t, err)
		assert.Contains(t, output, "does not match the trusted AWF release")
	})

	t.Run("artifact digest mismatch fails closed", func(t *testing.T) {
		scriptPath, env := setupNVXPreflightFixture(t, manifest)
		require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(scriptPath), "artifacts", "openvmm"), []byte("tampered"), 0o644))
		output, err := runNVXPreflightFixture(t, scriptPath, env)
		require.Error(t, err)
		assert.Contains(t, output, "openvmm artifact size or SHA-256 does not match")
	})
}
