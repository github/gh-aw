//go:build !integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	autoUpdateLog := filepath.Join(tempDir, "auto-update.log")
	require.NoError(t, os.WriteFile(cachedCopilot, []byte("#!/usr/bin/env bash\necho \"${COPILOT_AUTO_UPDATE:-}\" >> \""+autoUpdateLog+"\"\necho 'copilot 1.2.3'\n"), 0o755))

	fakeBinDir := filepath.Join(tempDir, "fake-bin")
	require.NoError(t, os.MkdirAll(fakeBinDir, 0o755))

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

	autoUpdateValues, err := os.ReadFile(autoUpdateLog)
	require.NoError(t, err)
	assert.Equal(t, "false\nfalse\n", string(autoUpdateValues), "installer version checks should disable automatic updates")

	wrapperOutput, err := exec.Command(installedCopilot, "--version").CombinedOutput()
	require.NoError(t, err, "wrapper should be executable: %s", wrapperOutput)
	assert.Contains(t, string(wrapperOutput), "copilot 1.2.3")
}

func TestInstallCopilotCLIScriptRejectsMislabeledToolcacheBinary(t *testing.T) {
	t.Parallel()
	wd, err := os.Getwd()
	require.NoError(t, err, "Failed to get working directory")

	projectRoot := filepath.Join(wd, "..", "..")
	installScript := filepath.Join(projectRoot, "actions", "setup", "sh", "install_copilot_cli.sh")

	tempDir := t.TempDir()
	toolcacheBin := filepath.Join(tempDir, "toolcache", "copilot-cli", "1.2.3", "x64", "bin")
	require.NoError(t, os.MkdirAll(toolcacheBin, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(toolcacheBin, "copilot"), []byte("#!/usr/bin/env bash\necho 'copilot 1.2.4'\n"), 0o755))

	fakeBinDir := filepath.Join(tempDir, "fake-bin")
	require.NoError(t, os.MkdirAll(fakeBinDir, 0o755))
	curlLog := filepath.Join(tempDir, "curl.log")
	require.NoError(t, os.WriteFile(filepath.Join(fakeBinDir, "sudo"), []byte("#!/usr/bin/env bash\nexec \"$@\"\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(fakeBinDir, "curl"), []byte("#!/usr/bin/env bash\necho curl-invoked >> \""+curlLog+"\"\nexit 97\n"), 0o755))

	cmd := exec.Command("bash", installScript, "1.2.3")
	cmd.Env = append(os.Environ(),
		"RUNNER_TOOL_CACHE="+filepath.Join(tempDir, "toolcache"),
		"COPILOT_INSTALL_DIR="+filepath.Join(tempDir, "install-bin"),
		"PATH="+fakeBinDir+":"+os.Getenv("PATH"),
	)

	output, err := cmd.CombinedOutput()
	require.Error(t, err, "script should attempt a download after rejecting the mislabeled cache entry")
	assert.Contains(t, string(output), "Skipping candidate (cached binary reports 1.2.4, directory claims 1.2.3)")
	assert.FileExists(t, curlLog, "release download should be attempted after rejecting the cache entry")
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
	require.NoError(t, os.MkdirAll(fakeBinDir, 0o755))
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
	require.NoError(t, os.MkdirAll(fakeBinDir, 0o755))

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

func TestInstallCopilotCLIScriptUsesBoundedRetriesForReleaseDownloads(t *testing.T) {
	t.Parallel()
	wd, err := os.Getwd()
	require.NoError(t, err)

	installScript := filepath.Join(wd, "..", "..", "actions", "setup", "sh", "install_copilot_cli.sh")
	script, err := os.ReadFile(installScript)
	require.NoError(t, err)

	for _, download := range []string{
		`curl -fsSL --retry 5 --retry-delay 2 --retry-max-time 60 --retry-all-errors -o "${TEMP_DIR}/SHA256SUMS.txt" "${CHECKSUMS_URL}"`,
		`curl -fsSL --retry 5 --retry-delay 2 --retry-max-time 60 --retry-all-errors -o "${TEMP_DIR}/${TARBALL_NAME}" "${TARBALL_URL}"`,
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
			require.NoError(t, os.MkdirAll(fakeBinDir, 0o755))

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
	require.NoError(t, os.MkdirAll(fakeBinDir, 0o755))

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
