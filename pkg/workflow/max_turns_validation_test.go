//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestMaxTurnsValidationWithUnsupportedEngine(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		engine      string
		expectError bool
		errorMsg    string
	}{
		{
			name: "max-turns with codex engine should fail",
			content: `---
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine:
  id: codex
  max-turns: 5
---

# Test Workflow

This should fail because codex doesn't support max-turns.`,
			engine:      "codex",
			expectError: true,
			errorMsg:    "max-turns not supported: engine 'codex' does not support the max-turns feature",
		},
		{
			name: "max-turns with claude engine should succeed",
			content: `---
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine:
  id: claude
  max-turns: 5
---

# Test Workflow

This should succeed because claude supports max-turns.`,
			engine:      "claude",
			expectError: false,
		},
		{
			name: "codex engine without max-turns should succeed",
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

This should succeed because no max-turns is specified.`,
			engine:      "codex",
			expectError: false,
		},
		{
			name: "claude engine without max-turns should succeed",
			content: `---
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
---

# Test Workflow

This should succeed because no max-turns is specified.`,
			engine:      "claude",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for the test
			tmpDir := testutil.TempDir(t, "max-turns-validation-test")

			// Create a test workflow file
			testFile := filepath.Join(tmpDir, "test.md")
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			// Create a compiler instance
			compiler := NewCompiler()
			compiler.SetSkipValidation(false)

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

func TestEngineSupportsMaxTurns(t *testing.T) {
	tests := []struct {
		name            string
		engineID        string
		expectedSupport bool
	}{
		{
			name:            "claude engine supports max-turns",
			engineID:        "claude",
			expectedSupport: true,
		},
		{
			name:            "codex engine does not support max-turns",
			engineID:        "codex",
			expectedSupport: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := GetGlobalEngineRegistry()
			engine, err := registry.GetEngine(tt.engineID)
			if err != nil {
				t.Fatalf("Failed to get engine '%s': %v", tt.engineID, err)
			}

			actualSupport := engine.SupportsMaxTurns()
			if actualSupport != tt.expectedSupport {
				t.Errorf("Expected engine '%s' to have SupportsMaxTurns() = %v, but got %v",
					tt.engineID, tt.expectedSupport, actualSupport)
			}
		})
	}
}
