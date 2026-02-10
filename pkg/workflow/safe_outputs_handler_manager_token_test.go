//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestHandlerManagerProjectGitHubTokenEnvVar verifies that GH_AW_PROJECT_GITHUB_TOKEN
// is exposed as an environment variable in the consolidated safe outputs handler step
// when any project-related safe output is configured
func TestHandlerManagerProjectGitHubTokenEnvVar(t *testing.T) {
	tests := []struct {
		name                string
		frontmatter         map[string]any
		expectedEnvVarValue string
		expectedWithToken   string
		shouldHaveToken     bool
	}{
		{
			name: "update-project with custom github-token",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"safe-outputs": map[string]any{
					"update-project": map[string]any{
						"github-token": "${{ secrets.PROJECTS_PAT }}",
						"project":      "https://github.com/orgs/myorg/projects/1",
					},
				},
			},
			expectedEnvVarValue: "GH_AW_PROJECT_GITHUB_TOKEN: ${{ secrets.PROJECTS_PAT }}",
			expectedWithToken:   "github-token: ${{ secrets.PROJECTS_PAT }}",
			shouldHaveToken:     true,
		},
		{
			name: "update-project without custom github-token (uses GH_AW_PROJECT_GITHUB_TOKEN)",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"safe-outputs": map[string]any{
					"update-project": map[string]any{
						"project": "https://github.com/orgs/myorg/projects/1",
					},
				},
			},
			expectedEnvVarValue: "GH_AW_PROJECT_GITHUB_TOKEN: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}",
			expectedWithToken:   "github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}",
			shouldHaveToken:     true,
		},
		{
			name: "update-project with top-level github-token",
			frontmatter: map[string]any{
				"name":         "Test Workflow",
				"github-token": "${{ secrets.CUSTOM_TOKEN }}",
				"safe-outputs": map[string]any{
					"update-project": map[string]any{
						"project": "https://github.com/orgs/myorg/projects/1",
					},
				},
			},
			expectedEnvVarValue: "GH_AW_PROJECT_GITHUB_TOKEN: ${{ secrets.CUSTOM_TOKEN }}",
			expectedWithToken:   "github-token: ${{ secrets.CUSTOM_TOKEN }}",
			shouldHaveToken:     true,
		},
		{
			name: "create-project-status-update with custom github-token",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"safe-outputs": map[string]any{
					"create-project-status-update": map[string]any{
						"github-token": "${{ secrets.STATUS_PAT }}",
						"project":      "https://github.com/orgs/myorg/projects/2",
					},
				},
			},
			expectedEnvVarValue: "GH_AW_PROJECT_GITHUB_TOKEN: ${{ secrets.STATUS_PAT }}",
			expectedWithToken:   "github-token: ${{ secrets.STATUS_PAT }}",
			shouldHaveToken:     true,
		},
		{
			name: "create-project with custom github-token (no project URL)",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"safe-outputs": map[string]any{
					"create-project": map[string]any{
						"github-token": "${{ secrets.CREATE_PAT }}",
					},
				},
			},
			expectedEnvVarValue: "GH_AW_PROJECT_GITHUB_TOKEN: ${{ secrets.CREATE_PAT }}",
			expectedWithToken:   "github-token: ${{ secrets.CREATE_PAT }}",
			shouldHaveToken:     true,
		},
		{
			name: "multiple project configs - update-project takes precedence",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"safe-outputs": map[string]any{
					"update-project": map[string]any{
						"github-token": "${{ secrets.UPDATE_PAT }}",
						"project":      "https://github.com/orgs/myorg/projects/1",
					},
					"create-project-status-update": map[string]any{
						"github-token": "${{ secrets.STATUS_PAT }}",
						"project":      "https://github.com/orgs/myorg/projects/2",
					},
					"create-project": map[string]any{
						"github-token": "${{ secrets.CREATE_PAT }}",
					},
				},
			},
			expectedEnvVarValue: "GH_AW_PROJECT_GITHUB_TOKEN: ${{ secrets.UPDATE_PAT }}",
			expectedWithToken:   "github-token: ${{ secrets.UPDATE_PAT }}",
			shouldHaveToken:     true,
		},
		{
			name: "no project configs - no token set",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"safe-outputs": map[string]any{
					"add-comment": map[string]any{
						"max": 5,
					},
				},
			},
			shouldHaveToken: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()

			// Parse frontmatter
			workflowData := &WorkflowData{
				Name:        "test-workflow",
				SafeOutputs: compiler.extractSafeOutputsConfig(tt.frontmatter),
			}

			// Set top-level github-token if present in frontmatter
			if githubToken, ok := tt.frontmatter["github-token"].(string); ok {
				workflowData.GitHubToken = githubToken
			}

			// Build the handler manager step
			steps := compiler.buildHandlerManagerStep(workflowData)
			yamlStr := strings.Join(steps, "")

			if tt.shouldHaveToken {
				// Check that the environment variable is present with the expected value
				assert.Contains(t, yamlStr, tt.expectedEnvVarValue,
					"Expected environment variable %q to be set in handler manager step",
					tt.expectedEnvVarValue)

				// Check that the github-script token matches the effective project token
				assert.Contains(t, yamlStr, tt.expectedWithToken,
					"Expected github-script token %q to be set in handler manager step",
					tt.expectedWithToken)
			} else {
				// Check that GH_AW_PROJECT_GITHUB_TOKEN is NOT set
				assert.NotContains(t, yamlStr, "GH_AW_PROJECT_GITHUB_TOKEN",
					"Expected GH_AW_PROJECT_GITHUB_TOKEN to NOT be set when no project configs are present")
			}
		})
	}
}

// TestHandlerManagerMultipleNonProjectTokens verifies that when multiple non-project handlers
// specify different github-token values, they are correctly included in the handler config JSON
func TestHandlerManagerMultipleNonProjectTokens(t *testing.T) {
	compiler := NewCompiler()

	// Parse frontmatter with create-issue and update-project having different tokens
	frontmatter := map[string]any{
		"name": "Test Multiple Tokens",
		"safe-outputs": map[string]any{
			"create-issue": map[string]any{
				"github-token": "${{ secrets.AGENT_GITHUB_TOKEN }}",
				"title-prefix": "[test] ",
				"max":          5,
			},
			"update-project": map[string]any{
				"github-token": "${{ secrets.PROJECT_GITHUB_TOKEN }}",
				"project":      "https://github.com/orgs/myorg/projects/1",
				"max":          10,
			},
		},
	}

	workflowData := &WorkflowData{
		Name:        "test-workflow",
		SafeOutputs: compiler.extractSafeOutputsConfig(frontmatter),
	}

	// Build the handler manager step
	steps := compiler.buildHandlerManagerStep(workflowData)
	yamlStr := strings.Join(steps, "")

	// The JSON is embedded in YAML with escaped quotes
	// Looking for: "{\"create_issue\":{\"github-token\":\"${{ secrets.AGENT_GITHUB_TOKEN }}\""
	assert.Contains(t, yamlStr, `create_issue`, "Expected create_issue handler in config")
	assert.Contains(t, yamlStr, `${{ secrets.AGENT_GITHUB_TOKEN }}`, "Expected AGENT_GITHUB_TOKEN in config")
	assert.Contains(t, yamlStr, `update_project`, "Expected update_project handler in config")
	assert.Contains(t, yamlStr, `${{ secrets.PROJECT_GITHUB_TOKEN }}`, "Expected PROJECT_GITHUB_TOKEN in config")

	// Verify that the project token is used for the github-script step (takes precedence)
	assert.Contains(t, yamlStr, "github-token: ${{ secrets.PROJECT_GITHUB_TOKEN }}",
		"Expected PROJECT_GITHUB_TOKEN as the github-script token")
}

// TestGitHubTokenPrecedenceAllLevels verifies token precedence across all configuration levels:
// handler-level > safe-outputs-level > top-level
func TestGitHubTokenPrecedenceAllLevels(t *testing.T) {
	tests := []struct {
		name                  string
		frontmatter           map[string]any
		expectedHandlerTokens map[string]string // handler name -> expected token in config
		expectedScriptToken   string            // expected token for github-script step
	}{
		{
			name: "safe-outputs level token used by handlers without handler-level token",
			frontmatter: map[string]any{
				"name": "Test Safe-Outputs Token",
				"safe-outputs": map[string]any{
					"github-token": "${{ secrets.SAFE_OUTPUTS_TOKEN }}",
					"create-issue": map[string]any{
						"title-prefix": "[test] ",
					},
					"add-comment": map[string]any{
						"max": 5,
					},
				},
			},
			expectedHandlerTokens: map[string]string{
				// Handlers should not have github-token since they inherit from safe-outputs level
				"create_issue": "",
				"add_comment":  "",
			},
			expectedScriptToken: "${{ secrets.SAFE_OUTPUTS_TOKEN }}",
		},
		{
			name: "handler-level token overrides safe-outputs level token",
			frontmatter: map[string]any{
				"name": "Test Handler Override",
				"safe-outputs": map[string]any{
					"github-token": "${{ secrets.SAFE_OUTPUTS_TOKEN }}",
					"create-issue": map[string]any{
						"github-token": "${{ secrets.ISSUE_TOKEN }}",
						"title-prefix": "[test] ",
					},
					"add-comment": map[string]any{
						"max": 5,
					},
				},
			},
			expectedHandlerTokens: map[string]string{
				"create_issue": "${{ secrets.ISSUE_TOKEN }}",
				"add_comment":  "", // Should not have token, inherits from safe-outputs level
			},
			expectedScriptToken: "${{ secrets.SAFE_OUTPUTS_TOKEN }}",
		},
		{
			name: "all three levels: handler > safe-outputs > top-level",
			frontmatter: map[string]any{
				"name":         "Test All Three Levels",
				"github-token": "${{ secrets.TOP_LEVEL_TOKEN }}",
				"safe-outputs": map[string]any{
					"github-token": "${{ secrets.SAFE_OUTPUTS_TOKEN }}",
					"create-issue": map[string]any{
						"github-token": "${{ secrets.ISSUE_TOKEN }}",
						"title-prefix": "[test] ",
					},
					"add-comment": map[string]any{
						"github-token": "${{ secrets.COMMENT_TOKEN }}",
						"max":          5,
					},
					"update-issue": map[string]any{
						"target": "issue",
					},
				},
			},
			expectedHandlerTokens: map[string]string{
				"create_issue": "${{ secrets.ISSUE_TOKEN }}",   // Has handler-level token
				"add_comment":  "${{ secrets.COMMENT_TOKEN }}", // Has handler-level token
				"update_issue": "",                             // No handler-level token, inherits safe-outputs level
			},
			expectedScriptToken: "${{ secrets.SAFE_OUTPUTS_TOKEN }}",
		},
		{
			name: "project handler with safe-outputs level token",
			frontmatter: map[string]any{
				"name": "Test Project with Safe-Outputs Token",
				"safe-outputs": map[string]any{
					"github-token": "${{ secrets.SAFE_OUTPUTS_TOKEN }}",
					"update-project": map[string]any{
						"github-token": "${{ secrets.PROJECT_TOKEN }}",
						"project":      "https://github.com/orgs/myorg/projects/1",
					},
					"create-issue": map[string]any{
						"title-prefix": "[test] ",
					},
				},
			},
			expectedHandlerTokens: map[string]string{
				"update_project": "${{ secrets.PROJECT_TOKEN }}",
				"create_issue":   "", // No handler-level token
			},
			expectedScriptToken: "${{ secrets.PROJECT_TOKEN }}", // Project token takes precedence for github-script
		},
		{
			name: "top-level token only (no safe-outputs or handler tokens)",
			frontmatter: map[string]any{
				"name":         "Test Top-Level Only",
				"github-token": "${{ secrets.TOP_LEVEL_TOKEN }}",
				"safe-outputs": map[string]any{
					"create-issue": map[string]any{
						"title-prefix": "[test] ",
					},
				},
			},
			expectedHandlerTokens: map[string]string{
				"create_issue": "", // No handler-level or safe-outputs token
			},
			expectedScriptToken: "${{ secrets.TOP_LEVEL_TOKEN }}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()

			// Parse frontmatter
			workflowData := &WorkflowData{
				Name:        "test-workflow",
				SafeOutputs: compiler.extractSafeOutputsConfig(tt.frontmatter),
			}

			// Set top-level github-token if present
			if githubToken, ok := tt.frontmatter["github-token"].(string); ok {
				workflowData.GitHubToken = githubToken
			}

			// Build the handler manager step
			steps := compiler.buildHandlerManagerStep(workflowData)
			yamlStr := strings.Join(steps, "")

			// Check expected github-script token
			assert.Contains(t, yamlStr, "github-token: "+tt.expectedScriptToken,
				"Expected github-script step to use token: %s", tt.expectedScriptToken)

			// Check each handler's token in the config JSON
			for handlerName, expectedToken := range tt.expectedHandlerTokens {
				if expectedToken != "" {
					assert.Contains(t, yamlStr, handlerName, "Expected handler %s in config", handlerName)
					assert.Contains(t, yamlStr, expectedToken, "Expected token %s for handler %s", expectedToken, handlerName)
				}
			}
		})
	}
}

// TestSafeOutputsLevelGitHubToken verifies that the safe-outputs level github-token
// is properly used as a default for handlers without their own token
func TestSafeOutputsLevelGitHubToken(t *testing.T) {
	compiler := NewCompiler()

	frontmatter := map[string]any{
		"name": "Test Safe-Outputs Level Token",
		"safe-outputs": map[string]any{
			"github-token": "${{ secrets.SAFE_OUTPUT_GITHUB_TOKEN }}",
			"create-issue": map[string]any{
				"title-prefix": "[dependabot-burner] ",
				"assignees":    []string{"copilot"},
				"max":          10,
			},
			"update-project": map[string]any{
				"github-token": "${{ secrets.PROJECT_GITHUB_TOKEN }}",
				"project":      "https://github.com/orgs/my-mona-org/projects/1",
				"max":          50,
			},
		},
	}

	workflowData := &WorkflowData{
		Name:        "test-workflow",
		SafeOutputs: compiler.extractSafeOutputsConfig(frontmatter),
	}

	// Build the handler manager step
	steps := compiler.buildHandlerManagerStep(workflowData)
	yamlStr := strings.Join(steps, "")

	// Verify that both tokens are preserved
	assert.Contains(t, yamlStr, `create_issue`, "Expected create_issue handler in config")
	assert.Contains(t, yamlStr, `update_project`, "Expected update_project handler in config")
	assert.Contains(t, yamlStr, `${{ secrets.PROJECT_GITHUB_TOKEN }}`, "Expected PROJECT_GITHUB_TOKEN in update_project config")

	// Verify that the project token is used for the github-script step (project token takes precedence)
	assert.Contains(t, yamlStr, "github-token: ${{ secrets.PROJECT_GITHUB_TOKEN }}",
		"Expected PROJECT_GITHUB_TOKEN as the github-script token (project token has priority)")
}
