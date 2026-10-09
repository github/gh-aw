//go:build !integration

package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDispatchWorkflowConfigTargetRepo verifies that dispatch-workflow correctly parses
// target-repo and allowed-repos fields.
func TestDispatchWorkflowConfigTargetRepo(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name          string
		configMap     map[string]any
		expectedRepo  string
		expectedRepos []string
		expectedRefs  []string
		expectedToken string
	}{
		{
			name: "target-repo and allowed-repos configured",
			configMap: map[string]any{
				"dispatch-workflow": map[string]any{
					"workflows":     []any{"worker"},
					"target-repo":   "githubnext/gh-aw-side-repo",
					"allowed-repos": []any{"githubnext/gh-aw-side-repo"},
					"allowed-refs":  []any{"refs/heads/release/*"},
					"github-token":  "${{ secrets.TEMP_USER_PAT }}",
				},
			},
			expectedRepo:  "githubnext/gh-aw-side-repo",
			expectedRepos: []string{"githubnext/gh-aw-side-repo"},
			expectedRefs:  []string{"refs/heads/release/*"},
			expectedToken: "${{ secrets.TEMP_USER_PAT }}",
		},
		{
			name: "multiple allowed repos",
			configMap: map[string]any{
				"dispatch-workflow": map[string]any{
					"workflows":     []any{"worker"},
					"target-repo":   "org/primary-repo",
					"allowed-repos": []any{"org/primary-repo", "org/secondary-repo"},
				},
			},
			expectedRepo:  "org/primary-repo",
			expectedRepos: []string{"org/primary-repo", "org/secondary-repo"},
			expectedRefs:  []string{defaultDispatchWorkflowAllowedRef},
			expectedToken: "",
		},
		{
			name: "allowed-repos as GitHub Actions expression",
			configMap: map[string]any{
				"dispatch-workflow": map[string]any{
					"workflows":     []any{"worker"},
					"target-repo":   "${{ inputs.target_repo }}",
					"allowed-repos": "${{ inputs['allowed-repos'] }}",
					"allowed-refs":  "${{ inputs['allowed-refs'] }}",
				},
			},
			expectedRepo:  "${{ inputs.target_repo }}",
			expectedRepos: []string{"${{ inputs['allowed-repos'] }}"},
			expectedRefs:  []string{"${{ inputs['allowed-refs'] }}"},
			expectedToken: "",
		},
		{
			name: "no cross-repo config",
			configMap: map[string]any{
				"dispatch-workflow": map[string]any{
					"workflows": []any{"worker"},
					"max":       2,
				},
			},
			expectedRepo:  "",
			expectedRepos: nil,
			expectedRefs:  []string{defaultDispatchWorkflowAllowedRef},
			expectedToken: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := compiler.parseDispatchWorkflowConfig(tt.configMap)

			require.NotNil(t, cfg, "config should not be nil")
			assert.Equal(t, tt.expectedRepo, cfg.TargetRepoSlug, "TargetRepoSlug mismatch")
			assert.Equal(t, tt.expectedRepos, cfg.AllowedRepos, "AllowedRepos mismatch")
			assert.Equal(t, tt.expectedRefs, cfg.AllowedRefs, "AllowedRefs mismatch")
			assert.Equal(t, tt.expectedToken, cfg.GitHubToken, "GitHubToken mismatch")
		})
	}
}

func TestDispatchWorkflowConfigShorthandUsesDefaultBranchRef(t *testing.T) {
	compiler := NewCompiler()

	cfg := compiler.parseDispatchWorkflowConfig(map[string]any{
		"dispatch-workflow": []any{"worker"},
	})

	require.NotNil(t, cfg)
	assert.Equal(t, []string{defaultDispatchWorkflowAllowedRef}, cfg.AllowedRefs)
}

// TestCreateCodeScanningAlertConfigTargetRepo verifies that create-code-scanning-alert
// correctly parses target-repo and allowed-repos fields.
func TestCreateCodeScanningAlertConfigTargetRepo(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name           string
		configMap      map[string]any
		expectedRepo   string
		expectedRepos  []string
		expectedToken  string
		expectedDriver string
	}{
		{
			name: "target-repo and allowed-repos configured",
			configMap: map[string]any{
				"create-code-scanning-alert": map[string]any{
					"max":           10,
					"target-repo":   "githubnext/gh-aw-side-repo",
					"allowed-repos": []any{"githubnext/gh-aw-side-repo"},
					"github-token":  "${{ secrets.TEMP_USER_PAT }}",
				},
			},
			expectedRepo:   "githubnext/gh-aw-side-repo",
			expectedRepos:  []string{"githubnext/gh-aw-side-repo"},
			expectedToken:  "${{ secrets.TEMP_USER_PAT }}",
			expectedDriver: "",
		},
		{
			name: "driver and target-repo configured",
			configMap: map[string]any{
				"create-code-scanning-alert": map[string]any{
					"driver":      "My Scanner",
					"target-repo": "owner/other-repo",
				},
			},
			expectedRepo:   "owner/other-repo",
			expectedRepos:  nil,
			expectedToken:  "",
			expectedDriver: "My Scanner",
		},
		{
			name: "no cross-repo config",
			configMap: map[string]any{
				"create-code-scanning-alert": map[string]any{
					"max": 5,
				},
			},
			expectedRepo:  "",
			expectedRepos: nil,
			expectedToken: "",
		},
		{
			name: "nil config value",
			configMap: map[string]any{
				"create-code-scanning-alert": nil,
			},
			expectedRepo:  "",
			expectedRepos: nil,
			expectedToken: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := compiler.parseCodeScanningAlertsConfig(tt.configMap)

			require.NotNil(t, cfg, "config should not be nil")
			assert.Equal(t, tt.expectedRepo, cfg.TargetRepoSlug, "TargetRepoSlug mismatch")
			assert.Equal(t, tt.expectedRepos, cfg.AllowedRepos, "AllowedRepos mismatch")
			assert.Equal(t, tt.expectedToken, cfg.GitHubToken, "GitHubToken mismatch")
			if tt.expectedDriver != "" {
				assert.Equal(t, tt.expectedDriver, cfg.Driver, "Driver mismatch")
			}
		})
	}
}

// TestPushToPullRequestBranchConfigTargetRepo verifies that push-to-pull-request-branch
// correctly parses target-repo and allowed-repos fields.
func TestPushToPullRequestBranchConfigTargetRepo(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name          string
		configMap     map[string]any
		expectedRepo  string
		expectedRepos []string
		expectedToken string
	}{
		{
			name: "target-repo and allowed-repos configured",
			configMap: map[string]any{
				"push-to-pull-request-branch": map[string]any{
					"target-repo":   "githubnext/gh-aw-side-repo",
					"allowed-repos": []any{"githubnext/gh-aw-side-repo"},
					"github-token":  "${{ secrets.TEMP_USER_PAT }}",
				},
			},
			expectedRepo:  "githubnext/gh-aw-side-repo",
			expectedRepos: []string{"githubnext/gh-aw-side-repo"},
			expectedToken: "${{ secrets.TEMP_USER_PAT }}",
		},
		{
			name: "multiple allowed repos",
			configMap: map[string]any{
				"push-to-pull-request-branch": map[string]any{
					"target-repo":   "org/primary-repo",
					"allowed-repos": []any{"org/primary-repo", "org/secondary-repo"},
				},
			},
			expectedRepo:  "org/primary-repo",
			expectedRepos: []string{"org/primary-repo", "org/secondary-repo"},
			expectedToken: "",
		},
		{
			name: "no cross-repo config",
			configMap: map[string]any{
				"push-to-pull-request-branch": map[string]any{
					"target": "triggering",
				},
			},
			expectedRepo:  "",
			expectedRepos: nil,
			expectedToken: "",
		},
		{
			name: "nil push-to-pull-request-branch config",
			configMap: map[string]any{
				"push-to-pull-request-branch": nil,
			},
			expectedRepo:  "",
			expectedRepos: nil,
			expectedToken: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := compiler.parsePushToPullRequestBranchConfig(tt.configMap)

			require.NotNil(t, cfg, "config should not be nil")
			assert.Equal(t, tt.expectedRepo, cfg.TargetRepoSlug, "TargetRepoSlug mismatch")
			assert.Equal(t, tt.expectedRepos, cfg.AllowedRepos, "AllowedRepos mismatch")
			assert.Equal(t, tt.expectedToken, cfg.GitHubToken, "GitHubToken mismatch")
		})
	}
}

// TestMergePullRequestConfigTargetRepo verifies that merge-pull-request
// correctly parses target-repo and allowed-repos fields.
func TestMergePullRequestConfigTargetRepo(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name           string
		configMap      map[string]any
		expectedRepo   string
		expectedRepos  []string
		expectedToken  string
		expectedTarget string
	}{
		{
			name: "target, target-repo and allowed-repos configured",
			configMap: map[string]any{
				"merge-pull-request": map[string]any{
					"target":        "*",
					"target-repo":   "githubnext/gh-aw-side-repo",
					"allowed-repos": []any{"githubnext/gh-aw-side-repo", "github/docs"},
					"github-token":  "${{ secrets.TEMP_USER_PAT }}",
				},
			},
			expectedTarget: "*",
			expectedRepo:   "githubnext/gh-aw-side-repo",
			expectedRepos:  []string{"githubnext/gh-aw-side-repo", "github/docs"},
			expectedToken:  "${{ secrets.TEMP_USER_PAT }}",
		},
		{
			name: "no cross-repo config",
			configMap: map[string]any{
				"merge-pull-request": map[string]any{
					"required-labels": []any{"safe-to-merge"},
				},
			},
			expectedTarget: "",
			expectedRepo:   "",
			expectedRepos:  nil,
			expectedToken:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := compiler.parseMergePullRequestConfig(tt.configMap)

			require.NotNil(t, cfg, "config should not be nil")
			assert.Equal(t, tt.expectedTarget, cfg.Target, "Target mismatch")
			assert.Equal(t, tt.expectedRepo, cfg.TargetRepoSlug, "TargetRepoSlug mismatch")
			assert.Equal(t, tt.expectedRepos, cfg.AllowedRepos, "AllowedRepos mismatch")
			assert.Equal(t, tt.expectedToken, cfg.GitHubToken, "GitHubToken mismatch")
		})
	}
}

// TestUpdateIssueConfigGitHubToken verifies that update-issue correctly parses the github-token field.
func TestUpdateIssueConfigGitHubToken(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name          string
		configMap     map[string]any
		expectedToken string
		expectedRepo  string
		expectedRepos []string
	}{
		{
			name: "github-token and allowed-repos configured",
			configMap: map[string]any{
				"update-issue": map[string]any{
					"target-repo":   "githubnext/gh-aw-side-repo",
					"allowed-repos": []any{"githubnext/gh-aw-side-repo"},
					"github-token":  "${{ secrets.TEMP_USER_PAT }}",
				},
			},
			expectedToken: "${{ secrets.TEMP_USER_PAT }}",
			expectedRepo:  "githubnext/gh-aw-side-repo",
			expectedRepos: []string{"githubnext/gh-aw-side-repo"},
		},
		{
			name: "no token or cross-repo",
			configMap: map[string]any{
				"update-issue": map[string]any{
					"body": true,
				},
			},
			expectedToken: "",
			expectedRepo:  "",
			expectedRepos: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := compiler.parseUpdateIssuesConfig(tt.configMap)

			require.NotNil(t, cfg, "config should not be nil")
			assert.Equal(t, tt.expectedToken, cfg.GitHubToken, "GitHubToken mismatch")
			assert.Equal(t, tt.expectedRepo, cfg.TargetRepoSlug, "TargetRepoSlug mismatch")
			assert.Equal(t, tt.expectedRepos, cfg.AllowedRepos, "AllowedRepos mismatch")
		})
	}
}

// TestAddCommentGitHubTokenInHandlerConfig verifies that github-token is included in
// the handler manager config JSON for add-comment.
func TestAddCommentGitHubTokenInHandlerConfig(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name: "Test",
		SafeOutputs: &SafeOutputsConfig{
			AddComments: &AddCommentsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					GitHubToken: "${{ secrets.TEMP_USER_PAT }}",
				},
				TargetRepoSlug: "githubnext/gh-aw-side-repo",
				AllowedRepos:   []string{"githubnext/gh-aw-side-repo"},
			},
		},
	}

	var steps []string
	compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)

	require.NotEmpty(t, steps, "steps should not be empty")
	stepsContent := strings.Join(steps, "")

	// Extract and parse the handler config JSON
	handlerConfig := extractHandlerConfig(t, stepsContent)

	addComment, ok := handlerConfig["add_comment"]
	require.True(t, ok, "add_comment config should be present")

	assert.Equal(t, "${{ secrets.TEMP_USER_PAT }}", addComment["github-token"], "github-token should be in handler config")
	assert.Equal(t, "githubnext/gh-aw-side-repo", addComment["target-repo"], "target-repo should be in handler config")

	allowedRepos, ok := addComment["allowed_repos"]
	require.True(t, ok, "allowed_repos should be present")
	assert.Contains(t, allowedRepos, "githubnext/gh-aw-side-repo", "allowed_repos should contain the repo")
}

// TestCreateIssueGitHubTokenInHandlerConfig verifies that github-token is included in
// the handler manager config JSON for create-issue.
func TestCreateIssueGitHubTokenInHandlerConfig(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name: "Test",
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					GitHubToken: "${{ secrets.TEMP_USER_PAT }}",
				},
				TargetRepoSlug: "githubnext/gh-aw-side-repo",
				AllowedRepos:   []string{"githubnext/gh-aw-side-repo"},
			},
		},
	}

	var steps []string
	compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)

	require.NotEmpty(t, steps)
	handlerConfig := extractHandlerConfig(t, strings.Join(steps, ""))

	createIssue, ok := handlerConfig["create_issue"]
	require.True(t, ok, "create_issue config should be present")

	assert.Equal(t, "${{ secrets.TEMP_USER_PAT }}", createIssue["github-token"], "github-token should be in handler config")
	assert.Equal(t, "githubnext/gh-aw-side-repo", createIssue["target-repo"], "target-repo should be in handler config")
}

// TestCreateDiscussionGitHubTokenInHandlerConfig verifies that github-token is included in
// the handler manager config JSON for create-discussion.
func TestCreateDiscussionGitHubTokenInHandlerConfig(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name: "Test",
		SafeOutputs: &SafeOutputsConfig{
			CreateDiscussions: &CreateDiscussionsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					GitHubToken: "${{ secrets.TEMP_USER_PAT }}",
				},
				TargetRepoSlug: "githubnext/gh-aw-side-repo",
				AllowedRepos:   []string{"githubnext/gh-aw-side-repo"},
			},
		},
	}

	var steps []string
	compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)

	require.NotEmpty(t, steps)
	handlerConfig := extractHandlerConfig(t, strings.Join(steps, ""))

	createDiscussion, ok := handlerConfig["create_discussion"]
	require.True(t, ok, "create_discussion config should be present")

	assert.Equal(t, "${{ secrets.TEMP_USER_PAT }}", createDiscussion["github-token"], "github-token should be in handler config")
}

// TestCreateCodeScanningAlertCrossRepoInHandlerConfig verifies that target-repo, allowed-repos,
// and github-token are included in the handler manager config for create-code-scanning-alert.
func TestCreateCodeScanningAlertCrossRepoInHandlerConfig(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name: "Test",
		SafeOutputs: &SafeOutputsConfig{
			CreateCodeScanningAlerts: &CreateCodeScanningAlertsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					GitHubToken: "${{ secrets.TEMP_USER_PAT }}",
				},
				TargetRepoSlug: "githubnext/gh-aw-side-repo",
				AllowedRepos:   []string{"githubnext/gh-aw-side-repo"},
				Driver:         "test-scanner",
			},
		},
	}

	var steps []string
	compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)

	require.NotEmpty(t, steps)
	handlerConfig := extractHandlerConfig(t, strings.Join(steps, ""))

	alert, ok := handlerConfig["create_code_scanning_alert"]
	require.True(t, ok, "create_code_scanning_alert config should be present")

	assert.Equal(t, "${{ secrets.TEMP_USER_PAT }}", alert["github-token"], "github-token should be in handler config")
	assert.Equal(t, "githubnext/gh-aw-side-repo", alert["target-repo"], "target-repo should be in handler config")
	assert.Equal(t, "test-scanner", alert["driver"], "driver should be in handler config")

	allowedRepos, ok := alert["allowed_repos"]
	require.True(t, ok, "allowed_repos should be present")
	assert.Contains(t, allowedRepos, "githubnext/gh-aw-side-repo", "allowed_repos should contain the repo")
}

// TestUpdateIssueGitHubTokenInHandlerConfig verifies that github-token is included in
// the handler manager config JSON for update-issue.
func TestUpdateIssueGitHubTokenInHandlerConfig(t *testing.T) {
	compiler := NewCompiler()

	bodyVal := true
	workflowData := &WorkflowData{
		Name: "Test",
		SafeOutputs: &SafeOutputsConfig{
			UpdateIssues: &UpdateIssuesConfig{
				UpdateEntityConfig: UpdateEntityConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						GitHubToken: "${{ secrets.TEMP_USER_PAT }}",
					},
					SafeOutputTargetConfig: SafeOutputTargetConfig{
						TargetRepoSlug: "githubnext/gh-aw-side-repo",
						AllowedRepos:   []string{"githubnext/gh-aw-side-repo"},
					},
				},
				Body: &bodyVal,
			},
		},
	}

	var steps []string
	compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)

	require.NotEmpty(t, steps)
	handlerConfig := extractHandlerConfig(t, strings.Join(steps, ""))

	updateIssue, ok := handlerConfig["update_issue"]
	require.True(t, ok, "update_issue config should be present")

	assert.Equal(t, "${{ secrets.TEMP_USER_PAT }}", updateIssue["github-token"], "github-token should be in handler config")
	assert.Equal(t, "githubnext/gh-aw-side-repo", updateIssue["target-repo"], "target-repo should be in handler config")

	allowedRepos, ok := updateIssue["allowed_repos"]
	require.True(t, ok, "allowed_repos should be present")
	assert.Contains(t, allowedRepos, "githubnext/gh-aw-side-repo", "allowed_repos should contain the repo")
}

// TestMergePullRequestCrossRepoInHandlerConfig verifies that target-repo and allowed-repos
// are included in the handler manager config JSON for merge-pull-request.
func TestMergePullRequestCrossRepoInHandlerConfig(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name: "Test",
		SafeOutputs: &SafeOutputsConfig{
			MergePullRequest: &MergePullRequestConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					GitHubToken: "${{ secrets.TEMP_USER_PAT }}",
				},
				SafeOutputTargetConfig: SafeOutputTargetConfig{
					Target:         "*",
					TargetRepoSlug: "githubnext/gh-aw-side-repo",
					AllowedRepos:   []string{"githubnext/gh-aw-side-repo"},
				},
			},
		},
	}

	var steps []string
	compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)

	require.NotEmpty(t, steps)
	handlerConfig := extractHandlerConfig(t, strings.Join(steps, ""))

	mergePR, ok := handlerConfig["merge_pull_request"]
	require.True(t, ok, "merge_pull_request config should be present")

	assert.Equal(t, "*", mergePR["target"], "target should be in handler config")

	assert.Equal(t, "githubnext/gh-aw-side-repo", mergePR["target-repo"], "target-repo should be in handler config")

	allowedRepos, ok := mergePR["allowed_repos"]
	require.True(t, ok, "allowed_repos should be present")
	assert.Contains(t, allowedRepos, "githubnext/gh-aw-side-repo", "allowed_repos should contain the repo")
}

// TestPushToPullRequestBranchCrossRepoInHandlerConfig verifies that target-repo and allowed-repos
// are included in the handler manager config JSON for push-to-pull-request-branch.
func TestPushToPullRequestBranchCrossRepoInHandlerConfig(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name: "Test",
		SafeOutputs: &SafeOutputsConfig{
			PushToPullRequestBranch: &PushToPullRequestBranchConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					GitHubToken: "${{ secrets.TEMP_USER_PAT }}",
				},
				TargetRepoSlug: "githubnext/gh-aw-side-repo",
				AllowedRepos:   []string{"githubnext/gh-aw-side-repo"},
			},
		},
	}

	var steps []string
	compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)

	require.NotEmpty(t, steps)
	handlerConfig := extractHandlerConfig(t, strings.Join(steps, ""))

	pushBranch, ok := handlerConfig["push_to_pull_request_branch"]
	require.True(t, ok, "push_to_pull_request_branch config should be present")

	assert.Equal(t, "githubnext/gh-aw-side-repo", pushBranch["target-repo"], "target-repo should be in handler config")

	allowedRepos, ok := pushBranch["allowed_repos"]
	require.True(t, ok, "allowed_repos should be present")
	assert.Contains(t, allowedRepos, "githubnext/gh-aw-side-repo", "allowed_repos should contain the repo")
}

// TestDispatchWorkflowCrossRepoInHandlerConfig verifies that target-repo, allowed-repos,
// and github-token are included in the handler manager config JSON for dispatch-workflow.
func TestDispatchWorkflowCrossRepoInHandlerConfig(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name: "Test",
		SafeOutputs: &SafeOutputsConfig{
			DispatchWorkflow: &DispatchWorkflowConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					GitHubToken: "${{ secrets.TEMP_USER_PAT }}",
				},
				Workflows:      []string{"worker"},
				TargetRepoSlug: "githubnext/gh-aw-side-repo",
				AllowedRepos:   []string{"githubnext/gh-aw-side-repo"},
				AllowedRefs:    []string{"refs/heads/release/*"},
			},
		},
	}

	var steps []string
	compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)

	require.NotEmpty(t, steps)
	handlerConfig := extractHandlerConfig(t, strings.Join(steps, ""))

	dispatchWorkflow, ok := handlerConfig["dispatch_workflow"]
	require.True(t, ok, "dispatch_workflow config should be present")

	assert.Equal(t, "${{ secrets.TEMP_USER_PAT }}", dispatchWorkflow["github-token"], "github-token should be in handler config")
	assert.Equal(t, "githubnext/gh-aw-side-repo", dispatchWorkflow["target-repo"], "target-repo should be in handler config")

	allowedRepos, ok := dispatchWorkflow["allowed_repos"]
	require.True(t, ok, "allowed_repos should be present")
	assert.Contains(t, allowedRepos, "githubnext/gh-aw-side-repo", "allowed_repos should contain the repo")

	allowedRefs, ok := dispatchWorkflow["allowed_refs"]
	require.True(t, ok, "allowed_refs should be present")
	assert.Contains(t, allowedRefs, "refs/heads/release/*", "allowed_refs should contain the ref glob")
}

func TestWorkflowCallDispatchRepoFallsBackWhenActivationOutputMissing(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		On: "on:\n  workflow_call:\n",
		SafeOutputs: &SafeOutputsConfig{
			DispatchWorkflow: &DispatchWorkflowConfig{Workflows: []string{"worker"}},
		},
	}

	var steps []string
	compiler.addHandlerManagerConfigEnvVar(&steps, data)
	config := extractHandlerConfig(t, strings.Join(steps, ""))
	assert.Equal(t, "${{ needs.activation.outputs.target_repo || github.repository }}",
		config["dispatch_workflow"]["target-repo"])
}

// TestHandlerManagerStepPerOutputTokenInHandlerConfig verifies that per-output tokens
// (e.g., add-comment.github-token) are wired into the handler config JSON (GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG)
// but NOT used as the step-level with.github-token. The step-level token follows the same
// precedence as github_token.go: project token > global safe-outputs token > magic secrets.
func TestHandlerManagerStepPerOutputTokenInHandlerConfig(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name                      string
		safeOutputs               *SafeOutputsConfig
		expectedInHandlerConfig   []string // tokens that should appear in handler config JSON
		expectedNotInWithToken    string   // token that should NOT be in with.github-token (per-output tokens)
		expectedStepLevelFallback string   // step-level token should use this instead
	}{
		{
			name: "add-comment token appears in handler config JSON, not step-level",
			safeOutputs: &SafeOutputsConfig{
				AddComments: &AddCommentsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						GitHubToken: "${{ secrets.TEMP_USER_PAT }}",
					},
					TargetRepoSlug: "githubnext/gh-aw-side-repo",
					AllowedRepos:   []string{"githubnext/gh-aw-side-repo"},
				},
			},
			expectedInHandlerConfig:   []string{"TEMP_USER_PAT"},
			expectedNotInWithToken:    "github-token: ${{ secrets.TEMP_USER_PAT }}",
			expectedStepLevelFallback: "GH_AW_GITHUB_TOKEN",
		},
		{
			name: "create-issue token appears in handler config JSON, not step-level",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						GitHubToken: "${{ secrets.TEMP_USER_PAT }}",
					},
					TargetRepoSlug: "githubnext/gh-aw-side-repo",
					AllowedRepos:   []string{"githubnext/gh-aw-side-repo"},
				},
			},
			expectedInHandlerConfig:   []string{"TEMP_USER_PAT"},
			expectedNotInWithToken:    "github-token: ${{ secrets.TEMP_USER_PAT }}",
			expectedStepLevelFallback: "GH_AW_GITHUB_TOKEN",
		},
		{
			name: "global safe-outputs token is used for step-level with.github-token",
			safeOutputs: &SafeOutputsConfig{
				GitHubToken: "${{ secrets.GLOBAL_PAT }}",
				AddComments: &AddCommentsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						GitHubToken: "${{ secrets.PER_OUTPUT_PAT }}",
					},
				},
			},
			// Both tokens should appear in step content
			expectedInHandlerConfig: []string{"GLOBAL_PAT", "PER_OUTPUT_PAT"},
			// Step-level should use global, not per-output
			expectedNotInWithToken:    "github-token: ${{ secrets.PER_OUTPUT_PAT }}",
			expectedStepLevelFallback: "GLOBAL_PAT",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowData := &WorkflowData{
				Name:        "Test",
				SafeOutputs: tt.safeOutputs,
			}

			steps, err := compiler.buildHandlerManagerStep(workflowData)
			require.NoError(t, err)
			stepsContent := strings.Join(steps, "")

			// Verify tokens appear somewhere in the step content (handler config JSON)
			for _, token := range tt.expectedInHandlerConfig {
				assert.Contains(t, stepsContent, token,
					"handler manager step should include token %q in step content", token)
			}

			// Verify per-output token is NOT used as the step-level with.github-token
			if tt.expectedNotInWithToken != "" {
				assert.NotContains(t, stepsContent, tt.expectedNotInWithToken,
					"per-output token should not be used directly as step-level with.github-token")
			}

			// Verify the step-level token uses the expected fallback
			if tt.expectedStepLevelFallback != "" {
				// Extract just the "with:" section to check step-level token
				withIdx := strings.Index(stepsContent, "        with:\n")
				if withIdx >= 0 {
					withSection := stepsContent[withIdx : withIdx+200]
					assert.Contains(t, withSection, tt.expectedStepLevelFallback,
						"step-level with.github-token should use %q", tt.expectedStepLevelFallback)
				}
			}
		})
	}
}

// TestParseAllowedRepos verifies the shared array parser for allowed-repos.
func TestParseAllowedRepos(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		expected []string
	}{
		{
			name: "single repo as array",
			input: map[string]any{
				"allowed-repos": []any{"owner/repo"},
			},
			expected: []string{"owner/repo"},
		},
		{
			name: "multiple repos",
			input: map[string]any{
				"allowed-repos": []any{"owner/repo1", "owner/repo2", "other-owner/repo3"},
			},
			expected: []string{"owner/repo1", "owner/repo2", "other-owner/repo3"},
		},
		{
			name: "allowed-repos as []string",
			input: map[string]any{
				"allowed-repos": []string{"owner/repo1", "owner/repo2"},
			},
			expected: []string{"owner/repo1", "owner/repo2"},
		},
		{
			name:     "no allowed-repos key",
			input:    map[string]any{},
			expected: nil,
		},
		{
			name: "empty allowed-repos array",
			input: map[string]any{
				"allowed-repos": []any{},
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseStringArrayFromConfig(tt.input, "allowed-repos", nil)
			if tt.expected == nil {
				assert.Emptyf(t, result, "ParseStringArrayFromConfig should return nil or empty for: %s", tt.name)
			} else {
				assert.Equal(t, tt.expected, result, "ParseStringArrayFromConfig mismatch")
			}
		})
	}
}

// TestLinkSubIssueConfigTargetRepo verifies that link-sub-issue correctly parses
// target-repo and allowed-repos fields, including GitHub Actions expressions for allowed-repos.
func TestLinkSubIssueConfigTargetRepo(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name          string
		configMap     map[string]any
		expectedRepo  string
		expectedRepos []string
		expectedToken string
	}{
		{
			name: "target-repo and allowed-repos configured",
			configMap: map[string]any{
				"link-sub-issue": map[string]any{
					"target-repo":   "githubnext/gh-aw-side-repo",
					"allowed-repos": []any{"githubnext/gh-aw-side-repo"},
					"github-token":  "${{ secrets.TEMP_USER_PAT }}",
				},
			},
			expectedRepo:  "githubnext/gh-aw-side-repo",
			expectedRepos: []string{"githubnext/gh-aw-side-repo"},
			expectedToken: "${{ secrets.TEMP_USER_PAT }}",
		},
		{
			name: "multiple allowed repos",
			configMap: map[string]any{
				"link-sub-issue": map[string]any{
					"target-repo":   "org/primary-repo",
					"allowed-repos": []any{"org/primary-repo", "org/secondary-repo"},
				},
			},
			expectedRepo:  "org/primary-repo",
			expectedRepos: []string{"org/primary-repo", "org/secondary-repo"},
			expectedToken: "",
		},
		{
			name: "allowed-repos as GitHub Actions expression",
			configMap: map[string]any{
				"link-sub-issue": map[string]any{
					"target-repo":   "${{ inputs.target_repo }}",
					"allowed-repos": "${{ inputs['allowed-repos'] }}",
				},
			},
			expectedRepo:  "${{ inputs.target_repo }}",
			expectedRepos: []string{"${{ inputs['allowed-repos'] }}"},
			expectedToken: "",
		},
		{
			name: "no cross-repo config",
			configMap: map[string]any{
				"link-sub-issue": map[string]any{
					"max": 5,
				},
			},
			expectedRepo:  "",
			expectedRepos: nil,
			expectedToken: "",
		},
		{
			name: "nil config value",
			configMap: map[string]any{
				"link-sub-issue": nil,
			},
			expectedRepo:  "",
			expectedRepos: nil,
			expectedToken: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := compiler.parseLinkSubIssueConfig(tt.configMap)

			require.NotNil(t, cfg, "config should not be nil")
			assert.Equal(t, tt.expectedRepo, cfg.TargetRepoSlug, "TargetRepoSlug mismatch")
			assert.Equal(t, tt.expectedRepos, cfg.AllowedRepos, "AllowedRepos mismatch")
			assert.Equal(t, tt.expectedToken, cfg.GitHubToken, "GitHubToken mismatch")
		})
	}
}

// extractHandlerConfig is a helper that parses the GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG
// JSON from the rendered step strings.
func extractHandlerConfig(t *testing.T, stepsContent string) map[string]map[string]any {
	t.Helper()

	var configJSON string
	for line := range strings.SplitSeq(stepsContent, "\n") {
		if strings.Contains(line, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
			parts := strings.SplitN(line, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ", 2)
			if len(parts) == 2 {
				configJSON = strings.TrimSpace(parts[1])
				configJSON = strings.Trim(configJSON, "\"")
				configJSON = strings.ReplaceAll(configJSON, "\\\"", "\"")
				break
			}
		}
	}

	require.NotEmpty(t, configJSON, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG env var not found in steps")

	var result map[string]map[string]any
	err := json.Unmarshal([]byte(configJSON), &result)
	require.NoError(t, err, "Handler config JSON should be valid: %s", configJSON)

	return result
}
