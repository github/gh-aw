//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestMainJobEnvironmentVariables(t *testing.T) {
	tests := []struct {
		name            string
		frontmatter     map[string]any
		expectedEnvVars []string
		shouldHaveEnv   bool
	}{
		{
			name: "No safe outputs - no env section",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"on":   "push",
			},
			shouldHaveEnv: false,
		},
		{
			name: "Safe outputs with create-issue",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"on":   "push",
				"safe-outputs": map[string]any{
					"create-issue": nil,
				},
			},
			expectedEnvVars: []string{
				// Config is now in file, not env var
			},
			shouldHaveEnv: true,
		},
		{
			name: "Safe outputs with custom env vars",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"on":   "push",
				"safe-outputs": map[string]any{
					"create-issue": nil,
					"env": map[string]any{
						"GITHUB_TOKEN": "${{ secrets.CUSTOM_PAT }}",
						"DEBUG_MODE":   "true",
					},
				},
			},
			expectedEnvVars: []string{
				// Config is now in file, not env var
			},
			shouldHaveEnv: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create workflow data
			compiler := NewCompiler()
			data := &WorkflowData{
				AI:          "claude",
				RunsOn:      "ubuntu-latest",
				Permissions: "contents: read",
			}

			// Parse safe-outputs configuration
			data.SafeOutputs = compiler.extractSafeOutputsConfig(tt.frontmatter)

			// Build the main job
			job, err := compiler.buildMainJob(data, false)
			if err != nil {
				t.Fatalf("Failed to build main job: %v", err)
			}

			// Check if env section should exist
			if !tt.shouldHaveEnv {
				if len(job.Env) > 0 {
					t.Errorf("Expected no environment variables, but got: %v", job.Env)
				}
				return
			}

			if len(job.Env) == 0 {
				t.Fatal("Expected environment variables to be present")
			}

			// Create job manager and render to YAML to test the output
			jobManager := NewJobManager()
			if err := jobManager.AddJob(job); err != nil {
				t.Fatalf("Failed to add job to manager: %v", err)
			}

			yamlOutput := jobManager.RenderToYAML()
			t.Logf("Generated YAML:\n%s", yamlOutput)

			// Check that env section exists in YAML
			if !strings.Contains(yamlOutput, "    env:\n") {
				t.Error("Expected 'env:' section in job YAML")
			}

			// Check each expected environment variable
			for _, expectedEnvVar := range tt.expectedEnvVars {
				if !strings.Contains(yamlOutput, "      "+expectedEnvVar) {
					t.Errorf("Expected environment variable %q not found in YAML output", expectedEnvVar)
				}
			}
		})
	}
}

func TestMainJobEnvironmentVariablesIntegration(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test workflow file with safe outputs and custom env vars
	workflowContent := `---
name: Test Job Environment Variables
on: push
safe-outputs:
  create-issue:
    title-prefix: "[test] "
    labels: ["automated"]
  env:
    GITHUB_TOKEN: ${{ secrets.CUSTOM_PAT }}
    DEBUG_MODE: "true"
    API_ENDPOINT: "https://api.example.com"
---

# Job Environment Variables Test

This workflow tests that job-level environment variables are properly set for safe outputs.
`

	workflowFile := filepath.Join(tmpDir, "test-job-env.md")
	if err := os.WriteFile(workflowFile, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	err := compiler.CompileWorkflow(workflowFile)
	if err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(workflowFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)
	t.Logf("Generated lock file content:\n%s", lockContentStr)

	// Check that the agent job has an env section
	if !strings.Contains(lockContentStr, "  agent:\n") {
		t.Fatal("Expected 'agent' job to be present")
	}

	// Check that the env section exists at job level
	agentJobStart := strings.Index(lockContentStr, "  agent:\n")
	if agentJobStart == -1 {
		t.Fatal("Could not find agent job")
	}

	// Find the next job (to limit our search scope)
	nextJobStart := len(lockContentStr) // Default to end of file
	lines := strings.Split(lockContentStr[agentJobStart:], "\n")
	for _, line := range lines[1:] { // Skip the "agent:" line
		if strings.HasPrefix(line, "  ") && strings.HasSuffix(line, ":") && !strings.HasPrefix(line, "    ") {
			nextJobStart = agentJobStart + strings.Index(lockContentStr[agentJobStart:], line)
			break
		}
	}

	agentJobSection := lockContentStr[agentJobStart:nextJobStart]
	t.Logf("Agent job section:\n%s", agentJobSection)

	// Verify env section exists at job level (not in steps)
	if !strings.Contains(agentJobSection, "    env:\n") {
		t.Error("Expected job-level 'env:' section in agent job")
	}

	// Check that GH_AW_SAFE_OUTPUTS_CONFIG is NOT in environment variables
	if strings.Contains(agentJobSection, "GH_AW_SAFE_OUTPUTS_CONFIG:") {
		t.Error("GH_AW_SAFE_OUTPUTS_CONFIG should NOT be in environment variables - config is now in file")
	}

	// Clean up
	os.Remove(lockFile)
}
