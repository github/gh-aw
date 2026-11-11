//go:build !integration

package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/require"
)

func TestAddCommandUpdatesGitAttributes(t *testing.T) {
	tests := []struct {
		name            string
		existing        *string
		noGitattributes bool
		wantEntry       bool
		wantContent     *string
		addTwice        bool
	}{
		{
			name:        "adds_entry_by_default",
			wantEntry:   true,
			wantContent: new(constants.WorkflowsLockYmlGitAttributesEntry),
		},
		{
			name:            "does_not_create_file_with_flag",
			noGitattributes: true,
		},
		{
			name:            "preserves_existing_file_with_flag",
			existing:        new("*.txt linguist-vendored=true\n"),
			noGitattributes: true,
			wantContent:     new("*.txt linguist-vendored=true\n"),
		},
		{
			name:        "appends_entry_and_preserves_existing_lines",
			existing:    new("*.txt linguist-vendored=true\n"),
			wantEntry:   true,
			wantContent: new("*.txt linguist-vendored=true\n\n" + constants.WorkflowsLockYmlGitAttributesEntry),
		},
		{
			name:        "does_not_duplicate_entry",
			wantEntry:   true,
			wantContent: new(constants.WorkflowsLockYmlGitAttributesEntry),
			addTwice:    true,
		},
		{
			name:        "replaces_legacy_entry",
			existing:    new("*.txt linguist-vendored=true\n" + constants.WorkflowsLockYmlGitAttributesEntryLegacy + "\n"),
			wantEntry:   true,
			wantContent: new("*.txt linguist-vendored=true\n" + constants.WorkflowsLockYmlGitAttributesEntry + "\n"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			t.Chdir(tmpDir)

			if err := exec.Command("git", "init").Run(); err != nil {
				t.Skip("Skipping test - git not available")
			}
			if err := exec.Command("git", "config", "user.email", "test@example.com").Run(); err != nil {
				t.Skip("Skipping test - git config failed")
			}
			if err := exec.Command("git", "config", "user.name", "Test User").Run(); err != nil {
				t.Skip("Skipping test - git config failed")
			}

			if tt.existing != nil {
				require.NoError(t, os.WriteFile(".gitattributes", []byte(*tt.existing), 0o644))
			}

			resolved := newTestResolvedWorkflow(`---
on: push
permissions:
  contents: read
engine: copilot
---

# Test Workflow

This is a test workflow.`)
			opts := AddOptions{NoGitattributes: tt.noGitattributes, Quiet: true}
			require.NoError(t, addWorkflows(context.Background(), []*ResolvedWorkflow{resolved}, opts))

			workflowPath := filepath.Join(".github", "workflows", "test.md")
			require.FileExists(t, workflowPath)
			require.FileExists(t, filepath.Join(".github", "workflows", "test.lock.yml"))

			gitAttributesPath := ".gitattributes"
			if tt.wantContent == nil {
				_, err := os.Stat(gitAttributesPath)
				require.ErrorIs(t, err, os.ErrNotExist)
				return
			}

			content, err := os.ReadFile(gitAttributesPath)
			require.NoError(t, err)
			require.Equal(t, *tt.wantContent, string(content))
			require.Equal(t, tt.wantEntry, strings.Contains(string(content), constants.WorkflowsLockYmlGitAttributesEntry))

			if tt.addTwice {
				opts.Force = true
				require.NoError(t, addWorkflows(context.Background(), []*ResolvedWorkflow{resolved}, opts))

				content, err = os.ReadFile(gitAttributesPath)
				require.NoError(t, err)
				require.Equal(t, *tt.wantContent, string(content))
				require.Equal(t, 1, strings.Count(string(content), constants.WorkflowsLockYmlGitAttributesEntry))
			}
		})
	}
}

func TestAddWorkflowDependencyNoGitattributes(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)
	require.NoError(t, exec.Command("git", "init").Run())

	workflowsDir := filepath.Join(".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0o755))
	mainPath := filepath.Join(workflowsDir, "dispatcher.md")
	workerPath := filepath.Join(workflowsDir, "worker.md")
	require.NoError(t, os.WriteFile(mainPath, []byte(`---
name: Dispatcher
on:
  workflow_dispatch:
safe-outputs:
  dispatch-workflow:
    workflows: [worker]
---

# Dispatcher
`), 0o644))
	require.NoError(t, os.WriteFile(workerPath, []byte(`---
name: Worker
on:
  workflow_dispatch:
---

# Worker
`), 0o644))

	compileDispatchWorkflowDependenciesWithOptions(context.Background(), mainPath, compileDepsOptions{
		quiet:           true,
		noGitattributes: true,
	})

	require.FileExists(t, filepath.Join(workflowsDir, "worker.lock.yml"))
	require.NoFileExists(t, ".gitattributes")
}

func newTestResolvedWorkflow(content string) *ResolvedWorkflow {
	return &ResolvedWorkflow{
		Spec: &WorkflowSpec{
			RepoSpec: RepoSpec{
				RepoSlug: "test/repo",
			},
			WorkflowPath: "./test.md",
			WorkflowName: "test",
		},
		Content: []byte(content),
		SourceInfo: &FetchedWorkflow{
			Content:    []byte(content),
			IsLocal:    true,
			SourcePath: "./test.md",
		},
	}
}
