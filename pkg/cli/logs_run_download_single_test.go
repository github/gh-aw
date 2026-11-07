//go:build !integration

package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsAgenticWorkflowPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "agentic workflow",
			path: ".github/workflows/research.lock.yml",
			want: true,
		},
		{
			name: "regular yml workflow",
			path: ".github/workflows/ci.yml",
			want: false,
		},
		{
			name: "regular yaml workflow",
			path: ".github/workflows/ci.yaml",
			want: false,
		},
		{
			name: "missing workflow path",
			path: "",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isAgenticWorkflowPath(tt.path))
		})
	}
}

func TestInferMissingWorkflowPathFromAwInfo(t *testing.T) {
	runDir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(runDir, "aw_info.json"),
		[]byte(`{"workflow_name":"Weekly Research"}`),
		0o600,
	))
	result := &DownloadResult{}

	inferMissingWorkflowPath(result, runDir)

	assert.Equal(t, ".github/workflows/weekly-research.lock.yml", result.Run.WorkflowPath)
	assert.True(t, isAgenticWorkflowPath(result.Run.WorkflowPath))
}

func TestSkipUnverifiedWorkflowRun(t *testing.T) {
	t.Run("pathless failure is skipped", func(t *testing.T) {
		result := &DownloadResult{RunAnalysis: RunAnalysis{Run: WorkflowRun{DatabaseID: 1}}}

		skipUnverifiedWorkflowRun(result, false)

		assert.True(t, result.Skipped)
	})

	t.Run("agentic failure is retained", func(t *testing.T) {
		result := &DownloadResult{RunAnalysis: RunAnalysis{Run: WorkflowRun{
			DatabaseID:   2,
			WorkflowPath: ".github/workflows/research.lock.yml",
		}}}

		skipUnverifiedWorkflowRun(result, false)

		assert.False(t, result.Skipped)
	})
}

func TestIsFailureWithoutArtifacts(t *testing.T) {
	assert.True(t, isFailureWithoutArtifacts(ErrNoArtifacts, "failure"))
	assert.False(t, isFailureWithoutArtifacts(ErrNoArtifacts, "success"))
	assert.False(t, isFailureWithoutArtifacts(errors.New("download failed"), "failure"))
}

func TestHandleArtifactDownloadErrorRetainsFailureWithoutArtifacts(t *testing.T) {
	result := &DownloadResult{RunAnalysis: RunAnalysis{Run: WorkflowRun{Conclusion: "failure"}}}

	handleArtifactDownloadError(result, ErrNoArtifacts, false)

	assert.False(t, result.Skipped)
	assert.Empty(t, result.Metrics)
}

func TestApplyKnownWorkflowPathOverridesCachedPath(t *testing.T) {
	result := &DownloadResult{RunAnalysis: RunAnalysis{Run: WorkflowRun{
		WorkflowPath: ".github/workflows/stale.lock.yml",
	}}}

	applyKnownWorkflowPath(result, WorkflowRun{WorkflowPath: ".github/workflows/ci.yml"})

	assert.Equal(t, ".github/workflows/ci.yml", result.Run.WorkflowPath)
}
