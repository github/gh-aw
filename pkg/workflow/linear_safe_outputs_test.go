//go:build !integration

package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractLinearSafeOutputsConfig(t *testing.T) {
	compiler := NewCompiler()
	config := compiler.extractSafeOutputsConfig(map[string]any{
		"safe-outputs": map[string]any{
			"linear-token": "${{ secrets.LINEAR_API_KEY }}",
			"linear-create-issue": map[string]any{
				"team-id":    "9cfb482a-81e3-4154-b5b9-2c805e70a02d",
				"project-id": "810f57a7e383",
			},
			"linear-add-comment": map[string]any{
				"target": "ENG-123",
			},
			"linear-update-issue": map[string]any{
				"target": "ENG-456",
				"title":  true,
			},
		},
	})

	require.NotNil(t, config)
	assert.Equal(t, "${{ secrets.LINEAR_API_KEY }}", config.LinearToken)
	require.NotNil(t, config.LinearCreateIssue)
	assert.Equal(t, "9cfb482a-81e3-4154-b5b9-2c805e70a02d", config.LinearCreateIssue.TeamID)
	assert.Equal(t, "810f57a7e383", config.LinearCreateIssue.ProjectID)
	assert.Equal(t, "1", *config.LinearCreateIssue.Max)
	require.NotNil(t, config.LinearAddComment)
	assert.Equal(t, "ENG-123", config.LinearAddComment.Target)
	require.NotNil(t, config.LinearUpdateIssue)
	assert.Equal(t, "ENG-456", config.LinearUpdateIssue.Target)
	require.NotNil(t, config.LinearUpdateIssue.Title)
	assert.True(t, *config.LinearUpdateIssue.Title)
}

func TestLinearHandlerConfigExcludesCredential(t *testing.T) {
	config := &SafeOutputsConfig{
		LinearToken: "${{ secrets.LINEAR_API_KEY }}",
		LinearCreateIssue: &LinearCreateIssueConfig{
			BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
			TeamID:               "${{ vars.LINEAR_TEAM_ID }}",
			ProjectID:            "810f57a7e383",
		},
		LinearAddComment: &LinearTargetConfig{
			BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("2")},
			Target:               "ENG-123",
		},
		LinearUpdateIssue: &LinearUpdateIssueConfig{
			LinearTargetConfig: LinearTargetConfig{Target: "ENG-456"},
			Title:              ptrBool(true),
		},
	}

	result, err := generateSafeOutputsConfig(&WorkflowData{SafeOutputs: config})
	require.NoError(t, err)
	assert.NotContains(t, result, "LINEAR_API_KEY")
	assert.NotContains(t, result, "linear-token")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	assert.Equal(t, "${{ vars.LINEAR_TEAM_ID }}", parsed["linear_create_issue"].(map[string]any)["team_id"])
	assert.Equal(t, "810f57a7e383", parsed["linear_create_issue"].(map[string]any)["project_id"])
	assert.Equal(t, "ENG-123", parsed["linear_add_comment"].(map[string]any)["target"])
	assert.Equal(t, true, parsed["linear_update_issue"].(map[string]any)["allow_title"])
}

func TestLinearSafeOutputsNeedNoGitHubWritePermissions(t *testing.T) {
	config := &SafeOutputsConfig{
		LinearCreateIssue: &LinearCreateIssueConfig{},
		LinearAddComment:  &LinearTargetConfig{},
		LinearUpdateIssue: &LinearUpdateIssueConfig{},
	}
	permissions := computePermissionsForSafeOutputs(config, false)
	require.NotNil(t, permissions)
	assert.Empty(t, permissions.permissions)
}

func TestLinearSafeOutputsPreventDefaultGitHubIssueInjection(t *testing.T) {
	data := &WorkflowData{
		WorkflowID: "linear",
		SafeOutputs: &SafeOutputsConfig{
			LinearAddComment: &LinearTargetConfig{Target: "ENG-123"},
		},
	}

	applyDefaultCreateIssue(data)
	assert.Nil(t, data.SafeOutputs.CreateIssues)
	assert.True(t, HasSafeOutputsEnabled(data.SafeOutputs))
}

func TestLinearOnlyDefaultsIncompleteReportingWithoutGitHubIssue(t *testing.T) {
	config := NewCompiler().extractSafeOutputsConfig(map[string]any{
		"safe-outputs": map[string]any{
			"linear-token":       "${{ secrets.LINEAR_API_KEY }}",
			"linear-add-comment": map[string]any{"target": "ENG-123"},
		},
	})

	require.NotNil(t, config.ReportIncomplete)
	require.NotNil(t, config.ReportIncomplete.CreateIssue)
	assert.Equal(t, "false", *config.ReportIncomplete.CreateIssue)
	assert.Empty(t, computePermissionsForSafeOutputs(config, false).permissions)
}

func TestLinearCredentialsOnlyAddedToTrustedProcessingStep(t *testing.T) {
	data := &WorkflowData{SafeOutputs: &SafeOutputsConfig{
		LinearToken:       "${{ secrets.LINEAR_API_KEY }}",
		LinearCreateIssue: &LinearCreateIssueConfig{TeamID: "9cfb482a-81e3-4154-b5b9-2c805e70a02d"},
		GitHubApp: &GitHubAppConfig{
			AppID:      "${{ vars.APP_ID }}",
			PrivateKey: "${{ secrets.APP_PRIVATE_KEY }}",
		},
	}}
	compiler := NewCompiler()
	steps, err := compiler.buildHandlerManagerStep(data)
	require.NoError(t, err)
	steps = injectLinearCredentialsIntoProcessorStep(steps, data.SafeOutputs)
	rendered := strings.Join(steps, "")

	assert.Contains(t, rendered, "GH_AW_LINEAR_TOKEN: ${{ secrets.LINEAR_API_KEY }}")
	assert.Contains(t, rendered, "LINEAR_PROJECT_ID: ${{ vars.LINEAR_PROJECT_ID }}")
	assert.Contains(t, rendered, "LINEAR_TEAM_ID: ${{ vars.LINEAR_TEAM_ID }}")
	assert.NotContains(t, rendered, "linear-token")
	processStep := rendered[strings.Index(rendered, "- name: Process Safe Outputs"):]
	assert.Contains(t, processStep, "env:\n          GH_AW_LINEAR_TOKEN:")
	assert.NotContains(t, rendered[:strings.Index(rendered, "- name: Process Safe Outputs")], "GH_AW_LINEAR_TOKEN")
}

func TestLinearSafeOutputUsesDefaultCredentials(t *testing.T) {
	config := &SafeOutputsConfig{
		LinearCreateIssue: &LinearCreateIssueConfig{TeamID: "9cfb482a-81e3-4154-b5b9-2c805e70a02d"},
	}
	steps := []string{
		"      - name: Process Safe Outputs\n",
		"        env:\n",
	}

	rendered := strings.Join(injectLinearCredentialsIntoProcessorStep(steps, config), "")
	assert.Contains(t, rendered, "GH_AW_LINEAR_TOKEN: ${{ secrets.LINEAR_API_KEY }}")
	assert.Contains(t, rendered, "LINEAR_PROJECT_ID: ${{ vars.LINEAR_PROJECT_ID }}")
	assert.Contains(t, rendered, "LINEAR_TEAM_ID: ${{ vars.LINEAR_TEAM_ID }}")
}

func TestLinearSafeOutputIDEnvOverrides(t *testing.T) {
	config := &SafeOutputsConfig{
		LinearCreateIssue: &LinearCreateIssueConfig{},
		Env: map[string]string{
			"LINEAR_PROJECT_ID": "${{ vars.OTHER_LINEAR_PROJECT_ID }}",
			"LINEAR_TEAM_ID":    "${{ vars.OTHER_LINEAR_TEAM_ID }}",
			"OTHER":             "value",
		},
	}
	steps := []string{
		"      - name: Process Safe Outputs\n",
		"        env:\n",
		"        with:\n",
	}

	rendered := strings.Join(injectLinearCredentialsIntoProcessorStep(steps, config), "")
	assert.Contains(t, rendered, "LINEAR_PROJECT_ID: ${{ vars.OTHER_LINEAR_PROJECT_ID }}")
	assert.Contains(t, rendered, "LINEAR_TEAM_ID: ${{ vars.OTHER_LINEAR_TEAM_ID }}")

	customSteps := []string{}
	NewCompiler().addCustomSafeOutputEnvVars(&customSteps, &WorkflowData{SafeOutputs: config})
	assert.Equal(t, "          OTHER: value\n", strings.Join(customSteps, ""))
}

// TestLinearOnlyWorkflowSkipsGlobalGitHubAppTokenMinting ensures that a Linear-only
// safe-outputs configuration does not mint an unrelated GitHub App installation token,
// even when a top-level (or auto-copied) SafeOutputs.GitHubApp is present. Minting a
// GitHub App token here would violate the credential-separation guarantee between
// Linear and GitHub App credentials.
func TestLinearOnlyWorkflowSkipsGlobalGitHubAppTokenMinting(t *testing.T) {
	data := &WorkflowData{SafeOutputs: &SafeOutputsConfig{
		LinearToken:       "${{ secrets.LINEAR_API_KEY }}",
		LinearCreateIssue: &LinearCreateIssueConfig{TeamID: "9cfb482a-81e3-4154-b5b9-2c805e70a02d"},
		GitHubApp: &GitHubAppConfig{
			AppID:      "${{ vars.APP_ID }}",
			PrivateKey: "${{ secrets.APP_PRIVATE_KEY }}",
		},
	}}
	compiler := NewCompiler()
	outputs := map[string]string{}
	steps := compiler.buildPreambleTokenSteps(data, outputs)

	rendered := strings.Join(steps, "")
	assert.NotContains(t, rendered, "safe-outputs-app-token")
	assert.NotContains(t, outputs, "app_token_minting_failed")
}

// TestGitHubHandlerWithGlobalAppStillMintsToken is the control case for
// TestLinearOnlyWorkflowSkipsGlobalGitHubAppTokenMinting: when a GitHub-backed handler is
// enabled alongside the global GitHub App, the app-token minting step must still be built.
func TestGitHubHandlerWithGlobalAppStillMintsToken(t *testing.T) {
	data := &WorkflowData{SafeOutputs: &SafeOutputsConfig{
		AddComments: &AddCommentsConfig{},
		GitHubApp: &GitHubAppConfig{
			AppID:      "${{ vars.APP_ID }}",
			PrivateKey: "${{ secrets.APP_PRIVATE_KEY }}",
		},
	}}
	compiler := NewCompiler()
	outputs := map[string]string{}
	steps := compiler.buildPreambleTokenSteps(data, outputs)

	rendered := strings.Join(steps, "")
	assert.Contains(t, rendered, "safe-outputs-app-token")
	assert.Contains(t, outputs, "app_token_minting_failed")
}
