//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestMaxTokensValidationWithUnsupportedEngine(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		engine      string
		expectError bool
		errorMsg    string
	}{
		{
			name: "max-tokens with codex engine should fail",
			content: `---
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine:
  id: codex
  max-tokens: 4096
---

# Test Workflow

This should fail because codex doesn't support max-tokens.`,
			engine:      "codex",
			expectError: true,
			errorMsg:    "max-tokens not supported: engine 'codex' does not support the max-tokens feature",
		},
		{
			name: "max-tokens with claude engine should succeed",
			content: `---
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine:
  id: claude
  max-tokens: 4096
---

# Test Workflow

This should succeed because claude supports max-tokens.`,
			engine:      "claude",
			expectError: false,
		},
		{
			name: "max-tokens with custom engine should succeed",
			content: `---
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine:
  id: custom
  max-tokens: 4096
  steps:
    - name: Test Step
      run: echo "test"
---

# Test Workflow

This should succeed because custom supports max-tokens.`,
			engine:      "custom",
			expectError: false,
		},
		{
			name: "codex engine without max-tokens should succeed",
			content: `---
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: codex
---

# Test Workflow

This should succeed because no max-tokens is specified.`,
			engine:      "codex",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for the test
			tmpDir := testutil.TempDir(t, "max-tokens-validation-test")

			// Create a test workflow file
			testFile := filepath.Join(tmpDir, "test.md")
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			// Create a compiler instance
			compiler := NewCompiler()
			compiler.SetSkipValidation(false)
			compiler.SetStrictMode(true) // Enable strict mode for validation errors

			// Try to compile the workflow
			err := compiler.CompileWorkflow(testFile)

			if tt.expectError {
				// We expect an error
				if err == nil {
					t.Errorf("Expected error but compilation succeeded")
					return
				}

				// Check if the error message contains the expected text
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error message to contain '%s', but got: %s", tt.errorMsg, err.Error())
				}
			} else {
				// We don't expect an error
				if err != nil {
					t.Errorf("Expected compilation to succeed but got error: %v", err)
				}
			}
		})
	}
}

func TestMaxIterationsValidationWithUnsupportedEngine(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		engine      string
		expectError bool
		errorMsg    string
	}{
		{
			name: "max-iterations with claude engine should fail",
			content: `---
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine:
  id: claude
  max-iterations: 3
---

# Test Workflow

This should fail because claude doesn't support max-iterations.`,
			engine:      "claude",
			expectError: true,
			errorMsg:    "max-iterations not supported: engine 'claude' does not support the max-iterations feature",
		},
		{
			name: "max-iterations with custom engine should succeed",
			content: `---
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine:
  id: custom
  max-iterations: 3
  steps:
    - name: Test Step
      run: echo "test"
---

# Test Workflow

This should succeed because custom supports max-iterations.`,
			engine:      "custom",
			expectError: false,
		},
		{
			name: "codex engine without max-iterations should succeed",
			content: `---
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: codex
---

# Test Workflow

This should succeed because no max-iterations is specified.`,
			engine:      "codex",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for the test
			tmpDir := testutil.TempDir(t, "max-iterations-validation-test")

			// Create a test workflow file
			testFile := filepath.Join(tmpDir, "test.md")
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			// Create a compiler instance
			compiler := NewCompiler()
			compiler.SetSkipValidation(false)
			compiler.SetStrictMode(true) // Enable strict mode for validation errors

			// Try to compile the workflow
			err := compiler.CompileWorkflow(testFile)

			if tt.expectError {
				// We expect an error
				if err == nil {
					t.Errorf("Expected error but compilation succeeded")
					return
				}

				// Check if the error message contains the expected text
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error message to contain '%s', but got: %s", tt.errorMsg, err.Error())
				}
			} else {
				// We don't expect an error
				if err != nil {
					t.Errorf("Expected compilation to succeed but got error: %v", err)
				}
			}
		})
	}
}

func TestEngineSupportsExecutionBounds(t *testing.T) {
	tests := []struct {
		engineID              string
		supportsMaxTurns      bool
		supportsMaxTokens     bool
		supportsMaxIterations bool
	}{
		{
			engineID:              "claude",
			supportsMaxTurns:      true,
			supportsMaxTokens:     true,
			supportsMaxIterations: false,
		},
		{
			engineID:              "copilot",
			supportsMaxTurns:      false,
			supportsMaxTokens:     false,
			supportsMaxIterations: false,
		},
		{
			engineID:              "codex",
			supportsMaxTurns:      false,
			supportsMaxTokens:     false,
			supportsMaxIterations: false,
		},
		{
			engineID:              "custom",
			supportsMaxTurns:      true,
			supportsMaxTokens:     true,
			supportsMaxIterations: true,
		},
	}

	registry := GetGlobalEngineRegistry()

	for _, tt := range tests {
		t.Run(tt.engineID, func(t *testing.T) {
			engine, err := registry.GetEngine(tt.engineID)
			if err != nil {
				t.Fatalf("Failed to get engine '%s': %v", tt.engineID, err)
			}

			actualMaxTurns := engine.SupportsMaxTurns()
			if actualMaxTurns != tt.supportsMaxTurns {
				t.Errorf("Expected engine '%s' to have SupportsMaxTurns() = %v, but got %v",
					tt.engineID, tt.supportsMaxTurns, actualMaxTurns)
			}

			actualMaxTokens := engine.SupportsMaxTokens()
			if actualMaxTokens != tt.supportsMaxTokens {
				t.Errorf("Expected engine '%s' to have SupportsMaxTokens() = %v, but got %v",
					tt.engineID, tt.supportsMaxTokens, actualMaxTokens)
			}

			actualMaxIterations := engine.SupportsMaxIterations()
			if actualMaxIterations != tt.supportsMaxIterations {
				t.Errorf("Expected engine '%s' to have SupportsMaxIterations() = %v, but got %v",
					tt.engineID, tt.supportsMaxIterations, actualMaxIterations)
			}
		})
	}
}
