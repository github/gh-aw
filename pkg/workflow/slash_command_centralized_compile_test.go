//go:build !integration

package workflow

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/require"
)

func TestCompileWorkflow_CentralizedSlashCommandWithPullRequest(t *testing.T) {
	tmpDir := testutil.TempDir(t, "workflow-centralized-slash-pr-test")
	markdownPath := filepath.Join(tmpDir, "matt.md")
	content := `---
on:
  pull_request:
    types: [ready_for_review]
  slash_command:
    name: matt
    events: [pull_request_comment, pull_request_review_comment]
    strategy: centralized
tools:
  github:
    allowed: [list_issues]
---

# Matt
`
	require.NoError(t, os.WriteFile(markdownPath, []byte(content), 0644))

	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(markdownPath)
	require.NoError(t, err)
	require.NoError(t, compiler.CompileWorkflowData(workflowData, markdownPath))

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(markdownPath))
	require.NoError(t, err)
	require.Contains(t, string(lockContent), "cancel-in-progress: ${{ github.event_name == 'pull_request' }}")
	require.NotContains(t, string(lockContent), "queue: ${{")

	require.NoError(t, GenerateCentralSlashCommandWorkflow(context.Background(), []*WorkflowData{workflowData}, tmpDir, nil))
	routerContent, err := os.ReadFile(filepath.Join(tmpDir, centralSlashCommandWorkflowFilename))
	require.NoError(t, err)
	require.Contains(t, string(routerContent), `"matt":[{"workflow":"matt","events":["pull_request_comment","pull_request_review_comment"]`)
}

func TestCompileWorkflow_SlashCommandCentralizedStrategy(t *testing.T) {
	tmpDir := testutil.TempDir(t, "workflow-centralized-slash-test")

	markdownPath := filepath.Join(tmpDir, "deploy.md")
	content := `---
on:
  slash_command:
    name: deploy
    strategy: centralized
  push:
    branches: [main]
tools:
  github:
    allowed: [list_issues]
---

# Deploy
`
	require.NoError(t, os.WriteFile(markdownPath, []byte(content), 0644))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(markdownPath))

	lockPath := stringutil.MarkdownToLockFile(markdownPath)
	lockContent, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	compiled := string(lockContent)

	require.Contains(t, compiled, "workflow_dispatch:")
	require.Contains(t, compiled, "push:")
	require.NotContains(t, compiled, "issue_comment:")
	require.NotContains(t, compiled, "pull_request_review_comment:")
	require.NotContains(t, compiled, "startsWith(github.event.comment.body")
}

func TestCompileWorkflow_SlashCommandCentralizedWithLabelCommand(t *testing.T) {
	tmpDir := testutil.TempDir(t, "workflow-centralized-slash-label-test")

	markdownPath := filepath.Join(tmpDir, "triage.md")
	content := `---
on:
  slash_command:
    name: triage
    strategy: centralized
  label_command:
    name: triage
    events: [issues]
tools:
  github:
    allowed: [list_issues]
---

# Triage
`
	require.NoError(t, os.WriteFile(markdownPath, []byte(content), 0644))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(markdownPath))

	lockPath := stringutil.MarkdownToLockFile(markdownPath)
	lockContent, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	compiled := string(lockContent)

	require.Contains(t, compiled, "on:\n  workflow_dispatch:")
	require.Contains(t, compiled, "workflow_dispatch:")
	require.NotContains(t, compiled, "\n  issues:\n    types:")
	require.Contains(t, compiled, "github.event_name == 'workflow_dispatch'")
	require.Contains(t, compiled, "fromJSON(github.event.inputs.aw_context || '{}').event_type == 'issue_comment'")
	require.Contains(t, compiled, "fromJSON(github.event.inputs.aw_context || '{}').trigger_label == 'triage'")
	require.Contains(t, compiled, "fromJSON(github.event.inputs.aw_context || '{}').event_type == 'issues'")
}

func TestCompileWorkflow_SlashCommandRejectsRequiredDispatchInputs(t *testing.T) {
	tmpDir := testutil.TempDir(t, "workflow-centralized-slash-dispatch-inputs-test")

	markdownPath := filepath.Join(tmpDir, "scout.md")
	content := `---
on:
  slash_command:
    name: scout
    strategy: centralized
  workflow_dispatch:
    inputs:
      topic:
        description: "Research topic"
        required: true
        type: string
tools:
  github:
    allowed: [list_issues]
---

# Scout
`
	require.NoError(t, os.WriteFile(markdownPath, []byte(content), 0644))

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(markdownPath)
	require.Error(t, err)
	require.ErrorContains(t, err, "on.workflow_dispatch.inputs.topic.required: true is not allowed when using slash_command")

	lockPath := stringutil.MarkdownToLockFile(markdownPath)
	_, statErr := os.Stat(lockPath)
	require.Error(t, statErr)
	require.True(t, os.IsNotExist(statErr))
}

func TestCompileWorkflow_LabelCommandRejectsRequiredDispatchInputs(t *testing.T) {
	tmpDir := testutil.TempDir(t, "workflow-label-dispatch-inputs-test")

	markdownPath := filepath.Join(tmpDir, "triage.md")
	content := `---
on:
  label_command:
    name: triage
  workflow_dispatch:
    inputs:
      topic:
        description: "Research topic"
        required: true
        type: string
tools:
  github:
    allowed: [list_issues]
---

# Triage
`
	require.NoError(t, os.WriteFile(markdownPath, []byte(content), 0644))

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(markdownPath)
	require.Error(t, err)
	require.ErrorContains(t, err, "on.workflow_dispatch.inputs.topic.required: true is not allowed when using label_command")

	lockPath := stringutil.MarkdownToLockFile(markdownPath)
	_, statErr := os.Stat(lockPath)
	require.Error(t, statErr)
	require.True(t, os.IsNotExist(statErr))
}
