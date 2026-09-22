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
	require.NoError(t, os.MkdirAll(filepath.Join(runDir, "detection"), 0o700))
	require.NoError(t, os.WriteFile(
		filepath.Join(runDir, "detection", "detection_result.json"),
		[]byte(`{"prompt_injection":false,"secret_leak":false,"malicious_patch":false}`),
		0o600,
	))
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
