//go:build !integration

package workflow

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateWorkQueueEventWorkflowWhenIssueMirroringIsEnabled(t *testing.T) {
	t.Setenv("GH_AW_ACTION_MODE", "dev")
	dir := t.TempDir()
	data := []*WorkflowData{{
		Tools: map[string]any{"work-queue": map[string]any{"issues": true}},
	}}

	require.NoError(t, GenerateWorkQueueEventWorkflow(context.Background(), data, dir, nil))
	content, err := os.ReadFile(filepath.Join(dir, workQueueEventWorkflowFilename))
	require.NoError(t, err)
	require.Contains(t, string(content), "name: Agentic Workqueue")
	require.Contains(t, string(content), "  issues:\n    types: [closed]")
	require.Contains(t, string(content), "  issue_comment:\n    types: [created]")
	require.Contains(t, string(content), "cancel_work_queue_closed_issue.cjs")
}

func TestGenerateWorkQueueEventWorkflowRemovesWhenIssueMirroringIsUnused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, workQueueEventWorkflowFilename)
	require.NoError(t, os.WriteFile(path, []byte("stale"), 0o600))

	require.NoError(t, GenerateWorkQueueEventWorkflow(context.Background(), []*WorkflowData{{
		Tools: map[string]any{"work-queue": true},
	}}, dir, nil))
	_, err := os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestGenerateWorkQueueEventWorkflowReconcilesWorkersWithoutIssueMirroring(t *testing.T) {
	t.Setenv("GH_AW_ACTION_MODE", "dev")
	dir := t.TempDir()
	worker := func(name string) *WorkflowData {
		return &WorkflowData{Name: name, Tools: map[string]any{"work-queue": map[string]any{"worker": true}}}
	}
	data := []*WorkflowData{worker("Zulu worker"), worker("Alpha worker"), worker("Alpha worker"), worker("Agentic Workqueue"), nil,
		{Name: "Observer", Tools: map[string]any{"work-queue": true}},
	}
	require.NoError(t, GenerateWorkQueueEventWorkflow(context.Background(), data, dir, nil))
	content, err := os.ReadFile(filepath.Join(dir, workQueueEventWorkflowFilename))
	require.NoError(t, err)
	yaml := string(content)
	require.Contains(t, yaml, "    workflows:\n      - \"Alpha worker\"\n      - \"Zulu worker\"\n    types: [completed]")
	require.NotContains(t, yaml, "      - \"Agentic Workqueue\"")
	require.NotContains(t, yaml, "Observer")
	require.NotContains(t, yaml, "issue_events:")
	require.NotContains(t, yaml, "issues: write")
	require.Contains(t, yaml, "reconcile_work_queue_run.cjs")
	require.Contains(t, yaml, "persist-credentials: false")
	require.Contains(t, yaml, "github.event.workflow_run.repository.id == github.repository_id")
	require.NotContains(t, yaml, "workflow_run.head_sha")
	require.NotContains(t, yaml, "download-artifact")
}

func TestGenerateWorkQueueEventWorkflowSeparatesIssueAndRunEvents(t *testing.T) {
	t.Setenv("GH_AW_ACTION_MODE", "dev")
	dir := t.TempDir()
	data := []*WorkflowData{{
		Name: "Queue worker",
		Tools: map[string]any{"work-queue": map[string]any{
			"require-assignment": true, "issues": true,
		}},
	}}
	require.NoError(t, GenerateWorkQueueEventWorkflow(context.Background(), data, dir, nil))
	content, err := os.ReadFile(filepath.Join(dir, workQueueEventWorkflowFilename))
	require.NoError(t, err)
	require.Contains(t, string(content), "github.event_name != 'workflow_run'")
	require.Contains(t, string(content), "github.event_name == 'workflow_run'")
	require.Contains(t, string(content), "      - \"Queue worker\"")
	require.Contains(t, string(content), "cancel_work_queue_closed_issue.cjs")
}
