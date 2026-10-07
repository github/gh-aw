//go:build !integration

package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddInteractiveConfig_determineFilesToAdd(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		workflowSpecs []string
		resolved      *ResolvedWorkflows
		wantFiles     []string
		wantErr       bool
	}{
		{
			name:          "single workflow",
			workflowSpecs: []string{"owner/repo/test-workflow"},
			wantFiles:     []string{"test-workflow.md", "test-workflow.lock.yml"},
			wantErr:       false,
		},
		{
			name:          "multiple workflows",
			workflowSpecs: []string{"owner/repo/workflow-one", "owner/repo/workflow-two"},
			wantFiles:     []string{"workflow-one.md", "workflow-one.lock.yml", "workflow-two.md", "workflow-two.lock.yml"},
			wantErr:       false,
		},
		{
			name:          "workflow with org/repo",
			workflowSpecs: []string{"owner/repo/workflow"},
			wantFiles:     []string{"workflow.md", "workflow.lock.yml"},
			wantErr:       false,
		},
		{
			name:          "invalid spec",
			workflowSpecs: []string{"invalid-spec"},
			wantErr:       true,
		},
		{
			name:          "repository package uses resolved workflows",
			workflowSpecs: []string{"owner/repo"},
			resolved: &ResolvedWorkflows{
				Workflows: []*ResolvedWorkflow{
					{
						Spec: &WorkflowSpec{WorkflowName: "review"},
					},
					{
						Spec: &WorkflowSpec{WorkflowName: "nightly-review"},
					},
				},
			},
			wantFiles: []string{"review.md", "review.lock.yml", "nightly-review.md", "nightly-review.lock.yml"},
			wantErr:   false,
		},
		{
			name:          "invalid resolved workflow fails loudly",
			workflowSpecs: []string{"owner/repo"},
			resolved: &ResolvedWorkflows{
				Workflows: []*ResolvedWorkflow{
					{},
				},
			},
			wantErr: true,
		},
		{
			name:          "resolved workflow with blank name fails loudly",
			workflowSpecs: []string{"owner/repo"},
			resolved: &ResolvedWorkflows{
				Workflows: []*ResolvedWorkflow{
					{
						Spec: &WorkflowSpec{WorkflowName: "   "},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			config := &AddInteractiveConfig{
				WorkflowSpecs:     tt.workflowSpecs,
				resolvedWorkflows: tt.resolved,
			}

			workflowFiles, initFiles, err := config.determineFilesToAdd()

			if tt.wantErr {
				assert.Error(t, err, "Expected error but got none")
			} else {
				require.NoError(t, err, "Unexpected error")
				assert.Equal(t, tt.wantFiles, workflowFiles, "Workflow files should match")
				assert.Empty(t, initFiles, "Init files should be empty")
			}
		})
	}
}

func TestAddInteractiveConfig_primaryWorkflowName(t *testing.T) {
	t.Parallel()
	t.Run("uses resolved workflow for repository package", func(t *testing.T) {
		t.Parallel()
		config := &AddInteractiveConfig{
			WorkflowSpecs: []string{"owner/repo"},
			resolvedWorkflows: &ResolvedWorkflows{
				Workflows: []*ResolvedWorkflow{
					{
						Spec: &WorkflowSpec{WorkflowName: "review"},
					},
				},
			},
		}

		assert.Equal(t, "review", config.primaryWorkflowName())
	})

	t.Run("falls back to parsed workflow spec", func(t *testing.T) {
		t.Parallel()
		config := &AddInteractiveConfig{
			WorkflowSpecs: []string{"owner/repo/test-workflow"},
		}

		assert.Equal(t, "test-workflow", config.primaryWorkflowName())
	})
}

func TestAddInteractiveConfig_welcomeMessage(t *testing.T) {
	t.Parallel()
	config := &AddInteractiveConfig{
		WorkflowSpecs: []string{"githubnext/agentics/repo-assist"},
		resolvedWorkflows: &ResolvedWorkflows{Workflows: []*ResolvedWorkflow{
			{Spec: &WorkflowSpec{WorkflowName: "repo-assist"}},
		}},
	}

	assert.Equal(t, `This tool will walk you through adding the automated workflow "repo-assist" from "githubnext/agentics/repo-assist".`, config.welcomeMessage())
	assert.Equal(t, "Source workflow: githubnext/agentics/repo-assist", config.sourceWorkflowMessage())
}

func TestAddInteractiveConfig_showLocalWriteInstructionsUsesWorkflowName(t *testing.T) {
	config := &AddInteractiveConfig{
		WorkflowSpecs: []string{"githubnext/agentics/repo-assist"},
		resolvedWorkflows: &ResolvedWorkflows{Workflows: []*ResolvedWorkflow{
			{Spec: &WorkflowSpec{WorkflowName: "repo-assist"}},
		}},
	}

	output := testutil.CaptureStderr(t, config.showLocalWriteInstructions)

	assert.Equal(t, 1, strings.Count(output, "written locally"), "local completion should be reported once")
	assert.Contains(t, output, "Workflow 'repo-assist' written locally; no pull request was created.")
	assert.Contains(t, output, "git add -A && git commit -m 'Add repo-assist'")
	assert.Contains(t, output, "gh aw run repo-assist  # Trigger the workflow")
	assert.NotContains(t, output, "Files written locally!")
	assert.NotContains(t, output, "gh aw run <workflow>")
}

func TestAddInteractiveConfig_showWorkflowDescriptions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name              string
		resolvedWorkflows *ResolvedWorkflows
		expectOutput      bool
	}{
		{
			name:              "nil resolved workflows",
			resolvedWorkflows: nil,
			expectOutput:      false,
		},
		{
			name: "empty workflows",
			resolvedWorkflows: &ResolvedWorkflows{
				Workflows: []*ResolvedWorkflow{},
			},
			expectOutput: false,
		},
		{
			name: "workflow with description",
			resolvedWorkflows: &ResolvedWorkflows{
				Workflows: []*ResolvedWorkflow{
					{
						Description: "Test workflow description",
					},
				},
			},
			expectOutput: true,
		},
		{
			name: "workflow without description",
			resolvedWorkflows: &ResolvedWorkflows{
				Workflows: []*ResolvedWorkflow{
					{
						Description: "",
					},
				},
			},
			expectOutput: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			config := &AddInteractiveConfig{
				resolvedWorkflows: tt.resolvedWorkflows,
			}

			// This function prints to stderr, so we just verify it doesn't panic
			require.NotPanics(t, func() {
				config.showWorkflowDescriptions()
			}, "showWorkflowDescriptions should not panic")
		})
	}
}

func TestAddInteractiveConfig_showFinalInstructions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name              string
		resolvedWorkflows *ResolvedWorkflows
	}{
		{
			name:              "no workflows",
			resolvedWorkflows: nil,
		},
		{
			name: "with workflow",
			resolvedWorkflows: &ResolvedWorkflows{
				Workflows: []*ResolvedWorkflow{
					{
						Spec: &WorkflowSpec{
							WorkflowName: "test-workflow",
						},
						Description: "Test description",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			config := &AddInteractiveConfig{
				resolvedWorkflows: tt.resolvedWorkflows,
			}

			// This function prints to stderr, so we just verify it doesn't panic
			require.NotPanics(t, func() {
				config.showFinalInstructions()
			}, "showFinalInstructions should not panic")
		})
	}
}

func TestAddInteractiveConfig_createWorkflowChangesLocallyDoesNotRequireCleanTreeOrCreatePR(t *testing.T) {
	tmpDir := t.TempDir()
	oldWd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() {
		require.NoError(t, os.Chdir(oldWd))
	}()

	gitInit := exec.Command("git", "init")
	gitInit.Dir = tmpDir
	require.NoError(t, gitInit.Run())
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "existing-change.txt"), []byte("dirty tree"), 0o644))

	fakeGH := filepath.Join(tmpDir, "gh")
	require.NoError(t, os.WriteFile(fakeGH, []byte("#!/bin/sh\necho unexpected gh invocation >&2\nexit 42\n"), 0o755))
	t.Setenv("PATH", tmpDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	config := &AddInteractiveConfig{
		WorkflowSpecs: []string{"owner/repo/test-workflow"},
		resolvedWorkflows: &ResolvedWorkflows{
			Workflows: []*ResolvedWorkflow{
				{
					Spec: &WorkflowSpec{
						RepoSpec: RepoSpec{
							RepoSlug: "owner/repo",
						},
						WorkflowName: "test-workflow",
						WorkflowPath: "test.md",
					},
					Content: []byte("---\non:\n  workflow_dispatch:\n---\n# Test workflow\n"),
				},
			},
			HasWorkflowDispatch: true,
		},
	}

	err = config.createWorkflowChangesAndConfigureSecret(context.Background(), []string{"test-workflow.md", "test-workflow.lock.yml"}, nil, "COPILOT_GITHUB_TOKEN", "secret", false)
	require.NoError(t, err)

	require.NotNil(t, config.addResult)
	assert.Zero(t, config.addResult.PRNumber)
	assert.Empty(t, config.addResult.PRURL)
	assert.True(t, config.addResult.HasWorkflowDispatch)

	workflowPath := filepath.Join(tmpDir, ".github", "workflows", "test-workflow.md")
	_, err = os.Stat(workflowPath)
	require.NoError(t, err, "workflow should be written locally")
}

// TestAddInteractiveConfig_prepareAndConfirmAddInteractive_localWriteSkipsSecretsAndPRSteps
// drives the actual orchestration in prepareAndConfirmAddInteractive (not just the
// downstream write helper) with a stubbed "No, write files locally" answer to the
// PR-vs-local prompt. It asserts that choosing local writes never invokes any gh
// mutation (secret upload, PR creation/merge) and that no secret is returned for the
// caller to configure.
func TestAddInteractiveConfig_prepareAndConfirmAddInteractive_localWriteSkipsSecretsAndPRSteps(t *testing.T) {
	tmpDir := t.TempDir()
	oldWd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() {
		require.NoError(t, os.Chdir(oldWd))
	}()

	gitInit := exec.Command("git", "init")
	gitInit.Dir = tmpDir
	require.NoError(t, gitInit.Run())

	// A fake gh that records every invocation instead of touching the network. Reads
	// (e.g. listing existing secrets) are expected and tolerated by the caller, but any
	// mutating invocation (secret set, pr create/merge) must never happen on the local
	// write path.
	ghLog := filepath.Join(tmpDir, "gh-invocations.log")
	fakeGH := filepath.Join(tmpDir, "gh")
	script := "#!/bin/sh\necho \"$@\" >> " + ghLog + "\nexit 0\n"
	require.NoError(t, os.WriteFile(fakeGH, []byte(script), 0o755))
	t.Setenv("PATH", tmpDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	originalConfirmAuthoringSupport := addConfirmAuthoringSupport
	originalConfirmChanges := addConfirmChanges
	originalMissingInitMarkers := addMissingInitMarkers
	originalInitRepository := addInitRepository
	initializationRan := false
	confirmationRan := false
	addConfirmChanges = func(c *AddInteractiveConfig, workflowFiles, initFiles []string) (bool, error) {
		confirmationRan = true
		assert.Equal(t, []string{"test-workflow.md", "test-workflow.lock.yml"}, workflowFiles)
		assert.Contains(t, initFiles, bootstrapAgenticSkillPath)
		return false, nil
	}
	addConfirmAuthoringSupport = func(context.Context) (bool, error) { return true, nil }
	addMissingInitMarkers = func(string, string) ([]string, error) {
		return []string{bootstrapAgenticSkillPath}, nil
	}
	addInitRepository = func(InitOptions) error {
		initializationRan = true
		return nil
	}
	t.Cleanup(func() {
		addConfirmAuthoringSupport = originalConfirmAuthoringSupport
		addConfirmChanges = originalConfirmChanges
		addMissingInitMarkers = originalMissingInitMarkers
		addInitRepository = originalInitRepository
	})

	config := &AddInteractiveConfig{
		Ctx:            context.Background(),
		WorkflowSpecs:  []string{"owner/repo/test-workflow"},
		EngineOverride: "copilot",
		SkipSecret:     true,
		hasWriteAccess: true,
		RepoOverride:   "owner/repo",
		resolvedWorkflows: &ResolvedWorkflows{
			Workflows: []*ResolvedWorkflow{
				{
					Spec: &WorkflowSpec{
						RepoSpec:     RepoSpec{RepoSlug: "owner/repo"},
						WorkflowName: "test-workflow",
						WorkflowPath: "test.md",
					},
					Content: []byte("---\non:\n  workflow_dispatch:\n---\n# Test workflow\n"),
				},
			},
			HasWorkflowDispatch: true,
		},
	}

	workflowFiles, initFiles, secretName, secretValue, createPR, err := config.prepareAndConfirmAddInteractive()
	require.NoError(t, err)

	assert.True(t, confirmationRan, "delivery confirmation must run before choosing local writes")
	assert.False(t, createPR, "choosing local writes should report createPR=false")
	assert.Empty(t, secretName, "local writes must not resolve a secret to configure")
	assert.Empty(t, secretValue, "local writes must not resolve a secret value")
	assert.NotEmpty(t, workflowFiles, "workflow files should still be determined for local writes")
	assert.Empty(t, initFiles, "local writes must not include repository initialization files")
	assert.False(t, initializationRan, "local writes must not initialize repository support files")

	if _, statErr := os.Stat(ghLog); statErr == nil {
		logContent, readErr := os.ReadFile(ghLog)
		require.NoError(t, readErr)
		assert.NotContains(t, string(logContent), "secret set", "local write path must never upload a repository secret")
		assert.NotContains(t, string(logContent), "pr create", "local write path must never create a pull request")
		assert.NotContains(t, string(logContent), "pr merge", "local write path must never merge a pull request")
	}
}
