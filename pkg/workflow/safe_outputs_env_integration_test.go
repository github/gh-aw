//go:build integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
)

// parseWorkflowFromContent is a helper function to parse workflow content for testing
func parseWorkflowFromContent(t *testing.T, content string, filename string) *WorkflowData {
	t.Helper()

	result, err := parser.ExtractFrontmatterFromContent(content)
	if err != nil {
		t.Fatalf("Failed to extract frontmatter: %v", err)
	}

	compiler := NewCompiler()
	safeOutputs := compiler.extractSafeOutputsConfig(result.Frontmatter)
	topTools := extractToolsFromFrontmatter(result.Frontmatter)

	workflowData := &WorkflowData{
		Name:            filename,
		FrontmatterName: extractStringFromMap(result.Frontmatter, "name", nil),
		SafeOutputs:     safeOutputs,
		Tools:           topTools,
	}

	return workflowData
}

func TestSafeOutputsEnvIntegration(t *testing.T) {
	tests := []struct {
		name               string
		frontmatter        map[string]any
		expectedEnvVars    []string
		expectedSafeOutput string
	}{
		{
			name: "Create issue job with custom env vars",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"on":   "push",
				"safe-outputs": map[string]any{
					"create-issue": nil,
					"env": map[string]any{
						"GITHUB_TOKEN": "${{ secrets.SOME_PAT_FOR_AGENTIC_WORKFLOWS }}",
						"DEBUG_MODE":   "true",
					},
				},
			},
			expectedEnvVars: []string{
				"GITHUB_TOKEN: ${{ secrets.SOME_PAT_FOR_AGENTIC_WORKFLOWS }}",
				"DEBUG_MODE: true",
			},
			expectedSafeOutput: "create-issue",
		},
		{
			name: "Create pull request job with custom env vars",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"on":   "push",
				"safe-outputs": map[string]any{
					"create-pull-request": nil,
					"env": map[string]any{
						"CUSTOM_API_KEY": "${{ secrets.CUSTOM_API_KEY }}",
						"ENVIRONMENT":    "production",
					},
				},
			},
			expectedEnvVars: []string{
				"CUSTOM_API_KEY: ${{ secrets.CUSTOM_API_KEY }}",
				"ENVIRONMENT: production",
			},
			expectedSafeOutput: "create-pull-request",
		},
		{
			name: "Add issue comment job with custom env vars",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"on":   "issues",
				"safe-outputs": map[string]any{
					"add-comment": nil,
					"env": map[string]any{
						"NOTIFICATION_URL": "${{ secrets.WEBHOOK_URL }}",
						"COMMENT_TEMPLATE": "template-v2",
					},
				},
			},
			expectedEnvVars: []string{
				"NOTIFICATION_URL: ${{ secrets.WEBHOOK_URL }}",
				"COMMENT_TEMPLATE: template-v2",
			},
			expectedSafeOutput: "add-comment",
		},
		{
			name: "Multiple safe outputs with shared env vars",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"on":   "push",
				"safe-outputs": map[string]any{
					"create-issue":        nil,
					"create-pull-request": nil,
					"env": map[string]any{
						"SHARED_TOKEN": "${{ secrets.SHARED_TOKEN }}",
						"WORKFLOW_ID":  "multi-output-test",
					},
				},
			},
			expectedEnvVars: []string{
				"SHARED_TOKEN: ${{ secrets.SHARED_TOKEN }}",
				"WORKFLOW_ID: multi-output-test",
			},
			expectedSafeOutput: "create-issue,create-pull-request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()

			// Extract the safe outputs configuration
			config := compiler.extractSafeOutputsConfig(tt.frontmatter)
			if config == nil {
				t.Fatal("Expected SafeOutputsConfig to be parsed")
			}

			// Verify env configuration is parsed correctly
			if config.Env == nil {
				t.Fatal("Expected Env to be parsed")
			}

			// Build workflow data
			data := &WorkflowData{
				Name:            "Test",
				FrontmatterName: "Test Workflow",
				SafeOutputs:     config,
			}

			// Test job generation for each safe output type
			if strings.Contains(tt.expectedSafeOutput, "create-issue") {
				job, err := compiler.buildCreateOutputIssueJob(data, "main_job")
				if err != nil {
					t.Errorf("Error building create issue job: %v", err)
				}

				assertEnvVarsInSteps(t, job.Steps, tt.expectedEnvVars)
			}

			if strings.Contains(tt.expectedSafeOutput, "create-pull-request") {
				job, err := compiler.buildCreateOutputPullRequestJob(data, "main_job")
				if err != nil {
					t.Errorf("Error building create pull request job: %v", err)
				}

				assertEnvVarsInSteps(t, job.Steps, tt.expectedEnvVars)
			}

			if strings.Contains(tt.expectedSafeOutput, "add-comment") {
				job, err := compiler.buildCreateOutputAddCommentJob(data, "main_job", "", "", "")
				if err != nil {
					t.Errorf("Error building add issue comment job: %v", err)
				}

				assertEnvVarsInSteps(t, job.Steps, tt.expectedEnvVars)
			}
		})
	}
}

func TestSafeOutputsEnvFullWorkflowCompilation(t *testing.T) {
	workflowContent := `---
name: Test Environment Variables
on: push
safe-outputs:
  create-issue:
    title-prefix: "[env-test] "
    labels: ["automated", "env-test"]
  env:
    GITHUB_TOKEN: ${{ secrets.SOME_PAT_FOR_AGENTIC_WORKFLOWS }}
    DEBUG_MODE: "true"
    CUSTOM_API_KEY: ${{ secrets.CUSTOM_API_KEY }}
---

# Environment Variables Test Workflow

This workflow tests that custom environment variables are properly passed through
to safe output jobs.

Create an issue with test results.
`

	workflowData := parseWorkflowFromContent(t, workflowContent, "test-env-workflow.md")

	// Verify the SafeOutputs configuration includes our environment variables
	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected SafeOutputs to be parsed")
	}

	if workflowData.SafeOutputs.Env == nil {
		t.Fatal("Expected Env to be parsed")
	}

	expectedEnvVars := map[string]string{
		"GITHUB_TOKEN":   "${{ secrets.SOME_PAT_FOR_AGENTIC_WORKFLOWS }}",
		"DEBUG_MODE":     "true",
		"CUSTOM_API_KEY": "${{ secrets.CUSTOM_API_KEY }}",
	}

	for key, expectedValue := range expectedEnvVars {
		if actualValue, exists := workflowData.SafeOutputs.Env[key]; !exists {
			t.Errorf("Expected env key %s to exist", key)
		} else if actualValue != expectedValue {
			t.Errorf("Expected env[%s] to be %q, got %q", key, expectedValue, actualValue)
		}
	}

	// Build the create issue job and verify it includes our environment variables
	compiler := NewCompiler()
	job, err := compiler.buildCreateOutputIssueJob(workflowData, "main_job")
	if err != nil {
		t.Fatalf("Failed to build create issue job: %v", err)
	}

	jobYAML := strings.Join(job.Steps, "")

	expectedEnvLines := []string{
		"GITHUB_TOKEN: ${{ secrets.SOME_PAT_FOR_AGENTIC_WORKFLOWS }}",
		"DEBUG_MODE: true",
		"CUSTOM_API_KEY: ${{ secrets.CUSTOM_API_KEY }}",
	}

	for _, expectedEnvLine := range expectedEnvLines {
		if !strings.Contains(jobYAML, expectedEnvLine) {
			t.Errorf("Expected environment variable %q not found in job YAML", expectedEnvLine)
		}
	}

	// Verify issue configuration is present
	if !strings.Contains(jobYAML, "GH_AW_ISSUE_TITLE_PREFIX: \"[env-test] \"") {
		t.Error("Expected issue title prefix not found in job YAML")
	}

	if !strings.Contains(jobYAML, "GH_AW_ISSUE_LABELS: \"automated,env-test\"") {
		t.Error("Expected issue labels not found in job YAML")
	}
}

func TestSafeOutputsEnvWithStagedMode(t *testing.T) {
	workflowContent := `---
name: Test Environment Variables with Staged Mode
on: push
safe-outputs:
  create-issue:
  env:
    GITHUB_TOKEN: ${{ secrets.SOME_PAT_FOR_AGENTIC_WORKFLOWS }}
    DEBUG_MODE: "true"
  staged: true
---

# Environment Variables with Staged Mode Test

This workflow tests that custom environment variables work with staged mode.
`

	workflowData := parseWorkflowFromContent(t, workflowContent, "test-env-staged-workflow.md")

	// Verify staged mode is enabled
	if !workflowData.SafeOutputs.Staged {
		t.Error("Expected staged mode to be enabled")
	}

	// Build the create issue job and verify it includes our environment variables and staged flag
	compiler := NewCompiler()
	job, err := compiler.buildCreateOutputIssueJob(workflowData, "main_job")
	if err != nil {
		t.Fatalf("Failed to build create issue job: %v", err)
	}

	jobYAML := strings.Join(job.Steps, "")

	expectedEnvVars := []string{
		"GITHUB_TOKEN: ${{ secrets.SOME_PAT_FOR_AGENTIC_WORKFLOWS }}",
		"DEBUG_MODE: true",
	}

	assertEnvVarsInSteps(t, job.Steps, expectedEnvVars)

	// Verify staged mode is enabled
	if !strings.Contains(jobYAML, "GH_AW_SAFE_OUTPUTS_STAGED: \"true\"") {
		t.Error("Expected staged mode flag not found in job YAML")
	}
}
