//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSafeOutputsAppConfiguration tests that app configuration is correctly parsed
func TestSafeOutputsAppConfiguration(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	markdown := `---
on: issues
safe-outputs:
  create-issue:
  github-app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
    repositories:
      - "repo1"
      - "repo2"
---

# Test Workflow

Test workflow with app configuration.
`

	// Create a temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")
	err := os.WriteFile(testFile, []byte(markdown), 0644)
	require.NoError(t, err, "Failed to write test file")

	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err, "Failed to parse markdown content")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")
	require.NotNil(t, workflowData.SafeOutputs.GitHubApp, "App configuration should be parsed")

	// Verify app configuration
	assert.Equal(t, "${{ vars.APP_ID }}", workflowData.SafeOutputs.GitHubApp.AppID)
	assert.Equal(t, "${{ secrets.APP_PRIVATE_KEY }}", workflowData.SafeOutputs.GitHubApp.PrivateKey)
	assert.Equal(t, []string{"repo1", "repo2"}, workflowData.SafeOutputs.GitHubApp.Repositories)
}

// TestSafeOutputsAppConfigurationMinimal tests minimal app configuration without repositories
func TestSafeOutputsAppConfigurationMinimal(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	markdown := `---
on: issues
safe-outputs:
  create-issue:
  github-app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
---

# Test Workflow

Test workflow with minimal app configuration.
`

	// Create a temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")
	err := os.WriteFile(testFile, []byte(markdown), 0644)
	require.NoError(t, err, "Failed to write test file")

	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err, "Failed to parse markdown content")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")
	require.NotNil(t, workflowData.SafeOutputs.GitHubApp, "App configuration should be parsed")

	// Verify app configuration
	assert.Equal(t, "${{ vars.APP_ID }}", workflowData.SafeOutputs.GitHubApp.AppID)
	assert.Equal(t, "${{ secrets.APP_PRIVATE_KEY }}", workflowData.SafeOutputs.GitHubApp.PrivateKey)
	assert.Empty(t, workflowData.SafeOutputs.GitHubApp.Repositories)
}

func TestSafeOutputsAppIgnoreIfMissing(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	markdown := `---
on: issues
safe-outputs:
  add-comment:
  github-app:
    app-id: ${{ secrets.GH_AW_APP_ID }}
    private-key: ${{ secrets.GH_AW_APP_PRIVATE_KEY }}
    ignore-if-missing: true
---

# Test Workflow
`

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")
	err := os.WriteFile(testFile, []byte(markdown), 0644)
	require.NoError(t, err, "Failed to write test file")

	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err, "Failed to parse markdown content")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")
	require.NotNil(t, workflowData.SafeOutputs.GitHubApp, "GitHub app configuration should be parsed")
	assert.True(t, workflowData.SafeOutputs.GitHubApp.IgnoreIfMissing)

	job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, "main", testFile)
	require.NoError(t, err, "Failed to build safe_outputs job")
	require.NotNil(t, job, "Job should not be nil")

	stepsStr := strings.Join(job.Steps, "")
	// Both credentials use secrets.* context, so the ignore-if-missing guard must
	// check step-local env aliases instead of emitting secrets.* directly in if:.
	assert.NotContains(t, stepsStr, "if: ${{ secrets.")
	assert.Contains(t, stepsStr, "GH_AW_IGNORE_IF_MISSING_APP_ID: ${{ secrets.GH_AW_APP_ID }}")
	assert.Contains(t, stepsStr, "GH_AW_IGNORE_IF_MISSING_PRIVATE_KEY: ${{ secrets.GH_AW_APP_PRIVATE_KEY }}")
	assert.Contains(t, stepsStr, "if: ${{ env.GH_AW_IGNORE_IF_MISSING_APP_ID != '' && env.GH_AW_IGNORE_IF_MISSING_PRIVATE_KEY != '' }}")
	assert.Contains(t, stepsStr, "github-token: ${{ steps.safe-outputs-app-token.outputs.token || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}")
}

func TestSafeOutputsAppIgnoreIfMissingInvalidType(t *testing.T) {
	app := parseAppConfig(map[string]any{
		"client-id":         "${{ vars.APP_ID }}",
		"private-key":       "${{ secrets.APP_PRIVATE_KEY }}",
		"ignore-if-missing": "not-a-bool",
	})

	require.NotNil(t, app)
	assert.False(t, app.IgnoreIfMissing)
	assert.False(t, app.shouldIgnoreMissingKey())
}

func TestParseAppConfigClonesTypedCollections(t *testing.T) {
	repositories := []string{"gh-aw"}
	permissions := map[string]string{"members": "read"}

	app := parseAppConfig(map[string]any{
		"repositories": repositories,
		"permissions":  permissions,
	})
	app.Repositories[0] = "other"
	app.Permissions["members"] = "write"

	assert.Equal(t, []string{"gh-aw"}, repositories)
	assert.Equal(t, map[string]string{"members": "read"}, permissions)
}

func TestBuildIgnoreIfMissingCondition(t *testing.T) {
	tests := []struct {
		name       string
		appID      string
		privateKey string
		expected   ignoreIfMissingGuard
	}{
		{
			name:       "both secrets - route through env aliases",
			appID:      "${{ secrets.GH_AW_APP_ID }}",
			privateKey: "${{ secrets.GH_AW_APP_PRIVATE_KEY }}",
			expected: ignoreIfMissingGuard{
				Condition: "${{ env.GH_AW_IGNORE_IF_MISSING_APP_ID != '' && env.GH_AW_IGNORE_IF_MISSING_PRIVATE_KEY != '' }}",
				EnvAssignments: []stepEnvAssignment{
					{Name: ignoreIfMissingAppIDEnvVar, Value: "${{ secrets.GH_AW_APP_ID }}"},
					{Name: ignoreIfMissingPrivateKeyEnvVar, Value: "${{ secrets.GH_AW_APP_PRIVATE_KEY }}"},
				},
			},
		},
		{
			name:       "vars client-id + secrets private-key",
			appID:      "${{ vars.APP_CLIENT_ID }}",
			privateKey: "${{ secrets.APP_PRIVATE_KEY }}",
			expected: ignoreIfMissingGuard{
				Condition: "${{ vars.APP_CLIENT_ID != '' && env.GH_AW_IGNORE_IF_MISSING_PRIVATE_KEY != '' }}",
				EnvAssignments: []stepEnvAssignment{
					{Name: ignoreIfMissingPrivateKeyEnvVar, Value: "${{ secrets.APP_PRIVATE_KEY }}"},
				},
			},
		},
		{
			name:       "both vars",
			appID:      "${{ vars.APP_CLIENT_ID }}",
			privateKey: "${{ vars.APP_PRIVATE_KEY }}",
			expected: ignoreIfMissingGuard{
				Condition: "${{ vars.APP_CLIENT_ID != '' && vars.APP_PRIVATE_KEY != '' }}",
			},
		},
		{
			name:       "literal values",
			appID:      "  id value  ",
			privateKey: "key'value",
			expected: ignoreIfMissingGuard{
				Condition: "${{ 'id value' != '' && 'key''value' != '' }}",
			},
		},
		{
			name:       "matrix remains valid in step if",
			appID:      "${{ matrix.app_id }}",
			privateKey: "${{ matrix.key }}",
			expected: ignoreIfMissingGuard{
				Condition: "${{ matrix.app_id != '' && matrix.key != '' }}",
			},
		},
		{
			name:       "jobs context with bracket syntax routes through env aliases",
			appID:      "${{ jobs.build.outputs['app-id'] }}",
			privateKey: "${{ jobs.build.outputs['private-key'] }}",
			expected: ignoreIfMissingGuard{
				Condition: "${{ env.GH_AW_IGNORE_IF_MISSING_APP_ID != '' && env.GH_AW_IGNORE_IF_MISSING_PRIVATE_KEY != '' }}",
				EnvAssignments: []stepEnvAssignment{
					{Name: ignoreIfMissingAppIDEnvVar, Value: "${{ jobs.build.outputs['app-id'] }}"},
					{Name: ignoreIfMissingPrivateKeyEnvVar, Value: "${{ jobs.build.outputs['private-key'] }}"},
				},
			},
		},
		{
			name:       "composite expression with secrets routes through env alias",
			appID:      "${{ vars.APP_ID || secrets.FALLBACK_ID }}",
			privateKey: "${{ vars.APP_KEY }}",
			expected: ignoreIfMissingGuard{
				Condition: "${{ env.GH_AW_IGNORE_IF_MISSING_APP_ID != '' && vars.APP_KEY != '' }}",
				EnvAssignments: []stepEnvAssignment{
					{Name: ignoreIfMissingAppIDEnvVar, Value: "${{ vars.APP_ID || secrets.FALLBACK_ID }}"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &GitHubAppConfig{
				AppID:      tt.appID,
				PrivateKey: tt.privateKey,
			}
			assert.Equal(t, tt.expected, buildIgnoreIfMissingCondition(app))
		})
	}
}

// TestSafeOutputsAppWithoutSafeOutputs tests that app without safe outputs doesn't break
func TestSafeOutputsAppWithoutSafeOutputs(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	markdown := `---
on: issues
permissions:
  contents: read
---

# Test Workflow

Test workflow without safe outputs.
`

	// Create a temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")
	err := os.WriteFile(testFile, []byte(markdown), 0644)
	require.NoError(t, err, "Failed to write test file")

	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err, "Failed to parse markdown content")
	// create-issue is auto-injected even when no safe-outputs section is configured
	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected SafeOutputs to be non-nil after auto-injection of create-issue")
	}
	if workflowData.SafeOutputs.CreateIssues == nil || !workflowData.SafeOutputs.AutoInjectedCreateIssue {
		t.Error("Expected create-issue to be auto-injected when no safe-outputs configured")
	}
}

// TestSafeOutputsAppTokenDiscussionsPermission tests that discussions permission is included
// in the GitHub App token minting step when create-discussion is configured.
//
// actions/create-github-app-token v3+ declares "permission-discussions" as a valid input.
// When any permission-* input is specified, the action scopes the token to ONLY those permissions,
// so omitting permission-discussions would exclude discussions access from the minted token.
func TestSafeOutputsAppTokenDiscussionsPermission(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	markdown := `---
on: issues
safe-outputs:
  create-discussion:
    category: "general"
  github-app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
---

# Test Workflow

Test workflow with discussions permission.
`

	// Create a temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")
	err := os.WriteFile(testFile, []byte(markdown), 0644)
	require.NoError(t, err, "Failed to write test file")

	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err, "Failed to parse markdown content")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")
	require.NotNil(t, workflowData.SafeOutputs.CreateDiscussions, "CreateDiscussions should not be nil")

	// Build the consolidated safe_outputs job
	job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, "main", testFile)
	require.NoError(t, err, "Failed to build safe_outputs job")
	require.NotNil(t, job, "Job should not be nil")

	// Convert steps to string for easier assertion
	stepsStr := strings.Join(job.Steps, "")

	// permission-discussions must be present because when any permission-* input is set,
	// actions/create-github-app-token scopes the token to only those permissions.
	assert.Contains(t, stepsStr, "permission-discussions: write", "GitHub App token should include discussions write permission")
	// Other explicitly supported permission inputs should still be present
	assert.Contains(t, stepsStr, "permission-issues: write", "GitHub App token should include issues write permission (create-discussion falls back to issue)")
	assert.NotContains(t, stepsStr, "permission-contents: read", "GitHub App token should not include contents read permission for output-only handlers")
}

// TestSafeOutputsAppTokenUpdateProjectIssuesWritePermission tests that issues write permission
// is included in the GitHub App token minting step when update-project is configured.
// The safe_outputs job also includes the built-in report_incomplete issue path, so the
// least-privilege token must be able to write issues, not just read them.
func TestSafeOutputsAppTokenUpdateProjectIssuesWritePermission(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	markdown := `---
on: issues
safe-outputs:
  update-project:
    project: "https://github.com/orgs/my-org/projects/1"
  github-app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
---

# Test Workflow

Test workflow with update-project permissions.
`

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")
	err := os.WriteFile(testFile, []byte(markdown), 0644)
	require.NoError(t, err, "Failed to write test file")

	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err, "Failed to parse markdown content")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")
	require.NotNil(t, workflowData.SafeOutputs.UpdateProjects, "UpdateProjects should not be nil")

	job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, "main", testFile)
	require.NoError(t, err, "Failed to build safe_outputs job")
	require.NotNil(t, job, "Job should not be nil")

	stepsStr := strings.Join(job.Steps, "")

	assert.Contains(t, stepsStr, "permission-organization-projects: write", "GitHub App token should include organization projects write permission")
	assert.Contains(t, stepsStr, "permission-issues: write", "GitHub App token should include issues write permission for issue-backed project items and built-in report_incomplete issue creation")
	assert.NotContains(t, stepsStr, "permission-contents: read", "GitHub App token should not include contents read permission for output-only handlers")
}

// TestSafeOutputsAppTokenCreateProjectWithItemURLIssuesWritePermission tests that issues write permission
// is included in the GitHub App token minting step when create-project is configured with item_url.
// The safe_outputs job also includes the built-in report_incomplete issue path, so the
// least-privilege token must be able to write issues, not just read them.
func TestSafeOutputsAppTokenCreateProjectWithItemURLIssuesWritePermission(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	markdown := `---
on: issues
safe-outputs:
  create-project:
    target-owner: "my-org"
  github-app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
---

# Test Workflow

Test workflow with create-project item_url permissions.
`

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")
	err := os.WriteFile(testFile, []byte(markdown), 0644)
	require.NoError(t, err, "Failed to write test file")

	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err, "Failed to parse markdown content")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")
	require.NotNil(t, workflowData.SafeOutputs.CreateProjects, "CreateProjects should not be nil")

	job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, "main", testFile)
	require.NoError(t, err, "Failed to build safe_outputs job")
	require.NotNil(t, job, "Job should not be nil")

	stepsStr := strings.Join(job.Steps, "")

	assert.Contains(t, stepsStr, "permission-organization-projects: write", "GitHub App token should include organization projects write permission")
	assert.Contains(t, stepsStr, "permission-issues: write", "GitHub App token should include issues write permission for issue-backed project items and built-in report_incomplete issue creation")
	assert.NotContains(t, stepsStr, "permission-contents: read", "GitHub App token should not include contents read permission for output-only handlers")
}

// TestSafeOutputsAppTokenAddCommentAddLabelsIssuesWrite is a regression test for the issue
// where safe_outputs App token permissions were capped at the workflow-level permissions block
// instead of being derived from the configured safe-output handlers.
//
// Repro: workflow declares `permissions: { issues: read }` (required by agent-no-write rule),
// but configures add-comment (issues: true) and add-labels — both needing issues: write.
// The compiled App token MUST emit `permission-issues: write`, not `read`.
func TestSafeOutputsAppTokenAddCommentAddLabelsIssuesWrite(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	markdown := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: read
safe-outputs:
  github-app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
    owner: my-org
  add-comment:
    max: 1
    issues: true
    pull-requests: false
    discussions: false
  add-labels:
    max: 4
    allowed: [routed]
---
Test workflow
`

	tmpDir := t.TempDir()
	testFile := tmpDir + "/test.md"
	require.NoError(t, os.WriteFile(testFile, []byte(markdown), 0644), "Failed to write test file")

	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err, "Failed to parse markdown content")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")
	require.NotNil(t, workflowData.SafeOutputs.GitHubApp, "GitHubApp should not be nil")

	job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, "agent", testFile)
	require.NoError(t, err, "Failed to build safe_outputs job")
	require.NotNil(t, job, "Job should not be nil")

	stepsStr := strings.Join(job.Steps, "")

	// The workflow declares `issues: read` but the handlers require `issues: write`.
	// The App token permissions MUST come from handler-computed scope, NOT the
	// workflow-level `permissions:` block.
	assert.Contains(t, stepsStr, "permission-issues: write",
		"App token must use handler-computed issues:write, not workflow-level issues:read")
	assert.Contains(t, stepsStr, "permission-pull-requests: write",
		"App token must include pull-requests:write from add-labels handler")
	assert.NotContains(t, stepsStr, "permission-contents: read",
		"App token must not include contents:read for output-only handlers")

	// The job-level permissions YAML must also reflect the handler-computed scope.
	assert.Contains(t, job.Permissions, "issues: write",
		"Job-level permissions must be handler-computed (issues:write)")
}

func TestSafeOutputsAppTokenAddLabelsPullRequestsOptOut(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	markdown := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: read
safe-outputs:
  github-app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
    owner: my-org
  add-labels:
    max: 4
    allowed: [routed]
    pull-requests: false
---
Test workflow
`

	tmpDir := t.TempDir()
	testFile := tmpDir + "/test.md"
	require.NoError(t, os.WriteFile(testFile, []byte(markdown), 0644), "Failed to write test file")

	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err, "Failed to parse markdown content")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")
	require.NotNil(t, workflowData.SafeOutputs.GitHubApp, "GitHubApp should not be nil")

	job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, "agent", testFile)
	require.NoError(t, err, "Failed to build safe_outputs job")
	require.NotNil(t, job, "Job should not be nil")

	stepsStr := strings.Join(job.Steps, "")

	assert.Contains(t, stepsStr, "permission-issues: write",
		"App token must keep issues:write for issue label operations")
	assert.NotContains(t, stepsStr, "permission-pull-requests: write",
		"App token must omit pull-requests:write when add-labels disables pull request targets")
	assert.Contains(t, job.Permissions, "issues: write",
		"Job-level permissions must preserve issues:write")
	assert.NotContains(t, job.Permissions, "pull-requests: write",
		"Job-level permissions must omit pull-requests:write when opted out")
}

func TestSafeOutputsAppTokenRemoveLabelsPullRequestsOptOut(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	markdown := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: read
safe-outputs:
  github-app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
    owner: my-org
  remove-labels:
    allowed: [routed]
    pull-requests: false
---
Test workflow
`

	tmpDir := t.TempDir()
	testFile := tmpDir + "/test.md"
	require.NoError(t, os.WriteFile(testFile, []byte(markdown), 0644), "Failed to write test file")

	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err, "Failed to parse markdown content")

	job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, "agent", testFile)
	require.NoError(t, err, "Failed to build safe_outputs job")
	require.NotNil(t, job, "Job should not be nil")

	stepsStr := strings.Join(job.Steps, "")
	assert.Contains(t, stepsStr, "permission-issues: write")
	assert.NotContains(t, stepsStr, "permission-pull-requests: write")
	assert.Contains(t, job.Permissions, "issues: write")
	assert.NotContains(t, job.Permissions, "pull-requests: write")
}

func TestSafeOutputsAppTokenRemoveLabelsIssuesOptOut(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	issuesFalse := false
	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			GitHubApp: &GitHubAppConfig{
				AppID:      "${{ vars.GLOBAL_APP_ID }}",
				PrivateKey: "${{ secrets.GLOBAL_APP_PRIVATE_KEY }}",
			},
			RemoveLabels: &RemoveLabelsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					GitHubApp: &GitHubAppConfig{
						AppID:      "${{ vars.REMOVE_LABELS_APP_ID }}",
						PrivateKey: "${{ secrets.REMOVE_LABELS_APP_PRIVATE_KEY }}",
					},
				},
				Issues: &issuesFalse,
			},
		},
	}

	job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, "agent", "test.md")
	require.NoError(t, err, "Failed to build safe_outputs job")
	require.NotNil(t, job, "Job should not be nil")

	stepsStr := strings.Join(job.Steps, "")
	assert.Contains(t, stepsStr, "id: remove-labels-app-token")
	assert.NotContains(t, stepsStr, "permission-issues: write")
	assert.Contains(t, stepsStr, "permission-pull-requests: write")
	assert.Contains(t, stepsStr, "steps.remove-labels-app-token.outputs.token")
}

// TestSafeOutputsAppTokenUpdateProjectDoesNotDowngradeIssuesWrite is a regression test for the
// add-comment + add-labels + update-project co-presence case reported after github/gh-aw#30437.
// update-project must not downgrade issues permission from write to read in the minted GitHub App token.
func TestSafeOutputsAppTokenUpdateProjectDoesNotDowngradeIssuesWrite(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	markdown := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: read
safe-outputs:
  github-app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
    owner: my-org
  add-comment:
    max: 1
    issues: true
    pull-requests: false
    discussions: false
  add-labels:
    max: 4
    allowed: [routed]
  update-project:
    max: 1
    project: https://github.com/orgs/my-org/projects/1
---
Test workflow
`

	tmpDir := t.TempDir()
	testFile := tmpDir + "/test.md"
	require.NoError(t, os.WriteFile(testFile, []byte(markdown), 0644), "Failed to write test file")

	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err, "Failed to parse markdown content")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")
	require.NotNil(t, workflowData.SafeOutputs.GitHubApp, "GitHubApp should not be nil")

	job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, "agent", testFile)
	require.NoError(t, err, "Failed to build safe_outputs job")
	require.NotNil(t, job, "Job should not be nil")

	stepsStr := strings.Join(job.Steps, "")

	assert.Contains(t, stepsStr, "permission-issues: write",
		"App token must preserve issues:write required by add-comment/add-labels when update-project is present")
	assert.Contains(t, stepsStr, "permission-organization-projects: write",
		"App token must include organization-projects:write for update-project")
	assert.Contains(t, job.Permissions, "issues: write",
		"Job-level permissions must preserve handler-computed issues:write")
}

// TestSafeOutputsAppTokenPermissionsOverride tests that safe-outputs.github-app.permissions:
// overrides take effect in the minted token. Users can supply GitHub App-only scopes
// (e.g. members: read) not expressible via standard safe-output handler declarations.
func TestSafeOutputsAppTokenPermissionsOverride(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			GitHubApp: &GitHubAppConfig{
				AppID:      "${{ vars.APP_ID }}",
				PrivateKey: "${{ secrets.APP_PRIVATE_KEY }}",
				Permissions: map[string]string{
					"members": "read",
				},
			},
			CreateIssues: &CreateIssuesConfig{TitlePrefix: "[Test] "},
		},
	}

	job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, "agent", "test.md")
	require.NoError(t, err, "Failed to build safe_outputs job")
	require.NotNil(t, job, "Job should not be nil")

	stepsStr := strings.Join(job.Steps, "")

	// The override must add permission-members: read to the minted token.
	assert.Contains(t, stepsStr, "permission-members: read",
		"App token must include members:read from github-app.permissions override")
}

// TestSafeOutputsCreateCheckRunAppTokenMinimalPermissions tests that the per-handler
// GitHub App token for create-check-run uses minimal permissions (no contents: read).
// When target is not configured, only checks: write is required.
// When target is configured, pull-requests: read is added for PR head SHA resolution.
func TestSafeOutputsCreateCheckRunAppTokenMinimalPermissions(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	t.Run("no target - only checks: write", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "Test Workflow",
			SafeOutputs: &SafeOutputsConfig{
				CreateCheckRun: &CreateCheckRunConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						GitHubApp: &GitHubAppConfig{
							AppID:      "${{ vars.APP_ID }}",
							PrivateKey: "${{ secrets.APP_PRIVATE_KEY }}",
						},
					},
				},
			},
		}

		job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, "agent", "test.md")
		require.NoError(t, err, "Failed to build safe_outputs job")
		require.NotNil(t, job, "Job should not be nil")

		stepsStr := strings.Join(job.Steps, "")

		assert.Contains(t, stepsStr, "id: create-check-run-app-token",
			"Per-handler app token step must be present")
		assert.Contains(t, stepsStr, "permission-checks: write",
			"App token must include checks:write")
		assert.NotContains(t, stepsStr, "permission-pull-requests:",
			"App token must not include pull-requests permission when no target is configured")
		assert.NotContains(t, stepsStr, "permission-contents: read",
			"App token must not include contents:read for create-check-run")
	})

	t.Run("with target - checks: write and pull-requests: read", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "Test Workflow",
			SafeOutputs: &SafeOutputsConfig{
				CreateCheckRun: &CreateCheckRunConfig{
					Target: "triggering",
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						GitHubApp: &GitHubAppConfig{
							AppID:      "${{ vars.APP_ID }}",
							PrivateKey: "${{ secrets.APP_PRIVATE_KEY }}",
						},
					},
				},
			},
		}

		job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, "agent", "test.md")
		require.NoError(t, err, "Failed to build safe_outputs job")
		require.NotNil(t, job, "Job should not be nil")

		stepsStr := strings.Join(job.Steps, "")

		assert.Contains(t, stepsStr, "id: create-check-run-app-token",
			"Per-handler app token step must be present")
		assert.Contains(t, stepsStr, "permission-checks: write",
			"App token must include checks:write")
		assert.Contains(t, stepsStr, "permission-pull-requests: read",
			"App token must include pull-requests:read when target is configured")
		assert.NotContains(t, stepsStr, "permission-contents: read",
			"App token must not include contents:read for create-check-run")
	})
}

// TestSafeOutputsPerHandlerGitHubAppAddComment tests that when add-comment has its own
// github-app override, a dedicated token step is minted using only issues:write, and the
// handler config references that per-handler token (not the global token).
func TestSafeOutputsPerHandlerGitHubAppAddComment(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			GitHubApp: &GitHubAppConfig{
				AppID:      "${{ vars.GLOBAL_APP_ID }}",
				PrivateKey: "${{ secrets.GLOBAL_APP_PRIVATE_KEY }}",
			},
			AddComments: &AddCommentConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					GitHubApp: &GitHubAppConfig{
						AppID:      "${{ vars.ISSUE_APP_ID }}",
						PrivateKey: "${{ secrets.ISSUE_APP_PRIVATE_KEY }}",
					},
				},
			},
		},
	}

	job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, "agent", "test.md")
	require.NoError(t, err, "Failed to build safe_outputs job")
	require.NotNil(t, job, "Job should not be nil")

	stepsStr := strings.Join(job.Steps, "")

	// Per-handler token step must be present with the correct step ID
	assert.Contains(t, stepsStr, "id: add-comment-app-token",
		"Per-handler app token step for add-comment must be present")

	// The per-handler step must use the handler-scoped app credentials
	assert.Contains(t, stepsStr, "${{ vars.ISSUE_APP_ID }}",
		"Per-handler token step must use the handler-level app-id")

	// The per-handler token must use only issues:write (not broader permissions)
	assert.Contains(t, stepsStr, "permission-issues: write",
		"Per-handler token must include issues:write")

	// Handler config must reference the per-handler token step
	assert.Contains(t, stepsStr, "steps.add-comment-app-token.outputs.token",
		"Handler config must reference the per-handler token step output")
}

// TestSafeOutputsPerHandlerGitHubAppDispatchWorkflow tests that when dispatch-workflow has
// its own github-app override, a dedicated token step is minted with actions:write
// and the handler config references that token.
func TestSafeOutputsPerHandlerGitHubAppDispatchWorkflow(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			GitHubApp: &GitHubAppConfig{
				AppID:      "${{ vars.GLOBAL_APP_ID }}",
				PrivateKey: "${{ secrets.GLOBAL_APP_PRIVATE_KEY }}",
			},
			DispatchWorkflow: &DispatchWorkflowConfig{
				Workflows: []string{"my-downstream.yml"},
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					GitHubApp: &GitHubAppConfig{
						AppID:      "${{ vars.ACTIONS_APP_ID }}",
						PrivateKey: "${{ secrets.ACTIONS_APP_PRIVATE_KEY }}",
					},
				},
			},
		},
	}

	job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, "agent", "test.md")
	require.NoError(t, err, "Failed to build safe_outputs job")
	require.NotNil(t, job, "Job should not be nil")

	stepsStr := strings.Join(job.Steps, "")

	// Per-handler token step must be present with the correct step ID
	assert.Contains(t, stepsStr, "id: dispatch-workflow-app-token",
		"Per-handler app token step for dispatch-workflow must be present")

	// The per-handler step must use the handler-scoped app credentials
	assert.Contains(t, stepsStr, "${{ vars.ACTIONS_APP_ID }}",
		"Per-handler token step must use the handler-level app-id")

	// Handler config must reference the per-handler token step
	assert.Contains(t, stepsStr, "steps.dispatch-workflow-app-token.outputs.token",
		"Handler config must reference the per-handler token step output")
}

// TestSafeOutputsPerHandlerGitHubAppMultipleHandlers tests the scenario from the issue:
// two separate apps, one for issues (add-comment) and one for actions (dispatch-workflow).
// Each handler must mint its own token with only its required permissions.
// The global token step must not appear when all handlers have per-handler overrides.
func TestSafeOutputsPerHandlerGitHubAppMultipleHandlers(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			AddComments: &AddCommentConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					GitHubApp: &GitHubAppConfig{
						AppID:      "${{ vars.ISSUE_APP_ID }}",
						PrivateKey: "${{ secrets.ISSUE_APP_PRIVATE_KEY }}",
					},
				},
			},
			DispatchWorkflow: &DispatchWorkflowConfig{
				Workflows: []string{"my-downstream.yml"},
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					GitHubApp: &GitHubAppConfig{
						AppID:      "${{ vars.ACTIONS_APP_ID }}",
						PrivateKey: "${{ secrets.ACTIONS_APP_PRIVATE_KEY }}",
					},
				},
			},
		},
	}

	job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, "agent", "test.md")
	require.NoError(t, err, "Failed to build safe_outputs job")
	require.NotNil(t, job, "Job should not be nil")

	stepsStr := strings.Join(job.Steps, "")

	// Both per-handler token steps must be present
	assert.Contains(t, stepsStr, "id: add-comment-app-token",
		"Per-handler app token step for add-comment must be present")
	assert.Contains(t, stepsStr, "id: dispatch-workflow-app-token",
		"Per-handler app token step for dispatch-workflow must be present")

	// Each step references its own app credentials
	assert.Contains(t, stepsStr, "${{ vars.ISSUE_APP_ID }}",
		"add-comment token step must reference issues app-id")
	assert.Contains(t, stepsStr, "${{ vars.ACTIONS_APP_ID }}",
		"dispatch-workflow token step must reference actions app-id")

	// The global safe-outputs token step must not appear (no global github-app configured)
	assert.NotContains(t, stepsStr, "id: safe-outputs-app-token",
		"Global token step must not appear when no global github-app is configured")

	// Each handler must reference its own per-handler token
	assert.Contains(t, stepsStr, "steps.add-comment-app-token.outputs.token",
		"add-comment handler config must reference its per-handler token")
	assert.Contains(t, stepsStr, "steps.dispatch-workflow-app-token.outputs.token",
		"dispatch-workflow handler config must reference its per-handler token")
}

func TestSafeOutputsPerHandlerGitHubAppReportIncomplete(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			ReportIncomplete: &ReportIncompleteConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					GitHubApp: &GitHubAppConfig{
						AppID:      "${{ vars.INCOMPLETE_APP_ID }}",
						PrivateKey: "${{ secrets.INCOMPLETE_APP_PRIVATE_KEY }}",
					},
				},
			},
		},
	}

	steps, err := compiler.buildHandlerManagerStep(workflowData)
	require.NoError(t, err)
	stepsStr := strings.Join(steps, "")
	assert.Contains(t, stepsStr, "id: report-incomplete-app-token")
	assert.Contains(t, stepsStr, "permission-issues: write")
	assert.Contains(t, stepsStr, "steps.report-incomplete-app-token.outputs.token")
}

func TestSafeOutputsPerHandlerGitHubAppCloseHandlers(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			CloseIssues: &CloseIssuesConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					GitHubApp: &GitHubAppConfig{
						AppID:      "${{ vars.CLOSE_ISSUE_APP_ID }}",
						PrivateKey: "${{ secrets.CLOSE_ISSUE_APP_PRIVATE_KEY }}",
					},
				},
			},
			CloseDiscussions: &CloseDiscussionsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					GitHubApp: &GitHubAppConfig{
						AppID:      "${{ vars.CLOSE_DISCUSSION_APP_ID }}",
						PrivateKey: "${{ secrets.CLOSE_DISCUSSION_APP_PRIVATE_KEY }}",
					},
				},
			},
		},
	}

	job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, "agent", "test.md")
	require.NoError(t, err)

	stepsStr := strings.Join(job.Steps, "")
	assert.Contains(t, stepsStr, "id: close-issue-app-token")
	assert.Contains(t, stepsStr, "steps.close-issue-app-token.outputs.token")
	assert.Contains(t, stepsStr, "id: close-discussion-app-token")
	assert.Contains(t, stepsStr, "steps.close-discussion-app-token.outputs.token")
}

func TestSafeOutputsPerHandlerGitHubAppDispatchRepository(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			DispatchRepository: &DispatchRepositoryConfig{
				Tools: map[string]*DispatchRepositoryToolConfig{
					"trigger-ci": {
						Workflow:    "ci.yml",
						EventType:   "ci_trigger",
						Repository:  "github/gh-aw",
						Max:         strPtr("1"),
						GitHubApp:   &GitHubAppConfig{AppID: "${{ vars.DISPATCH_APP_ID }}", PrivateKey: "${{ secrets.DISPATCH_APP_PRIVATE_KEY }}"},
						GitHubToken: "${{ secrets.FALLBACK_TOKEN }}",
					},
				},
			},
		},
	}

	steps, err := compiler.buildHandlerManagerStep(workflowData)
	require.NoError(t, err)
	stepsStr := strings.Join(steps, "")
	assert.Contains(t, stepsStr, "id: dispatch-repository-trigger_ci-app-token")
	assert.Contains(t, stepsStr, "permission-contents: write")
	assert.Contains(t, stepsStr, "steps.dispatch-repository-trigger_ci-app-token.outputs.token")
}

func TestGetHandlerGitHubAppRegisteredHandlers(t *testing.T) {
	config := &SafeOutputsConfig{}
	configValue := reflect.ValueOf(config).Elem()
	appFieldType := reflect.TypeFor[*GitHubAppConfig]()

	for _, handler := range safeOutputHandlers {
		if handler.StructField == "" {
			continue
		}

		field := configValue.FieldByName(handler.StructField)
		if !field.IsValid() || field.Kind() != reflect.Pointer || !field.CanSet() {
			continue
		}

		handlerValue := reflect.New(field.Type().Elem())
		inner := handlerValue.Elem()
		expected := &GitHubAppConfig{AppID: "app-id", PrivateKey: "private-key"}

		switch {
		case inner.FieldByName("GitHubApp").IsValid() && inner.FieldByName("GitHubApp").Type() == appFieldType:
			inner.FieldByName("GitHubApp").Set(reflect.ValueOf(expected))
		case inner.FieldByName("BaseSafeOutputConfig").IsValid():
			baseField := inner.FieldByName("BaseSafeOutputConfig")
			appField := baseField.FieldByName("GitHubApp")
			if !appField.IsValid() || appField.Type() != appFieldType {
				continue
			}
			appField.Set(reflect.ValueOf(expected))
		default:
			continue
		}

		field.Set(handlerValue)
		assert.Same(t, expected, getHandlerGitHubApp(config, handler.StructField), handler.StructField)
		field.Set(reflect.Zero(field.Type()))
	}
}

// TestSafeOutputsPerHandlerGitHubAppAddLabels tests that add-labels with its own nested
// github-app mints a dedicated token step with issues:write and pull-requests:write (defaults),
// and the handler config references that per-handler token (not the global token).
func TestSafeOutputsPerHandlerGitHubAppAddLabels(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			GitHubApp: &GitHubAppConfig{
				AppID:      "${{ vars.GLOBAL_APP_ID }}",
				PrivateKey: "${{ secrets.GLOBAL_APP_PRIVATE_KEY }}",
			},
			AddLabels: &AddLabelsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					GitHubApp: &GitHubAppConfig{
						AppID:      "${{ vars.LABELS_APP_ID }}",
						PrivateKey: "${{ secrets.LABELS_APP_PRIVATE_KEY }}",
					},
				},
			},
		},
	}

	job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, "agent", "test.md")
	require.NoError(t, err, "Failed to build safe_outputs job")
	require.NotNil(t, job, "Job should not be nil")

	stepsStr := strings.Join(job.Steps, "")

	// Per-handler token step must be present with the correct step ID
	assert.Contains(t, stepsStr, "id: add-labels-app-token",
		"Per-handler app token step for add-labels must be present")

	// The per-handler step must use the handler-scoped app credentials
	assert.Contains(t, stepsStr, "${{ vars.LABELS_APP_ID }}",
		"Per-handler token step must use the handler-level app-id")

	// The per-handler token must include both default permissions
	assert.Contains(t, stepsStr, "permission-issues: write",
		"Per-handler token must include issues:write (default)")
	assert.Contains(t, stepsStr, "permission-pull-requests: write",
		"Per-handler token must include pull-requests:write (default)")

	// Handler config must reference the per-handler token step
	assert.Contains(t, stepsStr, "steps.add-labels-app-token.outputs.token",
		"Handler config must reference the per-handler token step output")
}

// TestSafeOutputsPerHandlerGitHubAppAddLabelsIssuesOnly tests that add-labels with its own
// nested github-app and pull-requests: false mints a token with only issues:write.
// The per-handler app credentials are used, not the global app.
func TestSafeOutputsPerHandlerGitHubAppAddLabelsIssuesOnly(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	prFalse := false
	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			GitHubApp: &GitHubAppConfig{
				AppID:      "${{ vars.GLOBAL_APP_ID }}",
				PrivateKey: "${{ secrets.GLOBAL_APP_PRIVATE_KEY }}",
			},
			AddLabels: &AddLabelsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					GitHubApp: &GitHubAppConfig{
						AppID:      "${{ vars.ISSUES_ONLY_APP_ID }}",
						PrivateKey: "${{ secrets.ISSUES_ONLY_APP_PRIVATE_KEY }}",
					},
				},
				PullRequests: &prFalse,
			},
		},
	}

	job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, "agent", "test.md")
	require.NoError(t, err, "Failed to build safe_outputs job")
	require.NotNil(t, job, "Job should not be nil")

	stepsStr := strings.Join(job.Steps, "")

	// Per-handler token step must be present
	assert.Contains(t, stepsStr, "id: add-labels-app-token",
		"Per-handler app token step for add-labels must be present")

	// The per-handler step must use the handler-scoped app credentials
	assert.Contains(t, stepsStr, "${{ vars.ISSUES_ONLY_APP_ID }}",
		"Per-handler token step must use the handler-level app-id")

	// Token must carry only issues:write — pull-requests:write is opted out
	assert.Contains(t, stepsStr, "permission-issues: write",
		"Per-handler token must include issues:write")
	assert.NotContains(t, stepsStr, "permission-pull-requests: write",
		"Per-handler token must omit pull-requests:write when add-labels has pull-requests: false")

	// Handler config must reference the per-handler token step
	assert.Contains(t, stepsStr, "steps.add-labels-app-token.outputs.token",
		"Handler config must reference the per-handler token step output")
}

// TestSafeOutputsAppTokenAddLabelsNestedGitHubApp is an end-to-end compiler test that
// parses a markdown workflow with github-app nested directly inside add-labels (no global app).
// Verifies that the per-handler token step is emitted and the handler references it.
func TestSafeOutputsAppTokenAddLabelsNestedGitHubApp(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	markdown := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: read
safe-outputs:
  add-labels:
    max: 3
    allowed: [bug, enhancement]
    pull-requests: false
    github-app:
      app-id: ${{ vars.LABELS_APP_ID }}
      private-key: ${{ secrets.LABELS_APP_PRIVATE_KEY }}
      owner: my-org
---
Test workflow
`

	tmpDir := t.TempDir()
	testFile := tmpDir + "/test.md"
	require.NoError(t, os.WriteFile(testFile, []byte(markdown), 0644), "Failed to write test file")

	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err, "Failed to parse markdown content")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")
	require.NotNil(t, workflowData.SafeOutputs.AddLabels, "AddLabels should not be nil")
	require.NotNil(t, workflowData.SafeOutputs.AddLabels.GitHubApp,
		"Add-labels per-handler GitHubApp must be parsed from nested github-app key")
	assert.Nil(t, workflowData.SafeOutputs.GitHubApp,
		"No global github-app should be set — only the per-handler one is configured")

	job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, "agent", testFile)
	require.NoError(t, err, "Failed to build safe_outputs job")
	require.NotNil(t, job, "Job should not be nil")

	stepsStr := strings.Join(job.Steps, "")

	// Per-handler token step must be emitted
	assert.Contains(t, stepsStr, "id: add-labels-app-token",
		"Per-handler app token step for add-labels must be present")

	// Must use the nested app credentials
	assert.Contains(t, stepsStr, "${{ vars.LABELS_APP_ID }}",
		"Per-handler token step must use the nested app-id")

	// pull-requests: false → only issues:write in the minted token
	assert.Contains(t, stepsStr, "permission-issues: write",
		"Per-handler token must include issues:write")
	assert.NotContains(t, stepsStr, "permission-pull-requests: write",
		"Per-handler token must omit pull-requests:write when opted out")

	// No global safe-outputs-app-token step (there is no global github-app)
	assert.NotContains(t, stepsStr, "id: safe-outputs-app-token",
		"Global token step must not appear when only a per-handler app is configured")

	// Handler config must reference the per-handler token step
	assert.Contains(t, stepsStr, "steps.add-labels-app-token.outputs.token",
		"Handler config must reference the per-handler token step output")
}

func TestResolveHandlerGitHubTokenFallbackForHandlersWithoutPerHandlerMinting(t *testing.T) {
	app := &GitHubAppConfig{AppID: "app-id", PrivateKey: "private-key"}
	const fallback = "${{ secrets.FALLBACK_TOKEN }}"

	for _, handlerKey := range []string{"missing-tool", "missing-data", "upload-asset", "upload-artifact"} {
		t.Run(handlerKey, func(t *testing.T) {
			assert.Equal(t, fallback, resolveHandlerGitHubToken(app, handlerKey, fallback))
			assert.Empty(t, resolveHandlerGitHubToken(app, handlerKey, ""))
		})
	}
}

func TestResolveHandlerGitHubTokenUsesDedicatedMintStepForSupportedHandlers(t *testing.T) {
	app := &GitHubAppConfig{AppID: "app-id", PrivateKey: "private-key"}

	assert.Equal(t,
		"${{ steps.report-incomplete-app-token.outputs.token }}",
		resolveHandlerGitHubToken(app, "report-incomplete", "${{ secrets.FALLBACK_TOKEN }}"),
	)
	assert.Equal(t,
		"${{ steps.close-issue-app-token.outputs.token }}",
		resolveHandlerGitHubToken(app, "close-issue", "${{ secrets.FALLBACK_TOKEN }}"),
	)
}

// TestStagedSafeOutputsWithGitHubAppDoesNotEmitDanglingAppTokenReference verifies that a
// staged safe-outputs configuration combined with a global github-app never emits a
// github-token expression that only references the "safe-outputs-app-token" step: that
// minting step is skipped when no enabled handler consumes the global app, so the
// expression must carry a fallback token.
func TestStagedSafeOutputsWithGitHubAppDoesNotEmitDanglingAppTokenReference(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "staged-app.md")
	content := `---
on: workflow_dispatch
permissions:
  contents: read
engine: claude
safe-outputs:
  staged: true
  github-app:
    client-id: ${{ vars.APP_CLIENT_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
  create-pull-request:
---

Test workflow.
`
	require.NoError(t, os.WriteFile(testFile, []byte(content), 0600))

	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err)
	require.NotNil(t, workflowData.SafeOutputs)
	require.NotNil(t, workflowData.SafeOutputs.GitHubApp)

	assert.False(t, safeOutputsGlobalAppTokenMinted(workflowData.SafeOutputs),
		"Staged handlers must not trigger global GitHub App token minting")
	assert.Empty(t, compiler.buildPreambleTokenSteps(workflowData, map[string]string{}),
		"No preamble token steps should be built when all handlers are staged")

	lockContent, _, _, err := compiler.generateYAML(workflowData, testFile)
	require.NoError(t, err)

	for line := range strings.SplitSeq(lockContent, "\n") {
		if strings.Contains(line, "steps.safe-outputs-app-token.outputs.token") {
			assert.Contains(t, line, "||",
				"App token references must include a fallback when the minting step is skipped: %s", line)
		}
	}
}

// TestNonStagedSafeOutputsWithGitHubAppUsesAppTokenDirectly is the control case: when a
// GitHub-backed handler is enabled, the minting step is emitted and the token expression
// must reference it without a fallback.
func TestNonStagedSafeOutputsWithGitHubAppUsesAppTokenDirectly(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "app.md")
	content := `---
on: workflow_dispatch
permissions:
  contents: read
engine: claude
safe-outputs:
  github-app:
    client-id: ${{ vars.APP_CLIENT_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
  create-pull-request:
---

Test workflow.
`
	require.NoError(t, os.WriteFile(testFile, []byte(content), 0600))

	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err)

	assert.True(t, safeOutputsGlobalAppTokenMinted(workflowData.SafeOutputs))

	lockContent, _, _, err := compiler.generateYAML(workflowData, testFile)
	require.NoError(t, err)
	assert.Contains(t, lockContent, "github-token: ${{ steps.safe-outputs-app-token.outputs.token }}")
}
