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
