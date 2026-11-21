//go:build !integration

package cli

import (
	"context"
	"os"
	"os/exec"
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

func TestGenerateWorkQueueWorkersNeedNoEventWorkflow(t *testing.T) {
	repoRoot := t.TempDir()
	workflowDir := filepath.Join(repoRoot, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(workflowDir, "aw.json"), []byte("{"), 0o600))

	err := generateWorkQueueEventWorkflowWrapper(context.Background(), []*workflow.WorkflowData{{
		Name:  "Queue worker",
		Tools: map[string]any{"work-queue": map[string]any{"worker": true}},
	}}, workflowDir, repoRoot, true, false)

	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(workflowDir, "agentic-workqueue.yml"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestGenerateContinuationRelayWorkflowLazily(t *testing.T) {
	repoRoot := t.TempDir()
	workflowDir := filepath.Join(repoRoot, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowDir, 0o700))
	generatedPath := filepath.Join(workflowDir, workflow.ContinuationRelayWorkflowFile)
	enabled := []*workflow.WorkflowData{{
		Name:        "Continuable",
		SafeOutputs: &workflow.SafeOutputsConfig{RequestContinuation: &workflow.RequestContinuationConfig{}},
	}}

	require.NoError(t, generateContinuationRelayWorkflowWrapper(context.Background(), enabled, workflowDir, repoRoot, true, true))
	content, err := os.ReadFile(generatedPath)
	require.NoError(t, err)
	require.Contains(t, string(content), "parent_run_id")

	require.NoError(t, generateContinuationRelayWorkflowWrapper(context.Background(), []*workflow.WorkflowData{{Name: "Plain"}}, workflowDir, repoRoot, true, true))
	_, err = os.Stat(generatedPath)
	require.ErrorIs(t, err, os.ErrNotExist, "the relay is removed when no workflow requests continuation")
}

func TestCompileLegacyDurableGeneratesRelay(t *testing.T) {
	for _, targeted := range []bool{false, true} {
		name := "directory"
		if targeted {
			name = "targeted"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			workflowDir := filepath.Join(root, ".github", "workflows")
			require.NoError(t, os.MkdirAll(workflowDir, 0o700))
			cmd := exec.Command("git", "init", "-q", root)
			require.NoError(t, cmd.Run())
			previous, err := os.Getwd()
			require.NoError(t, err)
			require.NoError(t, os.Chdir(root))
			t.Cleanup(func() { require.NoError(t, os.Chdir(previous)) })
			source := "---\non: workflow_dispatch\nengine:\n  id: copilot\n  durable:\n    retention-days: 3\n---\nContinue the task.\n"
			require.NoError(t, os.WriteFile(filepath.Join(workflowDir, "durable.md"), []byte(source), 0o600))
			config := CompileConfig{}
			if targeted {
				config.MarkdownFiles = []string{"durable"}
			}
			data, err := CompileWorkflows(context.Background(), config)
			require.NoError(t, err)
			require.True(t, workflow.ContinuationRelayEnabled(data))
			lock, err := os.ReadFile(filepath.Join(workflowDir, "durable.lock.yml"))
			require.NoError(t, err)
			require.Contains(t, string(lock), `"continuation":{"workflow":"durable","max":5,"policy":"workflow-dispatch"}`)
			relay, err := os.ReadFile(filepath.Join(workflowDir, workflow.ContinuationRelayWorkflowFile))
			require.NoError(t, err)
			require.Contains(t, string(relay), "parent_run_id")
		})
	}
}
