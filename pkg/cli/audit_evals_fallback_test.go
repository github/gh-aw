//go:build !integration

package cli

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAuditRunConfigMarksExplicitEvalsArtifactRequest(t *testing.T) {
	t.Parallel()
	cfg, err := newAuditRunConfig(123, AuditOptions{
		ArtifactSets: []string{"evals"},
		EvalsOnly:    false,
	})
	require.NoError(t, err)
	assert.True(t, cfg.evalsArtifactRequested)
}

func TestRenderCachedAuditIfAvailableBypassesCacheWhenExplicitEvalsArtifactRequested(t *testing.T) {
	t.Parallel()
	runOutputDir := t.TempDir()
	summary := &RunSummary{
		CLIVersion:  GetVersion(),
		RunID:       123,
		ProcessedAt: time.Now(),
		RunAnalysis: RunAnalysis{
			Run: WorkflowRun{
				DatabaseID: 123,
			},
		},
	}
	require.NoError(t, saveRunSummary(runOutputDir, summary, false))

	done, _, err := renderCachedAuditIfAvailable(context.Background(), auditRunConfig{
		runID:                  123,
		outputDir:              runOutputDir,
		verbose:                false,
		evalsArtifactRequested: true,
	})
	require.NoError(t, err)
	assert.False(t, done, "cache should be bypassed so legacy evals fallback can run")
}
