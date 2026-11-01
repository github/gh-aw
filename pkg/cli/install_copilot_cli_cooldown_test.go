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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallCopilotCLIReleaseAgeRequiresJQ(t *testing.T) {
	root, err := os.Getwd()
	require.NoError(t, err)
	installer := filepath.Join(root, "..", "..", "actions", "setup", "sh", "install_copilot_cli.sh")
	bash, err := exec.LookPath("bash")
	require.NoError(t, err)

	for _, version := range []string{"1.2.3", "latest"} {
		t.Run(version, func(t *testing.T) {
			dir := t.TempDir()
			binDir := filepath.Join(dir, "bin")
			prepareCopilotInstallerTestBin(t, binDir)
			// Restrict PATH to installer prerequisites, deliberately excluding jq.
			for _, name := range []string{"bash", "dirname", "mkdir", "id", "mktemp", "rm", "chown"} {
				path, err := exec.LookPath(name)
				require.NoError(t, err)
				require.NoError(t, os.Symlink(path, filepath.Join(binDir, name)))
			}
			logPath := filepath.Join(dir, "curl.log")
			require.NoError(t, os.WriteFile(filepath.Join(binDir, "curl"), []byte(`#!/usr/bin/env bash
echo called >> "$CURL_LOG"
exit 97
`), 0o755))
			cmd := exec.Command(bash, installer, version, "--rootless", "--cooldown-days", "3")
			cmd.Env = append(os.Environ(), "HOME="+dir, "PATH="+binDir,
				"TMPDIR="+dir, "CURL_LOG="+logPath, "GITHUB_TOKEN=test-token")
			output, err := cmd.CombinedOutput()
			require.Error(t, err, "%s", output)
			assert.Contains(t, string(output), "jq is required for Copilot release-age validation")
			assert.Contains(t, string(output), "Install jq from https://jqlang.github.io/jq/download/")
			assert.NotContains(t, string(output), "release metadata is invalid")
			assert.NoFileExists(t, logPath, "missing jq must fail before fetching metadata")
			assert.NoFileExists(t, filepath.Join(dir, ".local", "bin", "copilot"))
		})
	}
}

func TestInstallCopilotCLIReleaseAgeCooldown(t *testing.T) {
	root, err := os.Getwd()
	require.NoError(t, err)
	root = filepath.Join(root, "..", "..")
	recent := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)
	fourDays := time.Now().UTC().Add(-4 * 24 * time.Hour).Format(time.RFC3339)
	release := func(published string) string {
		return fmt.Sprintf(`{"draft":false,"tag_name":"v1.2.3","published_at":%q}`, published)
	}
	for _, test := range []struct {
		name      string
		cached    bool
		days      string
		version   string
		metadata  string
		wantError string
		tamper    bool
	}{
		{"cached-old", true, "3", "1.2.3", release("2000-01-01T00:00:00Z"), "", false},
		{"cached-recent", true, "3", "1.2.3", release(recent), "at least 3 days old", false},
		{"cached-longer-cooldown", true, "7", "1.2.3", release(fourDays), "at least 7 days old", false},
		{"cached-missing-time", true, "3", "1.2.3", `{"draft":false,"tag_name":"v1.2.3"}`, "metadata is invalid", false},
		{"cached-invalid-time", true, "3", "1.2.3", release("not-a-date"), "metadata is invalid", false},
		{"cached-wrong-tag", true, "3", "1.2.3", `{"draft":false,"tag_name":"v9.9.9","published_at":"2000-01-01T00:00:00Z"}`, "metadata is invalid", false},
		{"cached-unavailable", true, "3", "1.2.3", "", "metadata is unavailable", false},
		{"cached-disabled", true, "0", "1.2.3", "", "", false},
		{"download-recent", false, "3", "1.2.3", release(recent), "at least 3 days old", false},
		{"download-old", false, "3", "1.2.3", release("2000-01-01T00:00:00Z"), "", false},
		{"download-latest", false, "3", "latest", release("2000-01-01T00:00:00Z"), "", false},
		{"download-tampered-manifest", false, "3", "1.2.3", release("2000-01-01T00:00:00Z"), "SHA256", true},
		{"negative-days", true, "-1", "1.2.3", "", "nonnegative integer", false},
		{"boolean-days", true, "false", "1.2.3", "", "nonnegative integer", false},
		{"fractional-days", true, "1.5", "1.2.3", "", "nonnegative integer", false},
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
			metadataPath := filepath.Join(dir, "release.json")
			require.NoError(t, os.WriteFile(metadataPath, []byte(test.metadata), 0o644))
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
    [ -s "$MOCK_RELEASE" ] || exit 97
    cp "$MOCK_RELEASE" "$output" ;;
  */SHA256SUMS.txt) cp "$MOCK_MANIFEST" "$output" ;;
  */copilot-linux-x64.tar.gz) cp "$MOCK_ARCHIVE" "$output" ;;
  *) echo "unexpected URL: $url" >&2; exit 97 ;;
esac
`), 0o755))
			logPath := filepath.Join(dir, "curl.log")
			installDir := filepath.Join(dir, "installed")
			require.NoError(t, os.MkdirAll(installDir, 0o755))
			args := []string{installerPath, test.version, "--cooldown-days", test.days}
			if test.cached {
				args = append(args, "--checksums-sha256", pin, "--prefer-toolcache")
			}
			cmd := exec.Command("bash", args...)
			cmd.Env = append(os.Environ(),
				"HOME="+dir, "RUNNER_TOOL_CACHE="+cache,
				"RUNNER_TEMP="+filepath.Join(dir, "runner-temp"),
				"COPILOT_INSTALL_DIR="+installDir, "GITHUB_PATH="+filepath.Join(dir, "github-path"),
				"PATH="+binDir+":"+os.Getenv("PATH"), "CURL_LOG="+logPath,
				"MOCK_RELEASE="+metadataPath, "MOCK_MANIFEST="+manifestPath, "MOCK_ARCHIVE="+archivePath,
				"GITHUB_TOKEN=test-token",
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
			if test.days == "0" {
				assert.Empty(t, log, "explicit zero must not query release metadata")
			} else if strings.Contains(string(log), "api.github.com") {
				assert.Contains(t, string(log), strings.Join([]string{"Authorization:", "Bearer", "test-token"}, " "))
			}
			if test.days != "0" && test.wantError == "" {
				assert.Contains(t, string(log), "https://api.github.com/repos/github/copilot-cli/releases/")
				if test.version == "latest" {
					assert.Contains(t, string(log), "/releases/latest")
					assert.Contains(t, string(log), "/releases/download/v1.2.3/", "bind downloads to the release whose age was checked")
				}
			} else if !test.tamper {
				assert.NotContains(t, string(log), "/SHA256SUMS.txt", "age rejection must precede archive verification")
			}
			if test.cached {
				assert.NotContains(t, string(log), ".tar.gz", "cached installs should only need release metadata")
			}
		})
	}
}
