//go:build !integration

package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallCopilotCLIWithoutReleaseMetadata(t *testing.T) {
	root, err := os.Getwd()
	require.NoError(t, err)
	root = filepath.Join(root, "..", "..")
	for _, test := range []struct {
		name      string
		cached    bool
		days      string
		token     string
		wantError string
		tamper    bool
		latest    bool
	}{
		{"cached-enterprise-token", true, "", "ghe-test-token", "", false, false},
		{"download-enterprise-token", false, "", "ghe-test-token", "", false, false},
		{"cached-no-token", true, "", "", "", false, false},
		{"download-no-token", false, "", "", "", false, false},
		{"legacy-cooldown-rejected", true, "3", "ghe-test-token", "Unknown Copilot installer flag: --cooldown-days", false, false},
		{"download-tampered-manifest", false, "", "ghe-test-token", "SHA256", true, false},
		{"download-latest", false, "", "", "", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			cache := filepath.Join(dir, "toolcache")
			cacheBin := filepath.Join(cache, "copilot-cli", "1.2.3", "x64", "bin")
			require.NoError(t, os.MkdirAll(cacheBin, 0o755))
			binary := []byte("#!/usr/bin/env bash\necho 'copilot 1.2.3'\n")
			if test.cached {
				require.NoError(t, os.WriteFile(filepath.Join(cacheBin, "copilot"), binary, 0o755))
			}
			var archive bytes.Buffer
			gz := gzip.NewWriter(&archive)
			tw := tar.NewWriter(gz)
			require.NoError(t, tw.WriteHeader(&tar.Header{Name: "copilot", Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg}))
			_, err := tw.Write(binary)
			require.NoError(t, err)
			require.NoError(t, tw.Close())
			require.NoError(t, gz.Close())
			archivePath := filepath.Join(dir, "archive.tar.gz")
			require.NoError(t, os.WriteFile(archivePath, archive.Bytes(), 0o644))
			manifest := []byte(fmt.Sprintf("%x  copilot-linux-x64.tar.gz\n", sha256.Sum256(archive.Bytes())))
			pin := fmt.Sprintf("%x", sha256.Sum256(manifest))
			if test.tamper {
				manifest = append(manifest, []byte("tampered\n")...)
			}
			manifestPath := filepath.Join(dir, "SHA256SUMS.txt")
			require.NoError(t, os.WriteFile(manifestPath, manifest, 0o644))
			installer, err := os.ReadFile(filepath.Join(root, "actions/setup/sh/install_copilot_cli.sh"))
			require.NoError(t, err)
			installerPath := filepath.Join(dir, "install_copilot_cli.sh")
			require.NoError(t, os.WriteFile(installerPath, installer, 0o755))
			integrity, err := os.ReadFile(filepath.Join(root, "actions/setup/sh/release_integrity.sh"))
			require.NoError(t, err)
			integrity = bytes.Replace(integrity, []byte("\nPINS"), []byte("\ngithub/copilot-cli v1.2.3 SHA256SUMS.txt "+pin+"\nPINS"), 1)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "release_integrity.sh"), integrity, 0o644))
			binDir := filepath.Join(dir, "bin")
			prepareCopilotInstallerTestBin(t, binDir)
			require.NoError(t, os.WriteFile(filepath.Join(binDir, "find"), []byte(`#!/usr/bin/env bash
case "$1" in "$RUNNER_TOOL_CACHE"/*) exec /usr/bin/find "$@" ;; *) exit 0 ;; esac
`), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(binDir, "sudo"), []byte(`#!/usr/bin/env bash
if [ "${1:-}" = -n ]; then shift; fi
if [ "${1:-}" = chown ]; then exit 0; fi
exec "$@"
`), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(binDir, "curl"), []byte(`#!/usr/bin/env bash
set -euo pipefail
output=""
url=""
authorization=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) output="$2"; shift 2 ;;
    -w) shift 2 ;;
    -H)
      case "$2" in Authorization:*) authorization="$2" ;; esac
      shift 2
      ;;
    https://*) url="$1"; shift ;;
    *) shift ;;
  esac
done
echo "$authorization $url" >> "$CURL_LOG"
case "$url" in
  https://api.github.com/repos/github/copilot-cli/releases/*)
    echo "curl: (22) The requested URL returned error: 401" >&2
    exit 22 ;;
  https://github.com/github/copilot-cli/releases/latest)
    printf '%s' "https://github.com/github/copilot-cli/releases/tag/v1.2.3" ;;
  */SHA256SUMS.txt) cp "$MOCK_MANIFEST" "$output" ;;
  */copilot-linux-x64.tar.gz) cp "$MOCK_ARCHIVE" "$output" ;;
  *) echo "unexpected URL: $url" >&2; exit 97 ;;
esac
`), 0o755))
			logPath := filepath.Join(dir, "curl.log")
			installDir := filepath.Join(dir, "installed")
			require.NoError(t, os.MkdirAll(installDir, 0o755))
			runnerTemp := filepath.Join(dir, "runner-temp")
			require.NoError(t, os.MkdirAll(runnerTemp, 0o700))
			version := "1.2.3"
			if test.latest {
				version = "latest"
			}
			args := []string{installerPath, version}
			if test.days != "" {
				args = append(args, "--cooldown-days", test.days)
			}
			if test.cached {
				args = append(args, "--checksums-sha256", pin, "--prefer-toolcache")
			}
			cmd := exec.Command("bash", args...)
			cmd.Env = append(os.Environ(),
				"HOME="+dir, "RUNNER_TOOL_CACHE="+cache,
				"RUNNER_TEMP="+runnerTemp,
				"COPILOT_INSTALL_DIR="+installDir, "GITHUB_PATH="+filepath.Join(dir, "github-path"),
				"PATH="+binDir+":"+os.Getenv("PATH"), "CURL_LOG="+logPath,
				"MOCK_MANIFEST="+manifestPath, "MOCK_ARCHIVE="+archivePath,
				"GITHUB_TOKEN="+test.token, "GH_HOST=github.com", "GITHUB_SERVER_URL=https://test.ghe.com",
			)
			output, err := cmd.CombinedOutput()
			if test.wantError != "" {
				require.Error(t, err, "%s", output)
				assert.Contains(t, string(output), test.wantError)
				assert.NoFileExists(t, filepath.Join(installDir, "copilot"), "rejected releases must not be activated")
			} else {
				require.NoError(t, err, "%s", output)
				assert.FileExists(t, filepath.Join(installDir, "copilot"))
			}
			log, _ := os.ReadFile(logPath)
			assert.NotContains(t, string(log), "api.github.com", "release metadata must not be requested at runtime")
			assert.NotContains(t, string(log), "Authorization:", "the Enterprise token must not be sent to github.com")
			if test.cached {
				assert.Empty(t, log, "an exact toolcache hit must not need network access")
			} else {
				assert.Contains(t, string(log), "/SHA256SUMS.txt", "downloads must still verify the pinned manifest")
			}
			if test.latest {
				assert.Contains(t, string(log), "https://github.com/github/copilot-cli/releases/latest")
				assert.Contains(t, string(log), "/releases/download/v1.2.3/SHA256SUMS.txt")
				assert.Contains(t, string(log), "/releases/download/v1.2.3/copilot-linux-x64.tar.gz")
				assert.NotContains(t, string(log), "/releases/latest/download")
			}
		})
	}
}
