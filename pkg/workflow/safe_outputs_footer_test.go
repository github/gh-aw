//go:build !integration

package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFooterConfiguration(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"name": "Test",
		"safe-outputs": map[string]any{
			"create-issue": map[string]any{"footer": false},
		},
	}
	config := compiler.extractSafeOutputsConfig(frontmatter)
	require.NotNil(t, config)
	require.NotNil(t, config.CreateIssues)
	require.NotNil(t, config.CreateIssues.Footer)
	assert.Equal(t, "false", *config.CreateIssues.Footer)
}

func TestBodyFooterConfiguration(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"name": "Test",
		"safe-outputs": map[string]any{
			"create-issue": map[string]any{
				"body-footer": "Issue footer from {workflow_name}",
			},
			"create-pull-request": map[string]any{
				"body-footer": "Pull request footer from {workflow_name}",
			},
		},
	}

	config := compiler.extractSafeOutputsConfig(frontmatter)
	require.NotNil(t, config)
	require.NotNil(t, config.CreateIssues)
	require.NotNil(t, config.CreatePullRequests)
	assert.Equal(t, "Issue footer from {workflow_name}", config.CreateIssues.BodyFooter)
	assert.Equal(t, "Pull request footer from {workflow_name}", config.CreatePullRequests.BodyFooter)

	issueConfig := issueHandlerRegistry["create_issue"](config)
	assert.Equal(t, "Issue footer from {workflow_name}", issueConfig["body_footer"])

	pullRequestConfig := pullRequestHandlerRegistry["create_pull_request"](config)
	assert.Equal(t, "Pull request footer from {workflow_name}", pullRequestConfig["body_footer"])
}

func TestAddCommentFooterConfiguration(t *testing.T) {
	t.Run("footer: false on add-comment", func(t *testing.T) {
		compiler := NewCompiler()
		frontmatter := map[string]any{
			"name": "Test",
			"safe-outputs": map[string]any{
				"add-comment": map[string]any{"footer": false},
			},
		}
		config := compiler.extractSafeOutputsConfig(frontmatter)
		require.NotNil(t, config)
		require.NotNil(t, config.AddComments)
		require.NotNil(t, config.AddComments.Footer)
		assert.Equal(t, "false", *config.AddComments.Footer, "add-comment footer should be false")
	})

	t.Run("global footer: false propagates to add-comment", func(t *testing.T) {
		compiler := NewCompiler()
		frontmatter := map[string]any{
			"name": "Test",
			"safe-outputs": map[string]any{
				"footer":      false,
				"add-comment": map[string]any{},
			},
		}
		config := compiler.extractSafeOutputsConfig(frontmatter)
		require.NotNil(t, config)
		require.NotNil(t, config.Footer)
		assert.False(t, *config.Footer, "Global footer should be false")

		workflowData := &WorkflowData{
			Name:        "Test",
			SafeOutputs: config,
		}
		var steps []string
		compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)

		for _, step := range steps {
			if strings.Contains(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
				parts := strings.Split(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
				if len(parts) == 2 {
					jsonStr := strings.TrimSpace(parts[1])
					jsonStr = strings.Trim(jsonStr, "\"")
					jsonStr = strings.ReplaceAll(jsonStr, "\\\"", "\"")
					var handlerConfig map[string]any
					err := json.Unmarshal([]byte(jsonStr), &handlerConfig)
					require.NoError(t, err)

					addCommentConfig, ok := handlerConfig["add_comment"].(map[string]any)
					require.True(t, ok, "add_comment handler config should exist")
					assert.Equal(t, false, addCommentConfig["footer"], "add_comment should inherit global footer: false")
				}
			}
		}
	})
}

func TestGlobalFooterConfiguration(t *testing.T) {
	t.Run("global footer: false applies to all handlers", func(t *testing.T) {
		compiler := NewCompiler()
		frontmatter := map[string]any{
			"name": "Test",
			"safe-outputs": map[string]any{
				"footer":              false, // Global footer control
				"add-comment":         map[string]any{},
				"create-issue":        map[string]any{"title-prefix": "[test] "},
				"create-pull-request": nil,
				"create-discussion":   nil,
				"update-issue":        map[string]any{"body": nil},
				"update-discussion":   map[string]any{"body": nil},
				"update-release":      nil,
				"update-pull-request": map[string]any{"body": nil},
			},
		}
		config := compiler.extractSafeOutputsConfig(frontmatter)
		require.NotNil(t, config)
		require.NotNil(t, config.Footer)
		assert.False(t, *config.Footer)

		// Verify global footer is propagated to handlers
		workflowData := &WorkflowData{
			Name:        "Test",
			SafeOutputs: config,
		}
		var steps []string
		compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)
		stepsContent := strings.Join(steps, "")
		require.Contains(t, stepsContent, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG")

		for _, step := range steps {
			if strings.Contains(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
				parts := strings.Split(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
				if len(parts) == 2 {
					jsonStr := strings.TrimSpace(parts[1])
					jsonStr = strings.Trim(jsonStr, "\"")
					jsonStr = strings.ReplaceAll(jsonStr, "\\\"", "\"")
					var handlerConfig map[string]any
					err := json.Unmarshal([]byte(jsonStr), &handlerConfig)
					require.NoError(t, err)

					// All handlers should have footer: false from global setting
					addCommentConfig, ok := handlerConfig["add_comment"].(map[string]any)
					require.True(t, ok, "add_comment handler config should exist in global footer test")
					assert.Equal(t, false, addCommentConfig["footer"], "add_comment should inherit global footer: false")
					if issueConfig, ok := handlerConfig["create_issue"].(map[string]any); ok {
						assert.Equal(t, false, issueConfig["footer"], "create_issue should inherit global footer: false")
					}
					if prConfig, ok := handlerConfig["create_pull_request"].(map[string]any); ok {
						assert.Equal(t, false, prConfig["footer"], "create_pull_request should inherit global footer: false")
					}
					if discussionConfig, ok := handlerConfig["create_discussion"].(map[string]any); ok {
						assert.Equal(t, false, discussionConfig["footer"], "create_discussion should inherit global footer: false")
					}
					if updateIssueConfig, ok := handlerConfig["update_issue"].(map[string]any); ok {
						assert.Equal(t, false, updateIssueConfig["footer"], "update_issue should inherit global footer: false")
					}
					if updateDiscussionConfig, ok := handlerConfig["update_discussion"].(map[string]any); ok {
						assert.Equal(t, false, updateDiscussionConfig["footer"], "update_discussion should inherit global footer: false")
					}
					if updateReleaseConfig, ok := handlerConfig["update_release"].(map[string]any); ok {
						assert.Equal(t, false, updateReleaseConfig["footer"], "update_release should inherit global footer: false")
					}
					if updatePRConfig, ok := handlerConfig["update_pull_request"].(map[string]any); ok {
						assert.Equal(t, false, updatePRConfig["footer"], "update_pull_request should inherit global footer: false")
					}
				}
			}
		}
	})

	t.Run("local footer overrides global footer", func(t *testing.T) {
		compiler := NewCompiler()
		frontmatter := map[string]any{
			"name": "Test",
			"safe-outputs": map[string]any{
				"footer":              false, // Global: hide footer
				"create-issue":        map[string]any{"title-prefix": "[test] "},
				"create-pull-request": map[string]any{"footer": true}, // Local: show footer
			},
		}
		config := compiler.extractSafeOutputsConfig(frontmatter)
		require.NotNil(t, config)
		require.NotNil(t, config.Footer)
		assert.False(t, *config.Footer, "Global footer should be false")
		require.NotNil(t, config.CreatePullRequests.Footer)
		assert.Equal(t, "true", *config.CreatePullRequests.Footer, "Local PR footer should override to true")

		// Verify in handler config
		workflowData := &WorkflowData{
			Name:        "Test",
			SafeOutputs: config,
		}
		var steps []string
		compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)

		for _, step := range steps {
			if strings.Contains(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
				parts := strings.Split(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
				if len(parts) == 2 {
					jsonStr := strings.TrimSpace(parts[1])
					jsonStr = strings.Trim(jsonStr, "\"")
					jsonStr = strings.ReplaceAll(jsonStr, "\\\"", "\"")
					var handlerConfig map[string]any
					err := json.Unmarshal([]byte(jsonStr), &handlerConfig)
					require.NoError(t, err)

					issueConfig, ok := handlerConfig["create_issue"].(map[string]any)
					require.True(t, ok)
					assert.Equal(t, false, issueConfig["footer"], "create_issue should use global footer: false")

					prConfig, ok := handlerConfig["create_pull_request"].(map[string]any)
					require.True(t, ok)
					assert.Equal(t, true, prConfig["footer"], "create_pull_request should override to footer: true")
				}
			}
		}
	})
}

func TestFooterInHandlerConfig(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{
		Name: "Test",
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					Max:    strPtr("1"),
					Footer: strPtr("false"),
				},
			},
		},
	}
	var steps []string
	compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)
	stepsContent := strings.Join(steps, "")
	require.Contains(t, stepsContent, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG")
	for _, step := range steps {
		if strings.Contains(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
			parts := strings.Split(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
			if len(parts) == 2 {
				jsonStr := strings.TrimSpace(parts[1])
				jsonStr = strings.Trim(jsonStr, "\"")
				jsonStr = strings.ReplaceAll(jsonStr, "\\\"", "\"")
				var config map[string]any
				err := json.Unmarshal([]byte(jsonStr), &config)
				require.NoError(t, err)
				issueConfig, ok := config["create_issue"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, false, issueConfig["footer"])
			}
		}
	}
}
