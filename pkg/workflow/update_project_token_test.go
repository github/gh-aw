//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestUpdateProjectGitHubTokenEnvVar verifies that GH_AW_PROJECT_GITHUB_TOKEN
// is exposed as an environment variable for all update_project jobs
func TestUpdateProjectGitHubTokenEnvVar(t *testing.T) {
	tests := []struct {
		name                string
		frontmatter         map[string]any
		expectedEnvVarValue string
	}{
		{
			name: "update-project with custom github-token",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"safe-outputs": map[string]any{
					"update-project": map[string]any{
						"github-token": "${{ secrets.PROJECTS_PAT }}",
					},
				},
			},
			expectedEnvVarValue: "GH_AW_PROJECT_GITHUB_TOKEN: ${{ secrets.PROJECTS_PAT }}",
		},
		{
			name: "update-project without custom github-token (uses GH_AW_PROJECT_GITHUB_TOKEN)",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"safe-outputs": map[string]any{
					"update-project": nil,
				},
			},
			expectedEnvVarValue: "GH_AW_PROJECT_GITHUB_TOKEN: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}",
		},
		{
			name: "update-project with top-level github-token",
			frontmatter: map[string]any{
				"name":         "Test Workflow",
				"github-token": "${{ secrets.CUSTOM_TOKEN }}",
				"safe-outputs": map[string]any{
					"update-project": nil,
				},
			},
			expectedEnvVarValue: "GH_AW_PROJECT_GITHUB_TOKEN: ${{ secrets.CUSTOM_TOKEN }}",
		},
		{
			name: "update-project with per-config token overrides top-level",
			frontmatter: map[string]any{
				"name":         "Test Workflow",
				"github-token": "${{ secrets.GLOBAL_TOKEN }}",
				"safe-outputs": map[string]any{
					"update-project": map[string]any{
						"github-token": "${{ secrets.PROJECT_SPECIFIC_TOKEN }}",
					},
				},
			},
			expectedEnvVarValue: "GH_AW_PROJECT_GITHUB_TOKEN: ${{ secrets.PROJECT_SPECIFIC_TOKEN }}",
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

			// Build the update_project job
			job, err := compiler.buildUpdateProjectJob(workflowData, "main")
			if err != nil {
				t.Fatalf("Failed to build update_project job: %v", err)
			}

			// Convert job to YAML to check for environment variables
			yamlStr := strings.Join(job.Steps, "")

			// Check that the environment variable is present with the expected value
			if !strings.Contains(yamlStr, tt.expectedEnvVarValue) {
				t.Errorf("Expected environment variable %q to be set in update_project job, but it was not found.\nGenerated YAML:\n%s",
					tt.expectedEnvVarValue, yamlStr)
			}

			// Also verify the token is passed to github-token parameter
			expectedWith := "github-token: " + strings.TrimPrefix(tt.expectedEnvVarValue, "GH_AW_PROJECT_GITHUB_TOKEN: ")
			if !strings.Contains(yamlStr, expectedWith) {
				t.Errorf("Expected github-token parameter to be set to %q, but it was not found.\nGenerated YAML:\n%s",
					expectedWith, yamlStr)
			}
		})
	}
}
