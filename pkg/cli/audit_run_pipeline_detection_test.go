package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditNeedsDetectionArtifact(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()
	summary := &RunSummary{RunAnalysis: RunAnalysis{
		JobDetails: []JobInfoWithDuration{{
			JobInfo: JobInfo{Name: "detection", Conclusion: "success"},
		}},
	}}
	cfg := auditRunConfig{outputDir: runDir}

	assert.True(t, auditNeedsDetectionArtifact(cfg, summary))

	cfg.artifactFilter = []string{"agent"}
	assert.False(t, auditNeedsDetectionArtifact(cfg, summary))

	cfg.artifactFilter = nil
	require.NoError(t, os.MkdirAll(filepath.Join(runDir, "usage", "detection"), 0o700))
	require.NoError(t, os.WriteFile(
		filepath.Join(runDir, "usage", "detection", "detection_result.json"),
		[]byte(`{"job_result":"success","conclusion":"success"}`), 0o600,
	))
	assert.False(t, auditNeedsDetectionArtifact(cfg, summary))
	cfg.artifactFilter = []string{"detection"}
	assert.True(t, auditNeedsDetectionArtifact(cfg, summary))
	require.NoError(t, os.Remove(filepath.Join(runDir, "usage", "detection", "detection_result.json")))
	require.NoError(t, os.MkdirAll(filepath.Join(runDir, "detection"), 0o700))
	require.NoError(t, os.WriteFile(
		filepath.Join(runDir, "detection", "detection_result.json"),
		[]byte(`{"prompt_injection":false,"secret_leak":false,"malicious_patch":false}`),
		0o600,
	))
	cfg.artifactFilter = nil
	assert.False(t, auditNeedsDetectionArtifact(cfg, summary))
}

func TestInvalidateCompleteArtifactDownloadMarker(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()
	marker := filepath.Join(runDir, downloadedArtifactsMarkerDir, string(ArtifactSetAll))
	require.NoError(t, os.MkdirAll(filepath.Dir(marker), 0o700))
	require.NoError(t, os.WriteFile(marker, nil, 0o600))

	require.NoError(t, invalidateCompleteArtifactDownloadMarker(runDir))
	assert.NoFileExists(t, marker)
}

func TestCachedAuditBypassesWhenRequestedAgentOutputIsMissing(t *testing.T) {
	runDir := t.TempDir()
	markerDir := filepath.Join(runDir, downloadedArtifactsMarkerDir)
	require.NoError(t, os.MkdirAll(markerDir, 0o700))
	for _, marker := range []string{string(ArtifactSetAll), "agent", "agent-output-fallback"} {
		require.NoError(t, os.WriteFile(filepath.Join(markerDir, marker), nil, 0o600))
	}
	summary := &RunSummary{CLIVersion: GetVersion(), RunID: 42,
		RunAnalysis: RunAnalysis{Run: WorkflowRun{DatabaseID: 42}}}
	require.NoError(t, saveRunSummary(runDir, summary, false))

	done, skipped, err := renderCachedAuditIfAvailable(t.Context(), auditRunConfig{
		runID: 42, outputDir: runDir, artifactFilter: ResolveArtifactFilter([]string{"agent"}),
	})
	require.NoError(t, err)
	assert.False(t, done)
	assert.False(t, skipped)
	for _, marker := range []string{string(ArtifactSetAll), "agent", "agent-output-fallback"} {
		assert.NoFileExists(t, filepath.Join(markerDir, marker))
	}
}

func TestAuditNeedsDetectionArtifactWithUnifiedSession(t *testing.T) {
	t.Parallel()
	runDir := writeUnifiedDetectionTestSession(t, unifiedDetectionTestHeader+unifiedDetectionTestEvent(`{"jobResult":"success","conclusion":"warning","reason":"parse_error"}`))
	summary := &RunSummary{RunAnalysis: RunAnalysis{
		JobDetails: []JobInfoWithDuration{{JobInfo: JobInfo{Name: "detection", Conclusion: "success"}}},
	}}
	cfg := auditRunConfig{outputDir: runDir}
	assert.False(t, auditNeedsDetectionArtifact(cfg, summary))
	cfg.artifactFilter = []string{"detection"}
	assert.True(t, auditNeedsDetectionArtifact(cfg, summary))
}
