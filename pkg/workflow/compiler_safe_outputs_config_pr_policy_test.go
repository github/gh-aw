//go:build !integration

package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ========================================
// Pull Request Policy and Fork-Backed PR Tests
// ========================================

// TestCreateReportIncompleteIssueTemplatableBool tests that create-issue in report-incomplete
// correctly handles literal booleans and GitHub Actions expressions.
func TestCreateReportIncompleteIssueTemplatableBool(t *testing.T) {
	compiler := NewCompiler()

	extractHandlerConfig := func(t *testing.T, safeOutputs *SafeOutputsConfig) map[string]any {
		t.Helper()
		workflowData := &WorkflowData{Name: "Test", SafeOutputs: safeOutputs}
		var steps []string
		compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)
		for _, step := range steps {
			if strings.Contains(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
				parts := strings.Split(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
				if len(parts) == 2 {
					jsonStr := strings.TrimSpace(parts[1])
					jsonStr = strings.Trim(jsonStr, "\"")
					jsonStr = strings.ReplaceAll(jsonStr, "\\\"", "\"")
					var config map[string]any
					require.NoError(t, json.Unmarshal([]byte(jsonStr), &config), "config JSON should be valid")
					return config
				}
			}
		}
		return nil
	}

	t.Run("create-issue nil (default) includes handler", func(t *testing.T) {
		config := extractHandlerConfig(t, &SafeOutputsConfig{
			ReportIncomplete: &ReportIncompleteConfig{},
		})
		require.NotNil(t, config)
		_, hasHandler := config["create_report_incomplete_issue"]
		assert.True(t, hasHandler, "create_report_incomplete_issue should be present when create-issue is nil (default)")
	})

	t.Run("create-issue true includes handler without create-issue field", func(t *testing.T) {
		trueVal := "true"
		config := extractHandlerConfig(t, &SafeOutputsConfig{
			ReportIncomplete: &ReportIncompleteConfig{CreateIssue: &trueVal},
		})
		require.NotNil(t, config)
		handlerCfg, hasHandler := config["create_report_incomplete_issue"]
		require.True(t, hasHandler, "create_report_incomplete_issue should be present when create-issue is true")
		handlerMap, ok := handlerCfg.(map[string]any)
		require.True(t, ok)
		_, hasCreateIssueField := handlerMap["create-issue"]
		assert.False(t, hasCreateIssueField, "create-issue field should not be in handler config for literal true")
	})

	t.Run("create-issue false excludes handler", func(t *testing.T) {
		falseVal := "false"
		config := extractHandlerConfig(t, &SafeOutputsConfig{
			ReportIncomplete: &ReportIncompleteConfig{CreateIssue: &falseVal},
		})
		require.NotNil(t, config)
		_, hasHandler := config["create_report_incomplete_issue"]
		assert.False(t, hasHandler, "create_report_incomplete_issue should be absent when create-issue is false")
	})

	t.Run("create-issue expression includes handler with create-issue expression field", func(t *testing.T) {
		expr := "${{ inputs.create-incomplete-issue }}"
		config := extractHandlerConfig(t, &SafeOutputsConfig{
			ReportIncomplete: &ReportIncompleteConfig{CreateIssue: &expr},
		})
		require.NotNil(t, config)
		handlerCfg, hasHandler := config["create_report_incomplete_issue"]
		require.True(t, hasHandler, "create_report_incomplete_issue should be present when create-issue is an expression")
		handlerMap, ok := handlerCfg.(map[string]any)
		require.True(t, ok)
		// Note: the JSON key is "create-issue" (hyphen); the JS handler manager normalises
		// hyphens to underscores at runtime, so handlers see "create_issue".
		createIssueVal, hasCreateIssueField := handlerMap["create-issue"]
		assert.True(t, hasCreateIssueField, "create-issue field should be in handler config for expression")
		assert.Equal(t, expr, createIssueVal, "create-issue field should carry the expression string")
	})
}

// TestPRPolicyFieldsExpressionsPassThrough verifies that GitHub Actions expression strings
// set on protected-files and patch-format are emitted verbatim into the handler config.
// This enables reusable workflow_call workflows to parameterise these policy fields per caller.
func TestProtectedFilesPolicyDefaultsAreHyphenated(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		safeOutputs *SafeOutputsConfig
		handlerKey  string
	}{
		{
			name: "create-pull-request defaults to request-review",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")}},
			},
			handlerKey: "create_pull_request",
		},
		{
			name: "push-to-pull-request-branch defaults to request-review",
			safeOutputs: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")}},
			},
			handlerKey: "push_to_pull_request_branch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			workflowData := &WorkflowData{Name: "Test Workflow", SafeOutputs: tt.safeOutputs}
			var steps []string
			compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)
			require.NotEmpty(t, steps, "should produce config steps")

			var configJSON string
			for _, step := range steps {
				if strings.Contains(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
					parts := strings.Split(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
					require.Len(t, parts, 2, "should split env var line")
					configJSON = strings.TrimSpace(parts[1])
					configJSON = strings.Trim(configJSON, "\"")
					configJSON = strings.ReplaceAll(configJSON, "\\\"", "\"")
				}
			}
			require.NotEmpty(t, configJSON, "should have extracted JSON")

			var config map[string]map[string]any
			require.NoError(t, json.Unmarshal([]byte(configJSON), &config), "config JSON should be valid")

			handlerConfig, ok := config[tt.handlerKey]
			require.True(t, ok, "should have %s config", tt.handlerKey)

			pfPolicy, ok := handlerConfig["protected_files_policy"]
			require.True(t, ok, "should have protected_files_policy field")
			assert.Equal(t, "request-review", pfPolicy, "default protected_files_policy should use the hyphenated request-review form")
		})
	}
}

func TestPRPolicyFieldsExpressionsPassThrough(t *testing.T) {
	t.Parallel()

	protectedFilesExpr := "${{ inputs.protected-files-policy }}"
	patchFormatExpr := "${{ inputs.patch-format }}"

	tests := []struct {
		name          string
		safeOutputs   *SafeOutputsConfig
		handlerKey    string
		wantProtected string
		wantFormat    string
	}{
		{
			name: "create-pull-request: expression values pass through",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
					ManifestFilesPolicy:  &protectedFilesExpr,
					PatchFormat:          patchFormatExpr,
				},
			},
			handlerKey:    "create_pull_request",
			wantProtected: protectedFilesExpr,
			wantFormat:    patchFormatExpr,
		},
		{
			name: "push-to-pull-request-branch: expression values pass through",
			safeOutputs: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
					ManifestFilesPolicy:  &protectedFilesExpr,
					PatchFormat:          patchFormatExpr,
				},
			},
			handlerKey:    "push_to_pull_request_branch",
			wantProtected: protectedFilesExpr,
			wantFormat:    patchFormatExpr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			workflowData := &WorkflowData{
				Name:        "Test Workflow",
				SafeOutputs: tt.safeOutputs,
			}

			var steps []string
			compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)
			require.NotEmpty(t, steps, "should produce config steps")

			// Extract handler-config JSON
			var configJSON string
			for _, step := range steps {
				if strings.Contains(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
					parts := strings.Split(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
					require.Len(t, parts, 2, "should split env var line")
					configJSON = strings.TrimSpace(parts[1])
					configJSON = strings.Trim(configJSON, "\"")
					configJSON = strings.ReplaceAll(configJSON, "\\\"", "\"")
				}
			}
			require.NotEmpty(t, configJSON, "should have extracted JSON")

			var config map[string]map[string]any
			require.NoError(t, json.Unmarshal([]byte(configJSON), &config), "config JSON should be valid")

			handlerConfig, ok := config[tt.handlerKey]
			require.True(t, ok, "should have %s config", tt.handlerKey)

			// protected_files_policy must contain the expression verbatim
			pfPolicy, ok := handlerConfig["protected_files_policy"]
			require.True(t, ok, "should have protected_files_policy field")
			assert.Equal(t, tt.wantProtected, pfPolicy, "protected_files_policy should contain the expression")

			// patch_format must contain the expression verbatim
			patchFmt, ok := handlerConfig["patch_format"]
			require.True(t, ok, "should have patch_format field")
			assert.Equal(t, tt.wantFormat, patchFmt, "patch_format should contain the expression")
		})
	}
}

func TestForkBackedPRFieldsPassThrough(t *testing.T) {
	t.Parallel()

	headRepoExpr := "${{ inputs.head-repo }}"
	headTokenExpr := "${{ secrets.FORK_PAT }}"

	tests := []struct {
		name        string
		safeOutputs *SafeOutputsConfig
		handlerKey  string
	}{
		{
			name: "create-pull-request emits fork head fields",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
					HeadRepoSlug:         headRepoExpr,
					HeadGitHubToken:      headTokenExpr,
				},
			},
			handlerKey: "create_pull_request",
		},
		{
			name: "push-to-pull-request-branch emits fork head fields",
			safeOutputs: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
					HeadRepoSlug:         headRepoExpr,
					HeadGitHubToken:      headTokenExpr,
				},
			},
			handlerKey: "push_to_pull_request_branch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			workflowData := &WorkflowData{
				Name:        "Test Workflow",
				SafeOutputs: tt.safeOutputs,
			}

			var steps []string
			compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)
			require.NotEmpty(t, steps, "should produce config steps")

			var configJSON string
			for _, step := range steps {
				if strings.Contains(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
					parts := strings.Split(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
					require.Len(t, parts, 2, "should split env var line")
					configJSON = strings.TrimSpace(parts[1])
					configJSON = strings.Trim(configJSON, "\"")
					configJSON = strings.ReplaceAll(configJSON, "\\\"", "\"")
				}
			}
			require.NotEmpty(t, configJSON, "should have extracted JSON")

			var config map[string]map[string]any
			require.NoError(t, json.Unmarshal([]byte(configJSON), &config), "config JSON should be valid")

			handlerConfig, ok := config[tt.handlerKey]
			require.True(t, ok, "should have %s config", tt.handlerKey)
			assert.Equal(t, headRepoExpr, handlerConfig["head-repo"])
			assert.Equal(t, headTokenExpr, handlerConfig["head-github-token"])
		})
	}
}

func TestForkBackedPRHeadGitHubAppFieldsPassThrough(t *testing.T) {
	t.Parallel()

	headRepoExpr := "automation-owner/vscode"
	headAppID := "${{ vars.HEAD_APP_ID }}"
	headAppKey := "${{ secrets.HEAD_APP_PRIVATE_KEY }}"
	// When head-github-app is configured, the handler config should receive the step
	// expression rather than a raw token value.
	expectedHeadTokenExpr := "${{ steps.safe-outputs-head-app-token.outputs.token }}"

	tests := []struct {
		name        string
		safeOutputs *SafeOutputsConfig
		handlerKey  string
	}{
		{
			name: "create-pull-request emits app-minted head token expression",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
					HeadRepoSlug:         headRepoExpr,
					HeadGitHubApp: &GitHubAppConfig{
						AppID:      headAppID,
						PrivateKey: headAppKey,
					},
				},
			},
			handlerKey: "create_pull_request",
		},
		{
			name: "push-to-pull-request-branch emits app-minted head token expression",
			safeOutputs: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
					HeadRepoSlug:         headRepoExpr,
					HeadGitHubApp: &GitHubAppConfig{
						AppID:      headAppID,
						PrivateKey: headAppKey,
					},
				},
			},
			handlerKey: "push_to_pull_request_branch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			workflowData := &WorkflowData{
				Name:        "Test Workflow",
				SafeOutputs: tt.safeOutputs,
			}

			var steps []string
			compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)
			require.NotEmpty(t, steps, "should produce config steps")

			var configJSON string
			for _, step := range steps {
				if strings.Contains(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
					parts := strings.Split(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
					require.Len(t, parts, 2, "should split env var line")
					configJSON = strings.TrimSpace(parts[1])
					configJSON = strings.Trim(configJSON, "\"")
					configJSON = strings.ReplaceAll(configJSON, "\\\"", "\"")
				}
			}
			require.NotEmpty(t, configJSON, "should have extracted JSON")

			var config map[string]map[string]any
			require.NoError(t, json.Unmarshal([]byte(configJSON), &config), "config JSON should be valid")

			handlerConfig, ok := config[tt.handlerKey]
			require.True(t, ok, "should have %s config", tt.handlerKey)
			assert.Equal(t, headRepoExpr, handlerConfig["head-repo"])
			assert.Equal(t, expectedHeadTokenExpr, handlerConfig["head-github-token"],
				"head-github-app should produce the safe-outputs-head-app-token step expression")
		})
	}
}

func TestCreatePullRequestProtectedFilesPolicyDefault(t *testing.T) {
	t.Parallel()

	compiler := NewCompiler()
	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			CreatePullRequests: &CreatePullRequestsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
			},
		},
	}
	var steps []string
	compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)
	require.NotEmpty(t, steps, "should produce config steps")

	var configJSON string
	for _, step := range steps {
		if strings.Contains(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
			parts := strings.Split(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
			require.Len(t, parts, 2, "should split env var line")
			configJSON = strings.TrimSpace(parts[1])
			configJSON = strings.Trim(configJSON, "\"")
			configJSON = strings.ReplaceAll(configJSON, "\\\"", "\"")
		}
	}
	require.NotEmpty(t, configJSON, "should have extracted JSON")

	var config map[string]map[string]any
	require.NoError(t, json.Unmarshal([]byte(configJSON), &config), "config JSON should be valid")

	handlerCfg, ok := config["create_pull_request"]
	require.True(t, ok, "create_pull_request handler config should be present")
	assert.Equal(t, "request-review", handlerCfg["protected_files_policy"], "default protected-files mode should be request-review")
}

// TestDispatchWorkflowRelayInjectsDispatchCompatibleRef verifies that when a workflow_call
// trigger is present and dispatch_workflow safe-outputs are configured, the compiler injects
// needs.activation.outputs.target_ref (the dispatch-compatible branch/tag ref) — not
// needs.activation.outputs.target_checkout_ref (the SHA) — as the target-ref for dispatch.
// Sending a SHA to createWorkflowDispatch causes "No ref found for: <sha>" errors.
func TestDispatchWorkflowRelayInjectsDispatchCompatibleRef(t *testing.T) {
	compiler := NewCompiler(WithVersion("dev"))
	compiler.SetActionMode(ActionModeDev)

	safeOutputs := &SafeOutputsConfig{
		DispatchWorkflow: &DispatchWorkflowConfig{
			BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
			Workflows:            []string{"repo-worker"},
		},
	}

	data := &WorkflowData{
		Name: "test-relay",
		On: `"on":
  workflow_call:
  workflow_dispatch:`,
		SafeOutputs: safeOutputs,
		AI:          "copilot",
	}

	var steps []string
	compiler.addHandlerManagerConfigEnvVar(&steps, data)
	require.NotEmpty(t, steps, "should produce at least one step env var")

	stepsContent := strings.Join(steps, "\n")

	// target_ref (dispatch-compatible branch/tag) must be injected
	assert.Contains(t, stepsContent, "needs.activation.outputs.target_ref",
		"dispatch target-ref must use needs.activation.outputs.target_ref (branch/tag ref)")

	// target_checkout_ref (SHA) must NOT be used as the dispatch ref
	assert.NotContains(t, stepsContent, "target_checkout_ref",
		"dispatch target-ref must NOT use target_checkout_ref (commit SHA)")
}
