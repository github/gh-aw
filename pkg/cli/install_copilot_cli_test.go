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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func prepareCopilotInstallerTestBin(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	// Never run the installer's shared-runner cleanup against the test host.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "find"), []byte(`#!/usr/bin/env bash
if [ "${1:-}" = /tmp ]; then exit 0; fi
exec /usr/bin/find "$@"
`), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "uname"), []byte(`#!/usr/bin/env bash
case "${1:-}" in -s) echo Linux ;; -m) echo x86_64 ;; *) echo Linux ;; esac
`), 0o755))
}

func TestInstallCopilotCLIScriptStagesMuslGuestVariant(t *testing.T) {
	t.Parallel()
	wd, err := os.Getwd()
	require.NoError(t, err)

	projectRoot := filepath.Join(wd, "..", "..")
	installScript := filepath.Join(projectRoot, "actions", "setup", "sh", "install_copilot_cli.sh")
	tempDir := t.TempDir()
	toolcacheBin := filepath.Join(tempDir, "toolcache", "copilot-cli", "1.2.3", "x64", "bin")
	require.NoError(t, os.MkdirAll(toolcacheBin, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(toolcacheBin, "copilot"), []byte("#!/usr/bin/env bash\necho 'copilot 1.2.3'\n"), 0o755))

	var archive bytes.Buffer
	gzipWriter := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gzipWriter)
	guestBinary := []byte("musl guest binary")
	require.NoError(t, tarWriter.WriteHeader(&tar.Header{Name: "copilot", Mode: 0o755, Size: int64(len(guestBinary)), Typeflag: tar.TypeReg}))
	_, err = tarWriter.Write(guestBinary)
	require.NoError(t, err)
	require.NoError(t, tarWriter.Close())
	require.NoError(t, gzipWriter.Close())
	guestArchive := filepath.Join(tempDir, "copilot-linuxmusl-x64.tar.gz")
	require.NoError(t, os.WriteFile(guestArchive, archive.Bytes(), 0o644))
	checksum := sha256.Sum256(archive.Bytes())
	checksums := filepath.Join(tempDir, "SHA256SUMS.txt")
	checksumsContent := []byte(fmt.Sprintf("%x  copilot-linuxmusl-x64.tar.gz\n", checksum))
	require.NoError(t, os.WriteFile(checksums, checksumsContent, 0o644))
	// Bundle a fixture pin beside a copy of the real installer, not in its environment.
	installerContent, err := os.ReadFile(installScript)
	require.NoError(t, err)
	installScript = filepath.Join(tempDir, "install_copilot_cli.sh")
	require.NoError(t, os.WriteFile(installScript, installerContent, 0o755))
	integrityContent, err := os.ReadFile(filepath.Join(projectRoot, "actions", "setup", "sh", "release_integrity.sh"))
	require.NoError(t, err)
	fixturePin := fmt.Sprintf("github/copilot-cli v1.2.3 SHA256SUMS.txt %x\nPINS", sha256.Sum256(checksumsContent))
	integrityContent = bytes.Replace(integrityContent, []byte("\nPINS"), []byte("\n"+fixturePin), 1)
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "release_integrity.sh"), integrityContent, 0o644))

	fakeBinDir := filepath.Join(tempDir, "fake-bin")
	prepareCopilotInstallerTestBin(t, fakeBinDir)
	require.NoError(t, os.WriteFile(filepath.Join(fakeBinDir, "uname"), []byte(`#!/usr/bin/env bash
case "${1:-}" in
  -s) echo Linux ;;
  -m) echo x86_64 ;;
  *) echo Linux ;;
esac
`), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(fakeBinDir, "sudo"), []byte(`#!/usr/bin/env bash
if [ "${1:-}" = "chown" ]; then exit 0; fi
exec "$@"
`), 0o755))
	curlLog := filepath.Join(tempDir, "curl.log")
	require.NoError(t, os.WriteFile(filepath.Join(fakeBinDir, "curl"), []byte(`#!/usr/bin/env bash
set -euo pipefail
output=""
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) output="$2"; shift 2 ;;
    *) url="$1"; shift ;;
  esac
done
echo "$url" >> "$CURL_LOG"
case "$url" in
  */SHA256SUMS.txt) cp "$MUSL_CHECKSUMS" "$output" ;;
  */copilot-linuxmusl-x64.tar.gz) cp "$MUSL_ARCHIVE" "$output" ;;
  *) echo "unexpected URL: $url" >&2; exit 97 ;;
esac
`), 0o755))

	runnerTemp := filepath.Join(tempDir, "runner-temp")
	installDir := filepath.Join(tempDir, "install-bin")
	require.NoError(t, os.MkdirAll(installDir, 0o755))
	cmd := exec.Command("bash", installScript, "1.2.3")
	cmd.Env = append(os.Environ(),
		"RUNNER_TOOL_CACHE="+filepath.Join(tempDir, "toolcache"),
		"COPILOT_INSTALL_DIR="+installDir,
		"GH_AW_COPILOT_GUEST_LIBC=musl",
		"RUNNER_TEMP="+runnerTemp,
		"CURL_LOG="+curlLog,
		"MUSL_CHECKSUMS="+checksums,
		"MUSL_ARCHIVE="+guestArchive,
		"PATH="+fakeBinDir+":"+os.Getenv("PATH"),
	)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	assert.Contains(t, string(output), "Staged musl Copilot CLI")
	staged, err := os.ReadFile(filepath.Join(runnerTemp, "gh-aw", "nvx-engine", "copilot"))
	require.NoError(t, err)
	assert.Equal(t, guestBinary, staged)
	curlOutput, err := os.ReadFile(curlLog)
	require.NoError(t, err)
	assert.Contains(t, string(curlOutput), "copilot-linuxmusl-x64.tar.gz")
	assert.NotContains(t, string(curlOutput), "copilot-linux-x64.tar.gz")
}

func TestInstallCopilotCLIScriptMuslCacheRequiresReviewedManifest(t *testing.T) {
	t.Parallel()
	wd, err := os.Getwd()
	require.NoError(t, err)
	projectRoot := filepath.Join(wd, "..", "..")

	for _, guestLibc := range []string{"musl", ""} {
		t.Run("guest_libc="+guestLibc, func(t *testing.T) {
			t.Parallel()
			tempDir := t.TempDir()
			toolcacheBin := filepath.Join(tempDir, "toolcache", "copilot-cli", "1.0.97", "x64", "bin")
			require.NoError(t, os.MkdirAll(toolcacheBin, 0o755))
			cachedCopilot := filepath.Join(toolcacheBin, "copilot")
			require.NoError(t, os.WriteFile(cachedCopilot, []byte("#!/usr/bin/env bash\necho 'copilot 1.0.97'\n"), 0o755))

			var archive bytes.Buffer
			gzipWriter := gzip.NewWriter(&archive)
			tarWriter := tar.NewWriter(gzipWriter)
			binary := []byte("#!/usr/bin/env bash\necho 'copilot 1.0.103'\n")
			require.NoError(t, tarWriter.WriteHeader(&tar.Header{Name: "copilot", Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg}))
			_, err := tarWriter.Write(binary)
			require.NoError(t, err)
			require.NoError(t, tarWriter.Close())
			require.NoError(t, gzipWriter.Close())
			archiveFile := filepath.Join(tempDir, "copilot.tar.gz")
			require.NoError(t, os.WriteFile(archiveFile, archive.Bytes(), 0o644))
			checksum := sha256.Sum256(archive.Bytes())
			manifest := []byte(fmt.Sprintf("%x  copilot-linux-x64.tar.gz\n%x  copilot-linuxmusl-x64.tar.gz\n", checksum, checksum))
			checksums := filepath.Join(tempDir, "SHA256SUMS.txt")
			require.NoError(t, os.WriteFile(checksums, manifest, 0o644))

			installer, err := os.ReadFile(filepath.Join(projectRoot, "actions", "setup", "sh", "install_copilot_cli.sh"))
			require.NoError(t, err)
			installScript := filepath.Join(tempDir, "install_copilot_cli.sh")
			require.NoError(t, os.WriteFile(installScript, installer, 0o755))
			integrity, err := os.ReadFile(filepath.Join(projectRoot, "actions", "setup", "sh", "release_integrity.sh"))
			require.NoError(t, err)
			lines := strings.Split(string(integrity), "\n")
			for i, line := range lines {
				if strings.HasPrefix(line, "github/copilot-cli v1.0.103 SHA256SUMS.txt ") {
					lines[i] = fmt.Sprintf("github/copilot-cli v1.0.103 SHA256SUMS.txt %x", sha256.Sum256(manifest))
				}
			}
			require.NoError(t, os.WriteFile(filepath.Join(tempDir, "release_integrity.sh"), []byte(strings.Join(lines, "\n")), 0o644))

			fakeBinDir := filepath.Join(tempDir, "fake-bin")
			prepareCopilotInstallerTestBin(t, fakeBinDir)
			require.NoError(t, os.WriteFile(filepath.Join(fakeBinDir, "curl"), []byte(`#!/usr/bin/env bash
set -euo pipefail
output=""
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in -o) output="$2"; shift 2 ;; *) url="$1"; shift ;; esac
done
echo "$url" >> "$CURL_LOG"
case "$url" in
  */compat.json)
    printf '%s\n' '{"agent-compat-v1":{"copilot":[{"min-gh-aw":"0.0.0","max-gh-aw":"*","min-agent":"1.0.21","max-agent":"1.0.103"}]}}' > "$output"
    ;;
  */v1.0.103/SHA256SUMS.txt) cp "$FIXTURE_CHECKSUMS" "$output" ;;
  */v1.0.103/copilot-linux-x64.tar.gz|*/v1.0.103/copilot-linuxmusl-x64.tar.gz) cp "$FIXTURE_ARCHIVE" "$output" ;;
  *) echo "unexpected URL: $url" >&2; exit 97 ;;
esac
`), 0o755))
			home := filepath.Join(tempDir, "home")
			installDir := filepath.Join(home, ".local", "bin")
			require.NoError(t, os.MkdirAll(installDir, 0o755))
			runnerTemp := filepath.Join(tempDir, "runner-temp")
			curlLog := filepath.Join(tempDir, "curl.log")
			githubPath := filepath.Join(tempDir, "github-path")
			cmd := exec.Command("bash", installScript, "--rootless")
			cmd.Env = append(os.Environ(),
				"HOME="+home,
				"RUNNER_TOOL_CACHE="+filepath.Join(tempDir, "toolcache"),
				"COPILOT_INSTALL_DIR="+installDir,
				"GH_AW_COMPILED_VERSION=v1.0.0",
				"GH_AW_COPILOT_GUEST_LIBC="+guestLibc,
				"RUNNER_TEMP="+runnerTemp,
				"GITHUB_PATH="+githubPath,
				"CURL_LOG="+curlLog,
				"FIXTURE_CHECKSUMS="+checksums,
				"FIXTURE_ARCHIVE="+archiveFile,
				"PATH="+fakeBinDir+":"+os.Getenv("PATH"),
			)
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", output)
			curlOutput, err := os.ReadFile(curlLog)
			require.NoError(t, err)
			installed, err := os.ReadFile(filepath.Join(installDir, "copilot"))
			require.NoError(t, err)
			pathOutput, err := os.ReadFile(githubPath)
			require.NoError(t, err)
			if guestLibc == "musl" {
				assert.Contains(t, string(output), "Skipping candidate (no reviewed manifest pin for musl guest: 1.0.97)")
				assert.NotContains(t, string(output), "Activating cached Copilot CLI")
				assert.Contains(t, string(curlOutput), "/v1.0.103/copilot-linux-x64.tar.gz")
				assert.Contains(t, string(curlOutput), "/v1.0.103/copilot-linuxmusl-x64.tar.gz")
				assert.Equal(t, binary, installed)
				assert.NotContains(t, string(pathOutput), toolcacheBin)
				staged, err := os.ReadFile(filepath.Join(runnerTemp, "gh-aw", "nvx-engine", "copilot"))
				require.NoError(t, err)
				assert.Equal(t, binary, staged)
			} else {
				assert.Contains(t, string(output), "Using cached GitHub Copilot CLI")
				assert.NotContains(t, string(output), "no reviewed manifest pin for musl guest")
				assert.NotContains(t, string(curlOutput), "SHA256SUMS.txt")
				assert.Contains(t, string(installed), cachedCopilot)
				assert.Contains(t, string(pathOutput), toolcacheBin)
			}
		})
	}
}

func TestInstallCopilotCLIScriptUsesToolcacheBeforeDownload(t *testing.T) {
	t.Parallel()
	wd, err := os.Getwd()
	require.NoError(t, err, "Failed to get working directory")

	projectRoot := filepath.Join(wd, "..", "..")
	installScript := filepath.Join(projectRoot, "actions", "setup", "sh", "install_copilot_cli.sh")

	tempDir := t.TempDir()
	toolcacheBin := filepath.Join(tempDir, "toolcache", "copilot-cli", "1.2.3", "x64", "bin")
	require.NoError(t, os.MkdirAll(toolcacheBin, 0o755))

	cachedCopilot := filepath.Join(toolcacheBin, "copilot")
	require.NoError(t, os.WriteFile(cachedCopilot, []byte("#!/usr/bin/env bash\necho 'copilot 1.2.3'\n"), 0o755))

	fakeBinDir := filepath.Join(tempDir, "fake-bin")
	prepareCopilotInstallerTestBin(t, fakeBinDir)

	curlLog := filepath.Join(tempDir, "curl.log")
	sudoScript := filepath.Join(fakeBinDir, "sudo")
	curlScript := filepath.Join(fakeBinDir, "curl")

	require.NoError(t, os.WriteFile(sudoScript, []byte(`#!/usr/bin/env bash
if [ "${1:-}" = "chown" ]; then
  exit 0
fi
exec "$@"
`), 0o755))
	require.NoError(t, os.WriteFile(curlScript, []byte(`#!/usr/bin/env bash
echo curl-invoked >> "`+curlLog+`"
exit 97
`), 0o755))

	githubPath := filepath.Join(tempDir, "github-path")
	installDir := filepath.Join(tempDir, "install-bin")
	require.NoError(t, os.MkdirAll(installDir, 0o755))
	cmd := exec.Command("bash", installScript, "1.2.3")
	cmd.Env = append(os.Environ(),
		"RUNNER_TOOL_CACHE="+filepath.Join(tempDir, "toolcache"),
		"GITHUB_PATH="+githubPath,
		"COPILOT_INSTALL_DIR="+installDir,
		"PATH="+fakeBinDir+":"+os.Getenv("PATH"),
	)

	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "install_copilot_cli.sh should succeed with a toolcache hit: %s", output)

	assert.Contains(t, string(output), "Using cached GitHub Copilot CLI", "script should use the toolcache before downloading")
	assert.NoFileExists(t, curlLog, "curl should not run when a cached Copilot CLI is available")

	githubPathContent, err := os.ReadFile(githubPath)
	require.NoError(t, err, "Expected the script to append the cached bin dir to GITHUB_PATH")
	assert.Contains(t, string(githubPathContent), toolcacheBin, "cached Copilot bin directory should be exported for later steps")

	// The agent is launched with the absolute install path, so a toolcache hit must still
	// materialize ${INSTALL_DIR}/copilot (see spawn ENOENT regression).
	installedCopilot := filepath.Join(installDir, "copilot")
	require.FileExists(t, installedCopilot, "toolcache hit must still create the canonical copilot path")
	wrapper, err := os.ReadFile(installedCopilot)
	require.NoError(t, err)
	assert.Contains(t, string(wrapper), cachedCopilot, "wrapper should exec the cached Copilot CLI")

	wrapperOutput, err := exec.Command(installedCopilot, "--version").CombinedOutput()
	require.NoError(t, err, "wrapper should be executable: %s", wrapperOutput)
	assert.Contains(t, string(wrapperOutput), "copilot 1.2.3")
}

func TestInstallCopilotCLIScriptPreservesCachedBinaryAtInstallPath(t *testing.T) {
	t.Parallel()
	wd, err := os.Getwd()
	require.NoError(t, err, "Failed to get working directory")

	projectRoot := filepath.Join(wd, "..", "..")
	installScript := filepath.Join(projectRoot, "actions", "setup", "sh", "install_copilot_cli.sh")

	tempDir := t.TempDir()
	toolcacheBin := filepath.Join(tempDir, "toolcache", "copilot-cli", "1.2.3", "x64", "bin")
	require.NoError(t, os.MkdirAll(toolcacheBin, 0o755))

	cachedCopilot := filepath.Join(toolcacheBin, "copilot")
	cachedContents := []byte("#!/usr/bin/env bash\necho 'copilot 1.2.3 preserved'\n")
	require.NoError(t, os.WriteFile(cachedCopilot, cachedContents, 0o755))
	fakeBinDir := filepath.Join(tempDir, "fake-bin")
	prepareCopilotInstallerTestBin(t, fakeBinDir)
	require.NoError(t, os.WriteFile(filepath.Join(fakeBinDir, "sudo"), []byte(`#!/usr/bin/env bash
if [ "${1:-}" = "chown" ]; then
  exit 0
fi
exec "$@"
`), 0o755))

	cmd := exec.Command("bash", installScript, "1.2.3")
	cmd.Env = append(os.Environ(),
		"RUNNER_TOOL_CACHE="+filepath.Join(tempDir, "toolcache"),
		"GITHUB_PATH="+filepath.Join(tempDir, "github-path"),
		"COPILOT_INSTALL_DIR="+toolcacheBin,
		"PATH="+fakeBinDir+":"+os.Getenv("PATH"),
	)

	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "install_copilot_cli.sh should preserve a cached binary already at the install path: %s", output)
	assert.Contains(t, string(output), "Cached binary already lives at "+cachedCopilot+" — no wrapper needed")

	actualContents, err := os.ReadFile(cachedCopilot)
	require.NoError(t, err)
	assert.Equal(t, cachedContents, actualContents, "cached binary should not be replaced with a wrapper")

	cachedOutput, err := exec.Command(cachedCopilot, "--version").CombinedOutput()
	require.NoError(t, err, "cached binary should remain executable: %s", cachedOutput)
	assert.Contains(t, string(cachedOutput), "copilot 1.2.3 preserved")
}

func TestInstallCopilotCLIScriptDevModeUsesToolcache(t *testing.T) {
	t.Parallel()
	const compatVersion = "1.0.56"
	const cachedCompatibleVersion = "1.0.40"
	const cachedBoundaryMinVersion = "1.0.21"
	const cachedTooOldVersion = "1.0.20"
	const cachedTooNewVersion = "1.0.60"

	wd, err := os.Getwd()
	require.NoError(t, err, "Failed to get working directory")

	projectRoot := filepath.Join(wd, "..", "..")
	installScript := filepath.Join(projectRoot, "actions", "setup", "sh", "install_copilot_cli.sh")

	tempDir := t.TempDir()
	toolcacheBin := filepath.Join(tempDir, "toolcache", "copilot-cli", cachedCompatibleVersion, "x64", "bin")
	minBoundaryToolcacheBin := filepath.Join(tempDir, "toolcache", "copilot-cli", cachedBoundaryMinVersion, "x64", "bin")
	tooOldToolcacheBin := filepath.Join(tempDir, "toolcache", "copilot-cli", cachedTooOldVersion, "x64", "bin")
	tooNewToolcacheBin := filepath.Join(tempDir, "toolcache", "copilot-cli", cachedTooNewVersion, "x64", "bin")
	require.NoError(t, os.MkdirAll(toolcacheBin, 0o755))
	require.NoError(t, os.MkdirAll(minBoundaryToolcacheBin, 0o755))
	require.NoError(t, os.MkdirAll(tooOldToolcacheBin, 0o755))
	require.NoError(t, os.MkdirAll(tooNewToolcacheBin, 0o755))

	cachedCopilot := filepath.Join(toolcacheBin, "copilot")
	minBoundaryCachedCopilot := filepath.Join(minBoundaryToolcacheBin, "copilot")
	tooOldCachedCopilot := filepath.Join(tooOldToolcacheBin, "copilot")
	tooNewCachedCopilot := filepath.Join(tooNewToolcacheBin, "copilot")
	require.NoError(t, os.WriteFile(cachedCopilot, []byte("#!/usr/bin/env bash\necho 'copilot "+cachedCompatibleVersion+"'\n"), 0o755))
	require.NoError(t, os.WriteFile(minBoundaryCachedCopilot, []byte("#!/usr/bin/env bash\necho 'copilot "+cachedBoundaryMinVersion+"'\n"), 0o755))
	require.NoError(t, os.WriteFile(tooOldCachedCopilot, []byte("#!/usr/bin/env bash\necho 'copilot "+cachedTooOldVersion+"'\n"), 0o755))
	require.NoError(t, os.WriteFile(tooNewCachedCopilot, []byte("#!/usr/bin/env bash\necho 'copilot "+cachedTooNewVersion+"'\n"), 0o755))

	fakeBinDir := filepath.Join(tempDir, "fake-bin")
	prepareCopilotInstallerTestBin(t, fakeBinDir)

	curlLog := filepath.Join(tempDir, "curl.log")
	sudoScript := filepath.Join(fakeBinDir, "sudo")
	curlScript := filepath.Join(fakeBinDir, "curl")

	require.NoError(t, os.WriteFile(sudoScript, []byte(`#!/usr/bin/env bash
if [ "${1:-}" = "chown" ]; then
  exit 0
fi
exec "$@"
`), 0o755))
	require.NoError(t, os.WriteFile(curlScript, []byte(`#!/usr/bin/env bash
set -euo pipefail
output_file=""
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o)
      output_file="$2"
      shift 2
      ;;
    *)
      url="$1"
      shift
      ;;
  esac
done
echo "$url" >> "`+curlLog+`"
if [[ "$url" == *"/compat.json" ]]; then
  cat > "$output_file" <<'JSON'
{
  "agent-compat-v1": {
    "copilot": [
      {
        "min-gh-aw": "0.72.0",
        "max-gh-aw": "*",
        "min-agent": "1.0.21",
        "max-agent": "`+compatVersion+`",
        "open": true
      }
    ]
  }
}
JSON
  exit 0
fi
echo "unexpected URL: $url" >&2
exit 97
`), 0o755))

	githubPath := filepath.Join(tempDir, "github-path")
	installDir := filepath.Join(tempDir, "install-bin")
	require.NoError(t, os.MkdirAll(installDir, 0o755))
	cmd := exec.Command("bash", installScript)
	cmd.Env = append(os.Environ(),
		"RUNNER_TOOL_CACHE="+filepath.Join(tempDir, "toolcache"),
		"GITHUB_PATH="+githubPath,
		"GH_AW_COMPILED_VERSION=dev",
		"COPILOT_INSTALL_DIR="+installDir,
		"PATH="+fakeBinDir+":"+os.Getenv("PATH"),
	)

	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "install_copilot_cli.sh should resolve compat version and use toolcache: %s", output)

	assert.Contains(t, string(output), "Resolved Copilot CLI version from compatibility matrix: "+compatVersion)
	assert.Contains(t, string(output), "Using compat-resolved Copilot CLI window: 1.0.21.."+compatVersion)
	assert.Contains(t, string(output), "Skipping candidate (below compat minimum: "+cachedTooOldVersion+" < 1.0.21)")
	assert.Contains(t, string(output), "Skipping candidate (above compat maximum: "+cachedTooNewVersion+" > "+compatVersion+")")
	assert.NotContains(t, string(output), "Skipping candidate (below compat minimum: "+cachedBoundaryMinVersion+" < 1.0.21)")
	assert.Contains(t, string(output), "Selected best cached version:")
	assert.NotContains(t, string(output), "Selected best cached version: "+cachedTooNewVersion)
	assert.Contains(t, string(output), "Using cached GitHub Copilot CLI")

	// A dev-mode compat toolcache hit must still create the canonical ${INSTALL_DIR}/copilot
	// path that the agent is spawned with.
	installedCopilot := filepath.Join(installDir, "copilot")
	require.FileExists(t, installedCopilot, "toolcache hit must still create the canonical copilot path")
	wrapper, err := os.ReadFile(installedCopilot)
	require.NoError(t, err)
	assert.Contains(t, string(wrapper), cachedCopilot, "wrapper should exec the best cached Copilot CLI")
	wrapperOutput, err := exec.Command(installedCopilot, "--version").CombinedOutput()
	require.NoError(t, err, "dev-mode toolcache wrapper should be executable: %s", wrapperOutput)
	assert.Contains(t, string(wrapperOutput), "copilot "+cachedCompatibleVersion)

	curlLogContent, err := os.ReadFile(curlLog)
	require.NoError(t, err, "Expected curl to fetch compatibility matrix")
	assert.Contains(t, string(curlLogContent), "/compat.json", "compat matrix should be downloaded")
	assert.NotContains(t, string(curlLogContent), "SHA256SUMS.txt", "release downloads should not run when toolcache is hit")

	// Ensure compat.json is only fetched once — no double network fallback.
	curlLines := strings.Split(strings.TrimSpace(string(curlLogContent)), "\n")
	compatFetches := 0
	for _, line := range curlLines {
		if strings.Contains(line, "/compat.json") {
			compatFetches++
		}
	}
	assert.Equal(t, 1, compatFetches, "compat.json should be fetched exactly once (no double fallback)")
}

func TestInstallCopilotCLIScriptAppliesCopilotMinVersionOverrideToToolcache(t *testing.T) {
	t.Parallel()
	const compatVersion = "1.0.87"
	const cachedTooOldVersion = "1.0.85"
	const cachedMinVersion = "1.0.87"

	wd, err := os.Getwd()
	require.NoError(t, err, "Failed to get working directory")

	projectRoot := filepath.Join(wd, "..", "..")
	installScript := filepath.Join(projectRoot, "actions", "setup", "sh", "install_copilot_cli.sh")

	tempDir := t.TempDir()
	tooOldToolcacheBin := filepath.Join(tempDir, "toolcache", "copilot-cli", cachedTooOldVersion, "x64", "bin")
	minToolcacheBin := filepath.Join(tempDir, "toolcache", "copilot-cli", cachedMinVersion, "x64", "bin")
	require.NoError(t, os.MkdirAll(tooOldToolcacheBin, 0o755))
	require.NoError(t, os.MkdirAll(minToolcacheBin, 0o755))

	tooOldCachedCopilot := filepath.Join(tooOldToolcacheBin, "copilot")
	minCachedCopilot := filepath.Join(minToolcacheBin, "copilot")
	require.NoError(t, os.WriteFile(tooOldCachedCopilot, []byte("#!/usr/bin/env bash\necho 'copilot "+cachedTooOldVersion+"'\n"), 0o755))
	require.NoError(t, os.WriteFile(minCachedCopilot, []byte("#!/usr/bin/env bash\necho 'copilot "+cachedMinVersion+"'\n"), 0o755))

	fakeBinDir := filepath.Join(tempDir, "fake-bin")
	prepareCopilotInstallerTestBin(t, fakeBinDir)

	curlLog := filepath.Join(tempDir, "curl.log")
	sudoScript := filepath.Join(fakeBinDir, "sudo")
	curlScript := filepath.Join(fakeBinDir, "curl")

	require.NoError(t, os.WriteFile(sudoScript, []byte(`#!/usr/bin/env bash
if [ "${1:-}" = "chown" ]; then
  exit 0
fi
exec "$@"
`), 0o755))
	require.NoError(t, os.WriteFile(curlScript, []byte(`#!/usr/bin/env bash
set -euo pipefail
output_file=""
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o)
      output_file="$2"
      shift 2
      ;;
    *)
      url="$1"
      shift
      ;;
  esac
done
echo "$url" >> "`+curlLog+`"
if [[ "$url" == *"/compat.json" ]]; then
  cat > "$output_file" <<'JSON'
{
  "agent-compat-v1": {
    "copilot": [
      {
        "min-gh-aw": "0.72.0",
        "max-gh-aw": "*",
        "min-agent": "1.0.21",
        "max-agent": "`+compatVersion+`",
        "open": true
      }
    ]
  }
}
JSON
  exit 0
fi
echo "unexpected URL: $url" >&2
exit 97
`), 0o755))

	githubPath := filepath.Join(tempDir, "github-path")
	installDir := filepath.Join(tempDir, "install-bin")
	require.NoError(t, os.MkdirAll(installDir, 0o755))
	cmd := exec.Command("bash", installScript)
	cmd.Env = append(os.Environ(),
		"RUNNER_TOOL_CACHE="+filepath.Join(tempDir, "toolcache"),
		"GITHUB_PATH="+githubPath,
		"GH_AW_COMPILED_VERSION=dev",
		"GH_AW_COPILOT_MIN_VERSION="+cachedMinVersion,
		"COPILOT_INSTALL_DIR="+installDir,
		"PATH="+fakeBinDir+":"+os.Getenv("PATH"),
	)

	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "install_copilot_cli.sh should apply GH_AW_COPILOT_MIN_VERSION to toolcache lookup: %s", output)

	outputString := string(output)
	assert.Contains(t, outputString, "Raising Copilot CLI minimum version from 1.0.21 to "+cachedMinVersion+" due to GH_AW_COPILOT_MIN_VERSION.")
	assert.Contains(t, outputString, "Using compat-resolved Copilot CLI window: "+cachedMinVersion+".."+compatVersion)
	assert.Contains(t, outputString, "Skipping candidate (below compat minimum: "+cachedTooOldVersion+" < "+cachedMinVersion+")")
	assert.Contains(t, outputString, "Using cached GitHub Copilot CLI")

	installedCopilot := filepath.Join(installDir, "copilot")
	require.FileExists(t, installedCopilot, "toolcache hit must still create the canonical copilot path")
	wrapper, err := os.ReadFile(installedCopilot)
	require.NoError(t, err)
	assert.Contains(t, string(wrapper), minCachedCopilot, "wrapper should exec the cached Copilot CLI at the feature minimum")
	wrapperOutput, err := exec.Command(installedCopilot, "--version").CombinedOutput()
	require.NoError(t, err, "feature-minimum toolcache wrapper should be executable: %s", wrapperOutput)
	assert.Contains(t, string(wrapperOutput), "copilot "+cachedMinVersion)
}

func TestInstallCopilotCLIScriptLatestAppliesCopilotMinVersionToToolcache(t *testing.T) {
	t.Parallel()
	wd, err := os.Getwd()
	require.NoError(t, err)
	installScript := filepath.Join(wd, "..", "..", "actions", "setup", "sh", "install_copilot_cli.sh")

	for _, hasCompatibleVersion := range []bool{false, true} {
		t.Run(fmt.Sprintf("compatible_cached=%t", hasCompatibleVersion), func(t *testing.T) {
			t.Parallel()
			tempDir := t.TempDir()
			toolcache := filepath.Join(tempDir, "toolcache")
			versions := []string{"1.0.34"}
			if hasCompatibleVersion {
				versions = append(versions, "1.0.35")
			}
			for _, version := range versions {
				binDir := filepath.Join(toolcache, "copilot-cli", version, "x64", "bin")
				require.NoError(t, os.MkdirAll(binDir, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(binDir, "copilot"), []byte("#!/usr/bin/env bash\necho 'copilot "+version+"'\n"), 0o755))
			}
			fakeBinDir := filepath.Join(tempDir, "fake-bin")
			prepareCopilotInstallerTestBin(t, fakeBinDir)
			require.NoError(t, os.WriteFile(filepath.Join(fakeBinDir, "sudo"), []byte(`#!/usr/bin/env bash
if [ "${1:-}" = "chown" ]; then exit 0; fi
exec "$@"
`), 0o755))
			curlLog := filepath.Join(tempDir, "curl.log")
			require.NoError(t, os.WriteFile(filepath.Join(fakeBinDir, "curl"), []byte("#!/usr/bin/env bash\necho \"$*\" >> \""+curlLog+"\"\nexit 97\n"), 0o755))

			installDir := filepath.Join(tempDir, "install-bin")
			require.NoError(t, os.MkdirAll(installDir, 0o755))
			cmd := exec.Command("bash", installScript, "latest")
			cmd.Env = append(os.Environ(),
				"RUNNER_TOOL_CACHE="+toolcache,
				"GH_AW_COPILOT_MIN_VERSION=1.0.35",
				"COPILOT_INSTALL_DIR="+installDir,
				"GITHUB_PATH="+filepath.Join(tempDir, "github-path"),
				"PATH="+fakeBinDir+":"+os.Getenv("PATH"),
			)
			output, err := cmd.CombinedOutput()
			assert.Contains(t, string(output), "Skipping candidate (below compat minimum: 1.0.34 < 1.0.35)")
			if hasCompatibleVersion {
				require.NoError(t, err, "%s", output)
				wrapperOutput, err := exec.Command(filepath.Join(installDir, "copilot"), "--version").CombinedOutput()
				require.NoError(t, err, "%s", wrapperOutput)
				assert.Contains(t, string(wrapperOutput), "copilot 1.0.35")
				assert.NoFileExists(t, curlLog)
			} else {
				require.Error(t, err, "the mocked download must fail: %s", output)
				assert.NotContains(t, string(output), "Using cached GitHub Copilot CLI")
				assert.Contains(t, string(output), "No reviewed SHA256 pin")
				assert.NoFileExists(t, curlLog, "an unreviewed latest download must fail before network access")
				assert.NoFileExists(t, filepath.Join(installDir, "copilot"))
			}
		})
	}
}

func TestInstallCopilotCLIScriptRejectsExplicitVersionBelowCopilotMinVersion(t *testing.T) {
	t.Parallel()

	wd, err := os.Getwd()
	require.NoError(t, err)

	installScript := filepath.Join(wd, "..", "..", "actions", "setup", "sh", "install_copilot_cli.sh")

	tempDir := t.TempDir()
	fakeBinDir := filepath.Join(tempDir, "fake-bin")
	prepareCopilotInstallerTestBin(t, fakeBinDir)
	require.NoError(t, os.WriteFile(filepath.Join(fakeBinDir, "sudo"), []byte(`#!/usr/bin/env bash
if [ "${1:-}" = "chown" ]; then
  exit 0
fi
exec "$@"
`), 0o755))

	cmd := exec.Command("bash", installScript, "1.0.85")
	cmd.Env = append(os.Environ(),
		"GH_AW_COPILOT_MIN_VERSION=1.0.87",
		"COPILOT_INSTALL_DIR="+filepath.Join(tempDir, "install-bin"),
		"GITHUB_PATH="+filepath.Join(tempDir, "github-path"),
		"PATH="+fakeBinDir+":"+os.Getenv("PATH"),
	)

	output, err := cmd.CombinedOutput()
	require.Error(t, err, "install_copilot_cli.sh should reject an explicit version below GH_AW_COPILOT_MIN_VERSION")
	assert.Contains(t, string(output), "1.0.85")
	assert.Contains(t, string(output), "is below required minimum")
	assert.Contains(t, string(output), "1.0.87")
	assert.NotContains(t, string(output), "Downloading binary", "script should fail before attempting a release download")
}

func TestInstallCopilotCLIScriptUsesBoundedRetriesForReleaseDownloads(t *testing.T) {
	t.Parallel()
	wd, err := os.Getwd()
	require.NoError(t, err)

	installScript := filepath.Join(wd, "..", "..", "actions", "setup", "sh", "install_copilot_cli.sh")
	script, err := os.ReadFile(installScript)
	require.NoError(t, err)

	for _, download := range []string{
		`curl --proto '=https' --proto-redir '=https' -fsSL --retry 5 --retry-delay 2 --retry-max-time 60 --retry-all-errors -o "${TEMP_DIR}/SHA256SUMS.txt" "${CHECKSUMS_URL}"`,
		`curl --proto '=https' --proto-redir '=https' -fsSL --retry 5 --retry-delay 2 --retry-max-time 60 --retry-all-errors -o "${TEMP_DIR}/${TARBALL_NAME}" "${TARBALL_URL}"`,
	} {
		assert.Contains(t, string(script), download)
	}
}

func TestInstallCopilotCLIScriptRootlessModeUsesRealScriptWithToolcacheAndNoSudo(t *testing.T) {
	t.Parallel()
	wd, err := os.Getwd()
	require.NoError(t, err, "Failed to get working directory")

	projectRoot := filepath.Join(wd, "..", "..")
	installScript := filepath.Join(projectRoot, "actions", "setup", "sh", "install_copilot_cli.sh")

	testCases := []struct {
		name string
		args []string
	}{
		{name: "rootless flag before version", args: []string{"--rootless", "1.2.3"}},
		{name: "rootless flag after version", args: []string{"1.2.3", "--rootless"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()
			homeDir := filepath.Join(tempDir, "home")
			require.NoError(t, os.MkdirAll(homeDir, 0o755))

			toolcacheBin := filepath.Join(tempDir, "toolcache", "copilot-cli", "1.2.3", "x64", "bin")
			require.NoError(t, os.MkdirAll(toolcacheBin, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(toolcacheBin, "copilot"), []byte("#!/usr/bin/env bash\necho 'copilot 1.2.3'\n"), 0o755))

			fakeBinDir := filepath.Join(tempDir, "fake-bin")
			prepareCopilotInstallerTestBin(t, fakeBinDir)

			sudoScript := filepath.Join(fakeBinDir, "sudo")
			sudoLog := filepath.Join(tempDir, "sudo.log")
			require.NoError(t, os.WriteFile(sudoScript, []byte(`#!/usr/bin/env bash
echo "sudo-called: $*" >> "`+sudoLog+`"
exit 99
`), 0o755))

			cmd := exec.Command("bash", append([]string{installScript}, tc.args...)...)
			cmd.Env = append(os.Environ(),
				"HOME="+homeDir,
				"RUNNER_TOOL_CACHE="+filepath.Join(tempDir, "toolcache"),
				"GITHUB_PATH=",
				"PATH="+fakeBinDir+":"+os.Getenv("PATH"),
			)

			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "install_copilot_cli.sh should succeed in rootless mode with toolcache and no sudo: %s", output)

			assert.Contains(t, string(output), "Using cached GitHub Copilot CLI", "script should use cached copilot CLI")
			assert.Contains(t, string(output), "GITHUB_PATH not set — relying on "+filepath.Join(homeDir, ".local", "bin", "copilot"))
			assert.Contains(t, string(output), "Wrapper installed at "+filepath.Join(homeDir, ".local", "bin", "copilot"))
			assert.FileExists(t, filepath.Join(homeDir, ".local", "bin", "copilot"))
			assert.NoFileExists(t, sudoLog, "sudo should not be called in rootless mode")
		})
	}
}

func TestInstallCopilotCLIScriptFallsBackToBakedInDefaultWhenCompatUnavailable(t *testing.T) {
	t.Parallel()
	// When no explicit version argument is passed AND GH_AW_COMPILED_VERSION is not set
	// (so compat.json resolution is skipped), the script must fall back to its baked-in
	// DEFAULT_COPILOT_VERSION rather than exiting with an error.
	wd, err := os.Getwd()
	require.NoError(t, err, "Failed to get working directory")

	projectRoot := filepath.Join(wd, "..", "..")
	installScript := filepath.Join(projectRoot, "actions", "setup", "sh", "install_copilot_cli.sh")

	// Parse DEFAULT_COPILOT_VERSION directly from the script so the test stays in sync.
	raw, readErr := os.ReadFile(installScript)
	require.NoError(t, readErr, "cannot read install script")
	defaultVersion := ""
	for line := range strings.SplitSeq(string(raw), "\n") {
		if val, ok := strings.CutPrefix(line, "DEFAULT_COPILOT_VERSION="); ok {
			defaultVersion = strings.Trim(val, `"`)
			break
		}
	}
	require.NotEmpty(t, defaultVersion, "DEFAULT_COPILOT_VERSION must be set in the install script")

	tempDir := t.TempDir()

	// Populate toolcache with exactly DEFAULT_COPILOT_VERSION so we can verify the
	// script selects it (rather than attempting a network download that would fail).
	toolcacheBin := filepath.Join(tempDir, "toolcache", "copilot-cli", defaultVersion, "x64", "bin")
	require.NoError(t, os.MkdirAll(toolcacheBin, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(toolcacheBin, "copilot"),
		[]byte("#!/usr/bin/env bash\necho 'copilot "+defaultVersion+"'\n"), 0o755))

	fakeBinDir := filepath.Join(tempDir, "fake-bin")
	prepareCopilotInstallerTestBin(t, fakeBinDir)

	curlLog := filepath.Join(tempDir, "curl.log")
	sudoScript := filepath.Join(fakeBinDir, "sudo")
	curlScript := filepath.Join(fakeBinDir, "curl")

	require.NoError(t, os.WriteFile(sudoScript, []byte(`#!/usr/bin/env bash
if [ "${1:-}" = "chown" ]; then
  exit 0
fi
exec "$@"
`), 0o755))
	require.NoError(t, os.WriteFile(curlScript, []byte(`#!/usr/bin/env bash
echo curl-invoked >> "`+curlLog+`"
exit 97
`), 0o755))

	githubPath := filepath.Join(tempDir, "github-path")
	installDir := filepath.Join(tempDir, "install-bin")
	require.NoError(t, os.MkdirAll(installDir, 0o755))

	// No version argument, no GH_AW_COMPILED_VERSION → script must fall back to DEFAULT_COPILOT_VERSION.
	cmd := exec.Command("bash", installScript)
	cmd.Env = append(os.Environ(),
		"RUNNER_TOOL_CACHE="+filepath.Join(tempDir, "toolcache"),
		"GITHUB_PATH="+githubPath,
		"COPILOT_INSTALL_DIR="+installDir,
		"PATH="+fakeBinDir+":"+os.Getenv("PATH"),
		// Explicitly unset GH_AW_COMPILED_VERSION to simulate the fallback scenario.
		"GH_AW_COMPILED_VERSION=",
	)

	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "install_copilot_cli.sh should succeed using baked-in default version: %s", output)

	assert.Contains(t, string(output), "Compat resolution unavailable; falling back to baked-in default version",
		"script should report that it fell back to baked-in default")
	assert.Contains(t, string(output), "Using cached GitHub Copilot CLI",
		"script should use the toolcache entry for DEFAULT_COPILOT_VERSION")
	assert.NoFileExists(t, curlLog, "curl should not run when a cached Copilot CLI is available")
}
