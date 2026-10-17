//go:build !integration

package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/require"
)

func TestGenerateWorkQueueEventWorkflowSkipsRepoConfigWhenIssueMirroringIsUnused(t *testing.T) {
	repoRoot := t.TempDir()
	workflowDir := filepath.Join(repoRoot, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(workflowDir, "aw.json"), []byte("{"), 0o600))

	generatedPath := filepath.Join(workflowDir, "agentic-workqueue.yml")
	require.NoError(t, os.WriteFile(generatedPath, []byte("stale"), 0o600))

	err := generateWorkQueueEventWorkflowWrapper(context.Background(), []*workflow.WorkflowData{{
		Tools: map[string]any{"work-queue": true},
	}}, workflowDir, repoRoot, true, false)

	require.NoError(t, err)
	_, err = os.Stat(generatedPath)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestGenerateWorkQueueWorkerEventsHonorRepoConfiguration(t *testing.T) {
	repoRoot := t.TempDir()
	workflowDir := filepath.Join(repoRoot, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(workflowDir, "aw.json"), []byte("{"), 0o600))

	err := generateWorkQueueEventWorkflowWrapper(context.Background(), []*workflow.WorkflowData{{
		Name:  "Queue worker",
		Tools: map[string]any{"work-queue": map[string]any{"worker": true}},
	}}, workflowDir, repoRoot, true, false)

	require.ErrorContains(t, err, "failed to load repo config for work queue event workflow")
}
