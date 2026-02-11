//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStrictModeDeprecatedFields(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expectError bool
		errorMsg    string
	}{
		{
			name: "removed timeout_minutes field rejected (unknown property)",
			content: `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
timeout_minutes: 10
engine: copilot
network:
  allowed:
    - "api.example.com"
---

# Test Workflow`,
			expectError: true,
			errorMsg:    "Unknown property: timeout_minutes",
		},
		{
			name: "non-deprecated timeout-minutes field allowed in strict mode",
			content: `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
timeout-minutes: 10
engine: copilot
network:
  allowed:
    - "api.example.com"
---

# Test Workflow`,
			expectError: false,
		},
		{
			name: "removed field rejected even in non-strict mode",
			content: `---
on: push
permissions:
  contents: read
timeout_minutes: 10
engine: copilot
strict: false
---

# Test Workflow`,
			expectError: true,
			errorMsg:    "Unknown property: timeout_minutes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir, err := os.MkdirTemp("", "strict-deprecated-test")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(tmpDir)

			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()

			// Determine if we should enable strict mode based on test name
			if strings.Contains(tt.name, "strict mode") && !strings.Contains(tt.name, "non-strict") {
				compiler.SetStrictMode(true)
			}

			err = compiler.CompileWorkflow(testFile)

			if tt.expectError && err == nil {
				t.Error("Expected compilation to fail but it succeeded")
			} else if !tt.expectError && err != nil {
				t.Errorf("Expected compilation to succeed but it failed: %v", err)
			} else if tt.expectError && err != nil && tt.errorMsg != "" {
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errorMsg, err.Error())
				}
			}
		})
	}
}

func TestStrictModeDeprecatedFieldErrorMessage(t *testing.T) {
	content := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
timeout_minutes: 10
engine: copilot
network:
  allowed:
    - "api.example.com"
---

# Test Workflow`

	tmpDir, err := os.MkdirTemp("", "strict-deprecated-message-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	compiler.SetStrictMode(true)
	err = compiler.CompileWorkflow(testFile)

	if err == nil {
		t.Fatal("Expected compilation to fail but it succeeded")
	}

	errorMsg := err.Error()

	// Check that error message includes:
	// 1. Mentions unknown property (timeout_minutes has been removed from schema)
	if !strings.Contains(errorMsg, "Unknown property") {
		t.Errorf("Error message should mention 'Unknown property': %s", errorMsg)
	}

	// 2. Mentions the specific field
	if !strings.Contains(errorMsg, "timeout_minutes") {
		t.Errorf("Error message should mention 'timeout_minutes': %s", errorMsg)
	}

	// 3. Provides replacement suggestion
	if !strings.Contains(errorMsg, "timeout-minutes") {
		t.Errorf("Error message should suggest 'timeout-minutes': %s", errorMsg)
	}
}
