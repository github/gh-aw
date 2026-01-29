//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestCreateProjectGitHubTokenEnvVar verifies that GH_AW_PROJECT_GITHUB_TOKEN
// is exposed as an environment variable for all create_project steps
func TestCreateProjectGitHubTokenEnvVar(t *testing.T) {
	tests := []struct {
		name                string
		frontmatter         map[string]any
		expectedEnvVarValue string
		expectedTokenValue  string
	}{
		{
			name: "create-project with custom github-token",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"safe-outputs": map[string]any{
					"create-project": map[string]any{
						"github-token": "${{ secrets.PROJECTS_PAT }}",
					},
				},
			},
			expectedEnvVarValue: "GH_AW_PROJECT_GITHUB_TOKEN: ${{ secrets.PROJECTS_PAT }}",
			expectedTokenValue:  "github-token: ${{ secrets.PROJECTS_PAT }}",
		},
		{
			name: "create-project without custom github-token (uses GH_AW_PROJECT_GITHUB_TOKEN)",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"safe-outputs": map[string]any{
					"create-project": nil,
				},
			},
			expectedEnvVarValue: "GH_AW_PROJECT_GITHUB_TOKEN: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}",
			expectedTokenValue:  "github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}",
		},
		{
			name: "create-project with top-level github-token",
			frontmatter: map[string]any{
				"name":         "Test Workflow",
				"github-token": "${{ secrets.CUSTOM_TOKEN }}",
				"safe-outputs": map[string]any{
					"create-project": nil,
				},
			},
			expectedEnvVarValue: "GH_AW_PROJECT_GITHUB_TOKEN: ${{ secrets.CUSTOM_TOKEN }}",
			expectedTokenValue:  "github-token: ${{ secrets.CUSTOM_TOKEN }}",
		},
		{
			name: "create-project with per-config token overrides top-level",
			frontmatter: map[string]any{
				"name":         "Test Workflow",
				"github-token": "${{ secrets.GLOBAL_TOKEN }}",
				"safe-outputs": map[string]any{
					"create-project": map[string]any{
						"github-token": "${{ secrets.PROJECT_SPECIFIC_TOKEN }}",
					},
				},
			},
			expectedEnvVarValue: "GH_AW_PROJECT_GITHUB_TOKEN: ${{ secrets.PROJECT_SPECIFIC_TOKEN }}",
			expectedTokenValue:  "github-token: ${{ secrets.PROJECT_SPECIFIC_TOKEN }}",
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

			// Build the create_project step config
			stepConfig := compiler.buildCreateProjectStepConfig(workflowData, "main", false)

			// Convert step config to string for checking
			yamlStr := strings.Join(stepConfig.CustomEnvVars, "")

			// Check that the environment variable is present with the expected value
			if !strings.Contains(yamlStr, tt.expectedEnvVarValue) {
				t.Errorf("Expected environment variable %q to be set in create_project step, but it was not found.\nGenerated YAML:\n%s",
					tt.expectedEnvVarValue, yamlStr)
			}

			// Also verify the token field matches the expected value
			if stepConfig.Token != strings.TrimPrefix(tt.expectedTokenValue, "github-token: ") {
				t.Errorf("Expected token to be %q, but got %q",
					strings.TrimPrefix(tt.expectedTokenValue, "github-token: "), stepConfig.Token)
			}
		})
	}
}
