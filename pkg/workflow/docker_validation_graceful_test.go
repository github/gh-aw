//go:build !integration && !js && !wasm

package workflow

import (
	"os/exec"
	"testing"

	"github.com/github/gh-aw/pkg/syncutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetDockerDaemonStateForTest resets the package-level daemon state so
// individual tests can control it without interference from earlier tests.
// We use a zero-value assignment to clear the cached state.
func resetDockerDaemonStateForTest() {
	// Clear by assigning a new zero-value loader that will re-run detection on next call
	dockerDaemonLoader = syncutil.OnceLoader[bool]{}
}

// TestValidateDockerImage_SkipsWhenDockerUnavailable verifies that
// validateDockerImage degrades gracefully (returns nil) when Docker
// is not installed or the daemon is not running, instead of returning
// an error that surfaces as a spurious warning.
func TestValidateDockerImage_SkipsWhenDockerUnavailable(t *testing.T) {
	// If docker is not installed or daemon not running, validation should
	// silently pass — no error, no warning.
	if _, lookErr := exec.LookPath("docker"); lookErr != nil {
		err := validateDockerImageWithMode("ghcr.io/some/image:latest", false, false, false)
		assert.NoError(t, err, "should silently skip when Docker is not installed")
		return
	}
	if !isDockerDaemonRunning() {
		err := validateDockerImageWithMode("ghcr.io/some/image:latest", false, false, false)
		assert.NoError(t, err, "should silently skip when Docker daemon is not running")
		return
	}

	t.Skip("Docker is available — graceful degradation path not exercised")
}

// TestValidateDockerImage_StillRejectsHyphenWithoutDocker verifies that
// the argument injection check still works even when Docker is unavailable.
func TestValidateDockerImage_StillRejectsHyphenWithoutDocker(t *testing.T) {
	// The hyphen-prefix guard runs before the Docker availability check,
	// so it should always reject invalid names regardless of Docker state.
	err := validateDockerImageWithMode("-malicious", false, false, false)
	require.Error(t, err, "should reject image names starting with hyphen regardless of Docker availability")
	require.ErrorContains(t, err, "names must not start with '-'",
		"error should explain why the name is invalid")
}

// TestValidateContainerImages_NoWarningWithoutDocker verifies that
// validateContainerImages does not produce errors when Docker is unavailable
// and the workflow references container-based tools.
func TestValidateContainerImages_NoWarningWithoutDocker(t *testing.T) {
	if _, lookErr := exec.LookPath("docker"); lookErr == nil && isDockerDaemonRunning() {
		t.Skip("Docker is available — graceful degradation path not exercised")
	}

	workflowData := &WorkflowData{
		Tools: map[string]any{
			"serena": map[string]any{
				"container": "ghcr.io/github/serena-mcp-server",
				"version":   "latest",
			},
		},
	}

	compiler := NewCompiler()
	err := compiler.validateContainerImages(workflowData)
	assert.NoError(t, err, "container image validation should silently pass when Docker is unavailable")
}

// TestValidateDockerImage_RequireDockerFailsWhenUnavailable verifies that
// when requireDocker is true, validateDockerImage returns an error instead
// of silently skipping when Docker is not installed or the daemon is not running.
func TestValidateDockerImage_RequireDockerFailsWhenUnavailable(t *testing.T) {
	if _, lookErr := exec.LookPath("docker"); lookErr != nil {
		err := validateDockerImageWithMode("ghcr.io/some/image:latest", false, true, false)
		require.Error(t, err, "should fail when Docker is not installed and requireDocker is true")
		require.ErrorContains(t, err, "docker not installed",
			"error should mention Docker is not installed")
		require.ErrorContains(t, err, "--validate-images",
			"error should mention the --validate-images flag")
		return
	}
	if !isDockerDaemonRunning() {
		err := validateDockerImageWithMode("ghcr.io/some/image:latest", false, true, false)
		require.Error(t, err, "should fail when Docker daemon is not running and requireDocker is true")
		require.ErrorContains(t, err, "docker daemon not running",
			"error should mention Docker daemon is not running")
		require.ErrorContains(t, err, "--validate-images",
			"error should mention the --validate-images flag")
		return
	}

	t.Skip("Docker is available — requireDocker failure path not exercised")
}

// TestValidateContainerImages_RequireDockerFailsWhenUnavailable verifies that
// when requireDocker is set on the compiler, validateContainerImages returns
// an error when Docker is unavailable.
func TestValidateContainerImages_RequireDockerFailsWhenUnavailable(t *testing.T) {
	if _, lookErr := exec.LookPath("docker"); lookErr == nil && isDockerDaemonRunning() {
		t.Skip("Docker is available — requireDocker failure path not exercised")
	}

	workflowData := &WorkflowData{
		Tools: map[string]any{
			"serena": map[string]any{
				"container": "ghcr.io/github/serena-mcp-server",
				"version":   "latest",
			},
		},
	}

	compiler := NewCompiler()
	compiler.SetRequireDocker(true)
	err := compiler.validateContainerImages(workflowData)
	require.Error(t, err, "container image validation should fail when Docker is unavailable and requireDocker is true")
}

// TestMarkDockerDaemonUnavailable_UpdatesState verifies that markDockerDaemonUnavailable
// updates the cached daemon state so subsequent calls to isDockerDaemonRunning return false.
func TestMarkDockerDaemonUnavailable_UpdatesState(t *testing.T) {
	t.Cleanup(resetDockerDaemonStateForTest)

	// Seed the cache as "available"
	dockerDaemonLoader.Override(true, nil)

	assert.True(t, isDockerDaemonRunning(), "daemon should appear available before marking unavailable")

	markDockerDaemonUnavailable()

	assert.False(t, isDockerDaemonRunning(), "daemon should appear unavailable after markDockerDaemonUnavailable")
}

// TestMarkDockerDaemonUnavailable_SkipsSubsequentValidation verifies that once the daemon
// is marked unavailable, validateDockerImage returns nil for additional images immediately,
// without attempting further docker commands.
func TestMarkDockerDaemonUnavailable_SkipsSubsequentValidation(t *testing.T) {
	if _, lookErr := exec.LookPath("docker"); lookErr != nil {
		t.Skip("Docker not installed — this test requires docker binary on PATH")
	}
	t.Cleanup(resetDockerDaemonStateForTest)

	// Mark daemon unavailable as if a previous pull had discovered "Cannot connect to the Docker daemon"
	dockerDaemonLoader.Override(false, nil)

	// Subsequent validation of any image (even a clearly non-existent one) should return nil, not an error.
	err := validateDockerImageWithMode("ghcr.io/github/serena-mcp-server:latest", false, false, false)
	assert.NoError(t, err, "should skip validation when daemon is already marked unavailable")
}
