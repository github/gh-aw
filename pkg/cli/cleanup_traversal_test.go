package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeUnreadableTraversalDirectory(t *testing.T, parent string) {
	t.Helper()
	path := filepath.Join(parent, "a-unreadable")
	require.NoError(t, os.MkdirAll(path, 0o700))
	require.NoError(t, os.Chmod(path, 0))
	t.Cleanup(func() {
		require.NoError(t, os.Chmod(path, 0o700))
	})
	if _, err := os.ReadDir(path); err == nil {
		t.Skip("directory permissions are not enforced for this user")
	}
}

func TestFindLegacyThreatDetectionVerdictContinuesAfterTraversalError(t *testing.T) {
	runDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "detection_result.json"),
		[]byte(`{"prompt_injection":false,"secret_leak":true,"malicious_patch":false}`), 0o600))
	makeUnreadableTraversalDirectory(t, runDir)

	verdict, found := findLegacyThreatDetectionVerdict(runDir)
	require.True(t, found)
	assert.True(t, verdict.SecretLeak)
}

func TestHasThreatDetectionArtifactContinuesAfterTraversalError(t *testing.T) {
	runDir := t.TempDir()
	artifactDir := filepath.Join(runDir, "detection")
	require.NoError(t, os.MkdirAll(artifactDir, 0o700))
	makeUnreadableTraversalDirectory(t, artifactDir)
	require.NoError(t, os.WriteFile(filepath.Join(artifactDir, "detection.log"), nil, 0o600))

	assert.True(t, hasThreatDetectionArtifact(runDir))
}

func TestFindModelRoutingFileContinuesAfterTraversalError(t *testing.T) {
	runDir := t.TempDir()
	makeUnreadableTraversalDirectory(t, runDir)
	routingPath := filepath.Join(runDir, "other", "model-routing.jsonl")
	require.NoError(t, os.MkdirAll(filepath.Dir(routingPath), 0o700))
	require.NoError(t, os.WriteFile(routingPath, nil, 0o600))

	assert.Equal(t, routingPath, findModelRoutingFile(runDir))
}

func TestCleanupTraversalMissingDirectory(t *testing.T) {
	runDir := filepath.Join(t.TempDir(), "missing")
	_, found := findLegacyThreatDetectionVerdict(runDir)
	assert.False(t, found)
	assert.False(t, hasThreatDetectionArtifact(runDir))
	assert.Empty(t, findModelRoutingFile(runDir))
}

func TestCleanupTraversalReportsErrors(t *testing.T) {
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestCleanupTraversalMissingDirectory$")
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(), "DEBUG=cli:audit_threat_detection,cli:token_usage_subagent", "DEBUG_COLORS=0")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	assert.Contains(t, string(output), "Failed to search threat-detection verdict path")
	assert.Contains(t, string(output), "Failed to search model routing path")
}
