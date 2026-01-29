//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestJobsSecretsValidation tests the validation of jobs.secrets field values
// Uses a representative sample of valid and invalid cases
func TestJobsSecretsValidation(t *testing.T) {
	tests := []struct {
		name        string
		markdown    string
		expectError bool
		errorMsg    string
	}{
		// Valid cases - representative sample
		{
			name: "valid single secret reference",
			markdown: `---
on: workflow_dispatch
engine: custom
jobs:
  deploy:
    uses: ./.github/workflows/deploy.yml
    secrets:
      token: ${{ secrets.GITHUB_TOKEN }}
---
Test workflow with valid single secret.`,
			expectError: false,
		},
		{
			name: "valid secret with fallback",
			markdown: `---
on: workflow_dispatch
engine: custom
jobs:
  deploy:
    uses: ./.github/workflows/deploy.yml
    secrets:
      token: ${{ secrets.CUSTOM_TOKEN || secrets.GITHUB_TOKEN }}
---
Test workflow with secret fallback.`,
			expectError: false,
		},
		// Invalid cases - representative sample
		{
			name: "invalid plaintext secret",
			markdown: `---
on: workflow_dispatch
engine: custom
jobs:
  deploy:
    uses: ./.github/workflows/deploy.yml
    secrets:
      token: my-plaintext-secret
---
Test workflow with plaintext secret.`,
			expectError: true,
			errorMsg:    "does not match pattern",
		},
		{
			name: "invalid env variable reference",
			markdown: `---
on: workflow_dispatch
engine: custom
jobs:
  deploy:
    uses: ./.github/workflows/deploy.yml
    secrets:
      token: ${{ env.MY_TOKEN }}
---
Test workflow with env variable.`,
			expectError: true,
			errorMsg:    "does not match pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test
			tempDir := t.TempDir()
			workflowPath := filepath.Join(tempDir, "test.md")

			// Write test workflow
			err := os.WriteFile(workflowPath, []byte(tt.markdown), 0o644)
			if err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			// Compile workflow
			compiler := NewCompiler()
			err = compiler.CompileWorkflow(workflowPath)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error containing %q, got no error", tt.errorMsg)
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing %q, got: %v", tt.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got: %v", err)
				}
			}
		})
	}
}

// TestValidateSecretsExpression tests the validateSecretsExpression function directly
func TestValidateSecretsExpression(t *testing.T) {
	tests := []struct {
		name        string
		value       string
		expectError bool
	}{
		// Valid cases
		{"valid simple secret", "${{ secrets.GITHUB_TOKEN }}", false},
		{"valid with fallback", "${{ secrets.TOKEN1 || secrets.TOKEN2 }}", false},
		{"valid with multiple fallbacks", "${{ secrets.TOKEN1 || secrets.TOKEN2 || secrets.TOKEN3 }}", false},
		{"valid with spaces", "${{  secrets.MY_TOKEN  }}", false},
		{"valid underscore prefix", "${{ secrets._PRIVATE }}", false},
		{"valid with numbers", "${{ secrets.TOKEN_V2 }}", false},

		// Invalid cases
		{"invalid plaintext", "my-secret", true},
		{"invalid GitHub PAT", "ghp_1234567890abcdef", true},
		{"invalid env reference", "${{ env.MY_TOKEN }}", true},
		{"invalid vars reference", "${{ vars.MY_TOKEN }}", true},
		{"invalid github context", "${{ github.token }}", true},
		{"invalid missing closing", "${{ secrets.MY_TOKEN", true},
		{"invalid missing opening", "secrets.MY_TOKEN }}", true},
		{"invalid empty", "", true},
		{"invalid with text prefix", "Bearer ${{ secrets.TOKEN }}", true},
		{"invalid with text suffix", "${{ secrets.TOKEN }} extra", true},
		{"invalid mixed contexts", "${{ secrets.TOKEN || env.FALLBACK }}", true},
		{"invalid number prefix", "${{ secrets.123TOKEN }}", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSecretsExpression(tt.value)
			if tt.expectError && err == nil {
				t.Errorf("Expected error, got nil")
			} else if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}
		})
	}
}

// TestJobsSecretsSchemaValidation tests that the JSON schema correctly validates jobs.secrets
func TestJobsSecretsSchemaValidation(t *testing.T) {
	// This test verifies that the schema validation catches invalid patterns
	// before the Go code validation runs
	tests := []struct {
		name        string
		markdown    string
		expectError bool
		errorMsg    string
	}{
		{
			name: "schema rejects plaintext in secrets",
			markdown: `---
on: workflow_dispatch
engine: custom
jobs:
  deploy:
    uses: ./.github/workflows/deploy.yml
    secrets:
      token: plaintext-secret
---
Test for schema validation.`,
			expectError: true,
			errorMsg:    "does not match pattern",
		},
		{
			name: "schema accepts valid secret expression",
			markdown: `---
on: workflow_dispatch
engine: custom
jobs:
  deploy:
    uses: ./.github/workflows/deploy.yml
    secrets:
      token: ${{ secrets.GITHUB_TOKEN }}
---
Test for schema validation.`,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			workflowPath := filepath.Join(tempDir, "test.md")

			err := os.WriteFile(workflowPath, []byte(tt.markdown), 0o644)
			if err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			compiler := NewCompiler()
			err = compiler.CompileWorkflow(workflowPath)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error containing %q, got no error", tt.errorMsg)
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing %q, got: %v", tt.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got: %v", err)
				}
			}
		})
	}
}

// TestJobsSecretsWithRealWorkflow tests with a more realistic workflow structure
func TestJobsSecretsWithRealWorkflow(t *testing.T) {
	markdown := `---
on:
  workflow_dispatch:
    inputs:
      environment:
        type: string
        default: production

permissions: read-all

engine: custom

jobs:
  deploy:
    uses: ./.github/workflows/reusable-deploy.yml
    with:
      environment: ${{ inputs.environment }}
    secrets:
      deploy_token: ${{ secrets.DEPLOY_TOKEN }}
      api_key: ${{ secrets.API_KEY || secrets.API_KEY_FALLBACK }}
      db_password: ${{ secrets.DB_PASSWORD }}

  notify:
    needs: deploy
    uses: ./.github/workflows/reusable-notify.yml
    secrets:
      slack_token: ${{ secrets.SLACK_TOKEN }}
---

# Deploy Application

This workflow deploys the application using reusable workflows with proper secret handling.
`

	tempDir := t.TempDir()
	workflowPath := filepath.Join(tempDir, "deploy.md")

	err := os.WriteFile(workflowPath, []byte(markdown), 0o644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(workflowPath)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Read the generated lock file
	lockPath := filepath.Join(tempDir, "deploy.lock.yml")
	yamlBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	yamlStr := string(yamlBytes)

	// Verify the compiled workflow contains the secrets
	if !strings.Contains(yamlStr, "secrets:") {
		t.Error("Expected YAML to contain 'secrets:' section")
	}
	if !strings.Contains(yamlStr, "deploy_token:") {
		t.Error("Expected YAML to contain 'deploy_token:' field")
	}
}
