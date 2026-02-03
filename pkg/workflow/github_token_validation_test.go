//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestGitHubTokenValidation(t *testing.T) {
	tests := []struct {
		name        string
		token       string
		expectError bool
		errorMsg    string
	}{
		// Valid cases
		{
			name:        "valid secret expression - GITHUB_TOKEN",
			token:       "${{ secrets.GITHUB_TOKEN }}",
			expectError: false,
		},
		{
			name:        "valid secret expression - custom PAT",
			token:       "${{ secrets.CUSTOM_PAT }}",
			expectError: false,
		},
		{
			name:        "valid secret expression - with fallback",
			token:       "${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}",
			expectError: false,
		},
		{
			name:        "valid secret expression - with spaces",
			token:       "${{  secrets.MY_TOKEN  }}",
			expectError: false,
		},
		{
			name:        "valid secret expression - underscore prefix",
			token:       "${{ secrets._PRIVATE_TOKEN }}",
			expectError: false,
		},
		{
			name:        "valid secret expression - numbers in name",
			token:       "${{ secrets.TOKEN_V2 }}",
			expectError: false,
		},
		{
			name:        "valid secret expression - multiple fallbacks",
			token:       "${{ secrets.TOKEN1 || secrets.TOKEN2 }}",
			expectError: false,
		},
		// Invalid cases - plaintext secrets
		{
			name:        "invalid - plaintext GitHub PAT",
			token:       "ghp_1234567890abcdefghijklmnopqrstuvwxyz",
			expectError: true,
			errorMsg:    "github-token",
		},
		{
			name:        "invalid - plaintext classic token",
			token:       "github_pat_11AAAAAA",
			expectError: true,
			errorMsg:    "github-token",
		},
		{
			name:        "invalid - plaintext string",
			token:       "my-secret-token",
			expectError: true,
			errorMsg:    "github-token",
		},
		{
			name:        "invalid - empty string",
			token:       "",
			expectError: true,
			errorMsg:    "github-token",
		},
		{
			name:        "invalid - partial expression without secrets",
			token:       "${{ env.GITHUB_TOKEN }}",
			expectError: true,
			errorMsg:    "github-token",
		},
		{
			name:        "invalid - missing closing braces",
			token:       "${{ secrets.GITHUB_TOKEN",
			expectError: true,
			errorMsg:    "github-token",
		},
		{
			name:        "invalid - missing opening braces",
			token:       "secrets.GITHUB_TOKEN }}",
			expectError: true,
			errorMsg:    "github-token",
		},
		{
			name:        "invalid - just the word secrets",
			token:       "secrets.GITHUB_TOKEN",
			expectError: true,
			errorMsg:    "github-token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "github-token-validation-test")

			testContent := `---
name: Test GitHub Token Validation
on:
  workflow_dispatch:
engine: copilot
github-token: ` + tt.token + `
tools:
  github:
    allowed: [list_issues]
---

# Test GitHub Token Validation
`

			testFile := filepath.Join(tmpDir, "test-token.md")
			if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()
			err := compiler.CompileWorkflow(testFile)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for token %q, but got none", tt.token)
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error to contain %q, got: %v", tt.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for token %q, but got: %v", tt.token, err)
				}
			}
		})
	}
}

func TestGitHubTokenValidationInSafeOutputs(t *testing.T) {
	tests := []struct {
		name        string
		token       string
		expectError bool
	}{
		{
			name:        "valid token in safe-outputs",
			token:       "${{ secrets.SAFE_OUTPUTS_PAT }}",
			expectError: false,
		},
		{
			name:        "invalid token in safe-outputs",
			token:       "ghp_plaintext_token",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "safe-outputs-token-test")

			testContent := `---
name: Test Safe-Outputs Token Validation
on:
  issues:
    types: [opened]
engine: copilot
safe-outputs:
  github-token: ` + tt.token + `
  create-issue:
---

# Test Safe-Outputs Token
`

			testFile := filepath.Join(tmpDir, "test-safe-outputs.md")
			if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()
			err := compiler.CompileWorkflow(testFile)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for token %q, but got none", tt.token)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for token %q, but got: %v", tt.token, err)
				}
			}
		})
	}
}

func TestGitHubTokenValidationInIndividualSafeOutput(t *testing.T) {
	tests := []struct {
		name        string
		token       string
		expectError bool
	}{
		{
			name:        "valid token in individual safe-output",
			token:       "${{ secrets.INDIVIDUAL_PAT }}",
			expectError: false,
		},
		{
			name:        "invalid token in individual safe-output",
			token:       "github_pat_plaintext",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "individual-token-test")

			testContent := `---
name: Test Individual Safe-Output Token
on:
  issues:
    types: [opened]
engine: copilot
safe-outputs:
  create-agent-session:
    github-token: ` + tt.token + `
---

# Test Individual Token
`

			testFile := filepath.Join(tmpDir, "test-individual.md")
			if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()
			err := compiler.CompileWorkflow(testFile)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for token %q, but got none", tt.token)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for token %q, but got: %v", tt.token, err)
				}
			}
		})
	}
}

func TestGitHubTokenValidationInGitHubTool(t *testing.T) {
	tests := []struct {
		name        string
		token       string
		expectError bool
	}{
		{
			name:        "valid token in github tool",
			token:       "${{ secrets.GITHUB_TOOL_PAT }}",
			expectError: false,
		},
		{
			name:        "invalid token in github tool",
			token:       "plaintext_secret",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "github-tool-token-test")

			testContent := `---
name: Test GitHub Tool Token
on:
  workflow_dispatch:
engine: copilot
tools:
  github:
    github-token: ` + tt.token + `
    allowed: [list_issues]
---

# Test GitHub Tool Token
`

			testFile := filepath.Join(tmpDir, "test-github-tool.md")
			if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()
			err := compiler.CompileWorkflow(testFile)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for token %q, but got none", tt.token)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for token %q, but got: %v", tt.token, err)
				}
			}
		})
	}
}

func TestGitHubTokenValidationErrorMessage(t *testing.T) {
	tmpDir := testutil.TempDir(t, "error-message-test")

	testContent := `---
name: Test Error Message
on:
  workflow_dispatch:
engine: copilot
github-token: ghp_actualSecretInPlainText
---

# Test Error Message
`

	testFile := filepath.Join(tmpDir, "test-error.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(testFile)

	if err == nil {
		t.Fatal("Expected validation error, got none")
	}

	// The error should be clear and helpful
	errorMsg := err.Error()
	if !strings.Contains(errorMsg, "github-token") {
		t.Errorf("Error message should mention 'github-token', got: %s", errorMsg)
	}
}

func TestMultipleGitHubTokenValidations(t *testing.T) {
	tmpDir := testutil.TempDir(t, "multiple-tokens-test")

	// Test that validation catches errors in any of the token locations
	testContent := `---
name: Test Multiple Tokens
on:
  workflow_dispatch:
engine: copilot
github-token: ${{ secrets.TOPLEVEL_TOKEN }}
tools:
  github:
    github-token: plaintext_token_in_github_tool
    allowed: [list_issues]
safe-outputs:
  create-issue:
    github-token: ${{ secrets.SAFE_OUTPUT_TOKEN }}
---

# Test Multiple Tokens
`

	testFile := filepath.Join(tmpDir, "test-multiple.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(testFile)

	// Should fail due to plaintext token in github tool
	if err == nil {
		t.Fatal("Expected validation error for invalid github tool token, got none")
	}
}
