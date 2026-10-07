//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInjectWorkflowCallOutputs(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name            string
		onSection       string
		safeOutputs     *SafeOutputsConfig
		expectContains  []string
		expectAbsent    []string
		expectUnchanged bool
	}{
		{
			name: "no workflow_call - no injection",
			onSection: `"on":
  push:
  workflow_dispatch:`,
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
			},
			expectUnchanged: true,
		},
		{
			name: "nil safe outputs - no injection",
			onSection: `"on":
  workflow_call:`,
			safeOutputs:     nil,
			expectUnchanged: true,
		},
		{
			name: "empty SafeOutputsConfig - no injection",
			onSection: `"on":
  workflow_call:`,
			safeOutputs:     &SafeOutputsConfig{},
			expectUnchanged: true,
		},
		{
			name: "workflow_call with create-issue outputs",
			onSection: `"on":
  workflow_call:
  workflow_dispatch:`,
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
			},
			expectContains: []string{
				"workflow_call:",
				"outputs:",
				"created_issue_number:",
				"created_issue_url:",
				"jobs.safe_outputs.outputs.created_issue_number",
				"jobs.safe_outputs.outputs.created_issue_url",
			},
		},
		{
			name: "workflow_call with create-pull-request outputs",
			onSection: `"on":
  workflow_call:`,
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{},
			},
			expectContains: []string{
				"created_pr_number:",
				"created_pr_url:",
				"jobs.safe_outputs.outputs.created_pr_number",
				"jobs.safe_outputs.outputs.created_pr_url",
			},
			expectAbsent: []string{
				"created_issue_number:",
			},
		},
		{
			name: "workflow_call with add-comment outputs",
			onSection: `"on":
  workflow_call:`,
			safeOutputs: &SafeOutputsConfig{
				AddComments: &AddCommentsConfig{},
			},
			expectContains: []string{
				"comment_id:",
				"comment_url:",
				"jobs.safe_outputs.outputs.comment_id",
				"jobs.safe_outputs.outputs.comment_url",
			},
		},
		{
			name: "workflow_call with push-to-pull-request-branch outputs",
			onSection: `"on":
  workflow_call:`,
			safeOutputs: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
			},
			expectContains: []string{
				"push_commit_sha:",
				"push_commit_url:",
				"jobs.safe_outputs.outputs.push_commit_sha",
				"jobs.safe_outputs.outputs.push_commit_url",
			},
		},
		{
			name: "workflow_call with multiple safe output types",
			onSection: `"on":
  workflow_call:
  issues:
    types: [opened]`,
			safeOutputs: &SafeOutputsConfig{
				CreateIssues:       &CreateIssuesConfig{},
				CreatePullRequests: &CreatePullRequestsConfig{},
				AddComments:        &AddCommentsConfig{},
			},
			expectContains: []string{
				"created_issue_number:",
				"created_issue_url:",
				"created_pr_number:",
				"created_pr_url:",
				"comment_id:",
				"comment_url:",
			},
		},
		{
			name: "workflow_call with no relevant safe output types",
			onSection: `"on":
  workflow_call:`,
			safeOutputs: &SafeOutputsConfig{
				AssignToAgent: &AssignToAgentConfig{},
			},
			expectUnchanged: true,
		},
		{
			name: "schedule sequence stays indented and cron stays quoted",
			onSection: `"on":
  schedule:
    - cron: "0 0 */2 * *"
  workflow_call:`,
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
			},
			expectContains: []string{
				"  schedule:\n    - cron: \"0 0 */2 * *\"",
			},
			expectAbsent: []string{
				"  schedule:\n  - cron:",
				"- cron: 0 0",
			},
		},
		{
			name: "user-defined outputs are preserved when merged",
			onSection: `"on":
  workflow_call:
    outputs:
      my_custom_output:
        description: Custom output
        value: ${{ jobs.my_job.outputs.my_value }}`,
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
			},
			expectContains: []string{
				"my_custom_output:",
				"created_issue_number:",
				"created_issue_url:",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compiler.injectWorkflowCallOutputs(tt.onSection, tt.safeOutputs)

			if tt.expectUnchanged {
				assert.Equal(t, tt.onSection, result, "on section should be unchanged")
				return
			}

			require.NotEmpty(t, result, "result should not be empty")

			for _, expected := range tt.expectContains {
				assert.Contains(t, result, expected,
					"expected result to contain %q", expected)
			}
			for _, absent := range tt.expectAbsent {
				assert.NotContains(t, result, absent,
					"expected result NOT to contain %q", absent)
			}
		})
	}
}

func TestBuildWorkflowCallOutputsMap(t *testing.T) {
	tests := []struct {
		name        string
		safeOutputs *SafeOutputsConfig
		expectKeys  []string
		absentKeys  []string
	}{
		{
			name:        "create-issue adds 2 outputs",
			safeOutputs: &SafeOutputsConfig{CreateIssues: &CreateIssuesConfig{}},
			expectKeys:  []string{"created_issue_number", "created_issue_url"},
			absentKeys:  []string{"created_pr_number", "comment_id", "push_commit_sha"},
		},
		{
			name:        "create-pull-request adds 2 outputs",
			safeOutputs: &SafeOutputsConfig{CreatePullRequests: &CreatePullRequestsConfig{}},
			expectKeys:  []string{"created_pr_number", "created_pr_url"},
			absentKeys:  []string{"created_issue_number"},
		},
		{
			name:        "add-comment adds 2 outputs",
			safeOutputs: &SafeOutputsConfig{AddComments: &AddCommentsConfig{}},
			expectKeys:  []string{"comment_id", "comment_url"},
		},
		{
			name:        "push-to-pull-request-branch adds 2 outputs",
			safeOutputs: &SafeOutputsConfig{PushToPullRequestBranch: &PushToPullRequestBranchConfig{}},
			expectKeys:  []string{"push_commit_sha", "push_commit_url"},
		},
		{
			name:        "no relevant types returns empty map",
			safeOutputs: &SafeOutputsConfig{AssignToAgent: &AssignToAgentConfig{}},
			absentKeys:  []string{"created_issue_number", "created_pr_number", "comment_id", "push_commit_sha"},
		},
		{
			name: "multiple types produce all outputs",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues:            &CreateIssuesConfig{},
				CreatePullRequests:      &CreatePullRequestsConfig{},
				AddComments:             &AddCommentsConfig{},
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
			},
			expectKeys: []string{
				"created_issue_number", "created_issue_url",
				"created_pr_number", "created_pr_url",
				"comment_id", "comment_url",
				"push_commit_sha", "push_commit_url",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildWorkflowCallOutputsMap(tt.safeOutputs)

			for _, key := range tt.expectKeys {
				entry, ok := result[key]
				assert.True(t, ok, "expected key %q in outputs map", key)
				assert.NotEmpty(t, entry.Description, "description for %q should not be empty", key)
				assert.NotEmpty(t, entry.Value, "value for %q should not be empty", key)
				assert.Contains(t, entry.Value, "jobs.safe_outputs.outputs.",
					"value for %q should reference safe_outputs job", key)
			}
			for _, key := range tt.absentKeys {
				_, ok := result[key]
				assert.False(t, ok, "key %q should not be in outputs map", key)
			}
		})
	}
}

// TestWorkflowCallOutputsEndToEnd tests that compiling a workflow with workflow_call + safe-outputs
// produces the correct on.workflow_call.outputs section in the compiled YAML.
func TestWorkflowCallOutputsEndToEnd(t *testing.T) {
	workflowContent := `---
on:
  workflow_call:
  workflow_dispatch:
engine: copilot
safe-outputs:
  create-issue:
    title-prefix: "[ai] "
  add-comment:
    max: 1
---

# Test Workflow

Create an issue and add a comment.
`

	tmpDir := testutil.TempDir(t, "workflow-call-outputs-test")
	testFile := filepath.Join(tmpDir, "test.md")
	err := os.WriteFile(testFile, []byte(workflowContent), 0644)
	require.NoError(t, err, "failed to write test file")

	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err, "failed to parse workflow file")

	yamlOutput, _, _, err := compiler.generateYAML(workflowData, testFile)
	require.NoError(t, err, "failed to generate YAML")

	// Verify workflow_call outputs section is present
	assert.Contains(t, yamlOutput, "workflow_call:", "should contain workflow_call trigger")
	assert.Contains(t, yamlOutput, "outputs:", "should contain outputs section")

	// Verify create-issue outputs
	assert.Contains(t, yamlOutput, "created_issue_number:", "should contain created_issue_number")
	assert.Contains(t, yamlOutput, "created_issue_url:", "should contain created_issue_url")
	assert.Contains(t, yamlOutput, "jobs.safe_outputs.outputs.created_issue_number", "should reference safe_outputs job created_issue_number")

	// Verify add-comment outputs
	assert.Contains(t, yamlOutput, "comment_id:", "should contain comment_id")
	assert.Contains(t, yamlOutput, "comment_url:", "should contain comment_url")

	// Verify safe_outputs job outputs contain the new individual outputs
	assert.Contains(t, yamlOutput, "created_issue_number: ${{ steps.process_safe_outputs.outputs.created_issue_number }}",
		"safe_outputs job should expose created_issue_number")
	assert.Contains(t, yamlOutput, "comment_id: ${{ steps.process_safe_outputs.outputs.comment_id }}",
		"safe_outputs job should expose comment_id")
}

func TestWorkflowCallDownstreamSetupComputesLocalPrefix(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{Name: "test", On: "workflow_call:"}
	for _, jobName := range []string{"agent", "detection", "conclusion", "safe_outputs"} {
		t.Run(jobName, func(t *testing.T) {
			steps := strings.Join(compiler.generateSetupStepForJob(jobName, data, "./actions/setup", SetupActionDestination, false, "", "", ""), "")
			setup := strings.Index(steps, "id: setup\n")
			prefix := strings.Index(steps, "id: artifact-prefix\n")
			resolver := strings.Index(steps, "id: resolve-host-repo\n")
			require.NotEqual(t, -1, setup)
			require.Greater(t, prefix, setup)
			require.Greater(t, resolver, prefix)
			assert.Contains(t, steps, "INPUTS_JSON: ${{ toJSON(inputs) }}")
			assert.Contains(t, steps, "compute_artifact_prefix.sh")
		})
	}
	for _, jobName := range []string{"activation", "pre_activation"} {
		steps := strings.Join(compiler.generateSetupStepForJob(jobName, data, "./actions/setup", SetupActionDestination, false, "", "", ""), "")
		assert.NotContains(t, steps, "id: artifact-prefix\n")
	}
	nonCall := &WorkflowData{Name: "test", On: "push:"}
	steps := strings.Join(compiler.generateSetupStepForJob("agent", nonCall, "./actions/setup", SetupActionDestination, false, "", "", ""), "")
	assert.NotContains(t, steps, "id: artifact-prefix\n")
	assert.NotContains(t, steps, "id: resolve-host-repo\n")
}

func TestHasWorkflowCallTrigger(t *testing.T) {
	tests := []struct {
		name      string
		onSection string
		expected  bool
	}{
		{
			name: "workflow_call present in map format",
			onSection: `"on":
  workflow_call:`,
			expected: true,
		},
		{
			name: "workflow_call present with inputs",
			onSection: `"on":
  workflow_call:
    inputs:
      issue_number:
        required: true
        type: number`,
			expected: true,
		},
		{
			name: "workflow_call absent - push and workflow_dispatch only",
			onSection: `"on":
  push:
  workflow_dispatch:`,
			expected: false,
		},
		{
			name: "workflow_call with other triggers - issue_comment and workflow_call",
			onSection: `"on":
  issue_comment:
    types: [created]
  workflow_call:`,
			expected: true,
		},
		{
			name:      "empty string",
			onSection: "",
			expected:  false,
		},
		{
			name: "workflow_dispatch only - not workflow_call",
			onSection: `"on":
  workflow_dispatch:`,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasWorkflowCallTrigger(tt.onSection)
			assert.Equal(t, tt.expected, result, "hasWorkflowCallTrigger() result mismatch for %q", tt.name)
		})
	}
}

// TestInjectWorkflowCallSecretsSection tests that secrets are correctly injected into
// the on.workflow_call.secrets section of the on: YAML string.
func TestInjectWorkflowCallSecretsSection(t *testing.T) {
	tests := []struct {
		name        string
		onSection   string
		secrets     []string
		wantContain []string
		wantAbsent  []string
		unchanged   bool
	}{
		{
			name: "injects secrets into workflow_call trigger",
			onSection: `"on":
  workflow_call:
    inputs:
      payload:
        type: string`,
			secrets:     []string{"MY_TOKEN", "ANOTHER_SECRET"},
			wantContain: []string{"MY_TOKEN", "ANOTHER_SECRET", "required: false"},
		},
		{
			name: "excludes GITHUB_TOKEN",
			onSection: `"on":
  workflow_call: {}`,
			secrets:     []string{"GITHUB_TOKEN", "MY_TOKEN"},
			wantContain: []string{"MY_TOKEN"},
			wantAbsent:  []string{"GITHUB_TOKEN"},
		},
		{
			name: "no-op when only GITHUB_TOKEN",
			onSection: `"on":
  workflow_call: {}`,
			secrets:   []string{"GITHUB_TOKEN"},
			unchanged: true,
		},
		{
			name: "no-op when no secrets",
			onSection: `"on":
  workflow_call: {}`,
			secrets:   []string{},
			unchanged: true,
		},
		{
			name: "no-op when no workflow_call trigger",
			onSection: `"on":
  workflow_dispatch: {}`,
			secrets:   []string{"MY_TOKEN"},
			unchanged: true,
		},
		{
			name: "user-defined secrets are preserved",
			onSection: `"on":
  workflow_call:
    secrets:
      USER_SECRET:
        required: true`,
			secrets:     []string{"AUTO_SECRET"},
			wantContain: []string{"USER_SECRET", "AUTO_SECRET"},
		},
		{
			name: "schedule sequence stays indented and cron stays quoted",
			onSection: `"on":
  schedule:
    - cron: "0 0 */2 * *"
  workflow_call: {}`,
			secrets:     []string{"MY_TOKEN"},
			wantContain: []string{"MY_TOKEN", "  schedule:\n    - cron: \"0 0 */2 * *\""},
			wantAbsent:  []string{"  schedule:\n  - cron:", "- cron: 0 0"},
		},
		{
			name:        "handles string shorthand on: workflow_call",
			onSection:   `"on": workflow_call`,
			secrets:     []string{"MY_TOKEN"},
			wantContain: []string{"MY_TOKEN", "required: false"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := injectWorkflowCallSecretsSection(tt.onSection, tt.secrets)
			if tt.unchanged {
				assert.Equal(t, tt.onSection, result, "on section should be unchanged")
				return
			}
			for _, want := range tt.wantContain {
				assert.Contains(t, result, want, "should contain %q", want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, result, absent, "should not contain %q", absent)
			}
		})
	}
}
