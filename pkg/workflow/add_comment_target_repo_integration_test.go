//go:build integration

package workflow

import (
	"os"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestAddCommentTargetRepoIntegration(t *testing.T) {
	tests := []struct {
		name                     string
		frontmatter              map[string]any
		expectedTargetRepoInYAML string
		shouldHaveTargetRepo     bool
		trialLogicalRepoSlug     string
		expectedTargetRepoValue  string
	}{
		{
			name: "target-repo configuration should be in handler config",
			frontmatter: map[string]any{
				"name":   "Test Workflow",
				"engine": "copilot",
				"safe-outputs": map[string]any{
					"add-comment": map[string]any{
						"max":         5,
						"target":      "*",
						"target-repo": "github/customer-feedback",
					},
				},
			},
			shouldHaveTargetRepo:    true,
			expectedTargetRepoValue: "github/customer-feedback",
		},
		{
			name: "target-repo should take precedence over trial target repo",
			frontmatter: map[string]any{
				"name":   "Test Workflow",
				"engine": "copilot",
				"safe-outputs": map[string]any{
					"add-comment": map[string]any{
						"max":         5,
						"target":      "*",
						"target-repo": "github/customer-feedback",
					},
				},
			},
			trialLogicalRepoSlug:    "trial/repo",
			shouldHaveTargetRepo:    true,
			expectedTargetRepoValue: "github/customer-feedback", // Should prefer config over trial
		},
		{
			name: "no target-repo should fall back to trial target repo (via env var)",
			frontmatter: map[string]any{
				"name":   "Test Workflow",
				"engine": "copilot",
				"safe-outputs": map[string]any{
					"add-comment": map[string]any{
						"max":    5,
						"target": "*",
					},
				},
			},
			trialLogicalRepoSlug:    "trial/repo",
			shouldHaveTargetRepo:    false, // Trial mode sets env var, not config
			expectedTargetRepoValue: "",    // Not checked
		},
		{
			name: "no target-repo and no trial should not have target-repo in handler config",
			frontmatter: map[string]any{
				"name":   "Test Workflow",
				"engine": "copilot",
				"safe-outputs": map[string]any{
					"add-comment": map[string]any{
						"max":    5,
						"target": "*",
					},
				},
			},
			trialLogicalRepoSlug: "", // explicitly empty
			shouldHaveTargetRepo: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for this test
			tempDir := testutil.TempDir(t, "test-*")
			workflowPath := tempDir + "/test-workflow.md"

			// Create a simple workflow content
			workflowContent := "# Test Workflow\n\nThis is a test workflow for target-repo functionality."

			// Write the workflow file
			err := os.WriteFile(workflowPath, []byte(workflowContent), 0644)
			if err != nil {
				t.Fatalf("Failed to write workflow file: %v", err)
			}

			// Create compiler with trial mode if needed
			compiler := NewCompiler()
			if tt.trialLogicalRepoSlug != "" {
				compiler.SetTrialMode(true)
				compiler.SetTrialLogicalRepoSlug(tt.trialLogicalRepoSlug)
			}

			// Parse workflow data
			workflowData := &WorkflowData{
				Name: tt.frontmatter["name"].(string),
			}

			// Extract safe outputs configuration
			workflowData.SafeOutputs = compiler.extractSafeOutputsConfig(tt.frontmatter)

			if workflowData.SafeOutputs == nil || workflowData.SafeOutputs.AddComments == nil {
				t.Fatal("Expected AddComments configuration to be parsed")
			}

			// Build the consolidated safe outputs job (handler manager)
			job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, "main", "")
			if err != nil {
				t.Fatalf("Failed to build consolidated safe outputs job: %v", err)
			}

			// Convert steps to string to check for target-repo in handler config
			jobYAML := strings.Join(job.Steps, "")

			if tt.shouldHaveTargetRepo {
				// Check that target-repo is in the handler config JSON
				// Format: GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: "{...\"target-repo\":\"value\"...}"
				expectedConfigField := `\"target-repo\":\"` + tt.expectedTargetRepoValue + `\"`
				if !strings.Contains(jobYAML, expectedConfigField) {
					t.Errorf("Expected to find target-repo in GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG JSON: %s\nActual job YAML:\n%s", expectedConfigField, jobYAML)
				}
			} else {
				// Check that target-repo is not in the handler config JSON
				if strings.Contains(jobYAML, `\"target-repo\"`) {
					t.Errorf("Expected not to find target-repo in GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG JSON when no target-repo is configured.\nActual job YAML:\n%s", jobYAML)
				}
			}
		})
	}
}
