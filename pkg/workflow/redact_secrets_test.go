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

func TestCollectSecretReferences(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		expected []string
	}{
		{
			name: "Single secret reference",
			yaml: `env:
  GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}`,
			expected: []string{"GITHUB_TOKEN"},
		},
		{
			name: "Multiple secret references",
			yaml: `env:
  GITHUB_TOKEN: ${{ secrets.COPILOT_GITHUB_TOKEN }}
  API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
  TAVILY_KEY: ${{ secrets.TAVILY_API_KEY }}`,
			expected: []string{"ANTHROPIC_API_KEY", "COPILOT_GITHUB_TOKEN", "TAVILY_API_KEY"},
		},
		{
			name: "Secret references with OR fallback",
			yaml: `env:
  TOKEN: ${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}`,
			expected: []string{"GH_AW_GITHUB_MCP_SERVER_TOKEN", "GH_AW_GITHUB_TOKEN", "GITHUB_TOKEN"},
		},
		{
			name: "Duplicate secret references",
			yaml: `env:
  TOKEN1: ${{ secrets.API_KEY }}
  TOKEN2: ${{ secrets.API_KEY }}
  TOKEN3: ${{ secrets.API_KEY }}`,
			expected: []string{"API_KEY"},
		},
		{
			name: "No secret references",
			yaml: `env:
  FOO: bar
  BAZ: qux`,
			expected: []string{},
		},
		{
			name: "Mixed case - only uppercase secrets",
			yaml: `env:
  GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
  api_key: ${{ secrets.api_key }}`,
			expected: []string{"GITHUB_TOKEN"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CollectSecretReferences(tt.yaml)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d secrets, got %d", len(tt.expected), len(result))
				t.Logf("Expected: %v", tt.expected)
				t.Logf("Got: %v", result)
				return
			}

			for i, expected := range tt.expected {
				if result[i] != expected {
					t.Errorf("Expected secret[%d] = %s, got %s", i, expected, result[i])
				}
			}
		})
	}
}

func TestSecretRedactionStepGeneration(t *testing.T) {
	// Create a temporary directory for test
	tmpDir := testutil.TempDir(t, "secret-redaction-test")

	// Create a test workflow file
	testWorkflow := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
---

# Test Workflow

Test workflow for secret redaction.
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testWorkflow), 0644); err != nil {
		t.Fatal(err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(testFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockStr := string(lockContent)

	// Verify the redaction step is present (copilot engine has declared output files)
	if !strings.Contains(lockStr, "Redact secrets in logs") {
		t.Error("Expected redaction step in generated workflow")
	}

	// Verify the environment variable is set
	if !strings.Contains(lockStr, "GH_AW_SECRET_NAMES") {
		t.Error("Expected GH_AW_SECRET_NAMES environment variable")
	}

	// Verify secret environment variables are passed (both new and legacy names)
	if !strings.Contains(lockStr, "SECRET_COPILOT_GITHUB_TOKEN") {
		t.Error("Expected SECRET_COPILOT_GITHUB_TOKEN environment variable")
	}

	// Verify the redaction step uses actions/github-script
	if !strings.Contains(lockStr, "uses: actions/github-script@ed597411d8f924073f98dfc5c65a23a2325f34cd") {
		t.Error("Expected redaction step to use actions/github-script@ed597411d8f924073f98dfc5c65a23a2325f34cd")
	}

	// Verify the redaction step runs with if: always()
	redactionStepIdx := strings.Index(lockStr, "Redact secrets in logs")
	if redactionStepIdx == -1 {
		t.Fatal("Redaction step not found")
	}

	// Check that if: always() appears near the redaction step
	redactionSection := lockStr[redactionStepIdx:min(redactionStepIdx+500, len(lockStr))]
	if !strings.Contains(redactionSection, "if: always()") {
		t.Error("Expected redaction step to have 'if: always()' condition")
	}
}

func TestValidateSecretReferences(t *testing.T) {
	tests := []struct {
		name    string
		secrets []string
		wantErr bool
	}{
		{
			name:    "empty list",
			secrets: []string{},
			wantErr: false,
		},
		{
			name:    "valid secret names",
			secrets: []string{"GITHUB_TOKEN", "API_KEY", "MY_SECRET_123"},
			wantErr: false,
		},
		{
			name:    "invalid secret name - lowercase start",
			secrets: []string{"apiKey"},
			wantErr: true,
		},
		{
			name:    "invalid secret name - special characters",
			secrets: []string{"API-KEY"},
			wantErr: true,
		},
		{
			name:    "invalid secret name - spaces",
			secrets: []string{"API KEY"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSecretReferences(tt.secrets)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateSecretReferences() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
