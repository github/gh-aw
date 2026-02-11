//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestMaxTurnsCompilation(t *testing.T) {
	tests := []struct {
		name             string
		content          string
		expectedMaxTurns string
		shouldInclude    bool
	}{
		{
			name: "workflow with max-turns",
			content: `---
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine:
  id: claude
  max-turns: 3
tools:
  github:
    allowed: [issue_read]
---

# Test Max Turns

This workflow tests the max-turns feature.`,
			expectedMaxTurns: "--max-turns 3",
			shouldInclude:    true,
		},
		{
			name: "workflow without max-turns",
			content: `---
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
tools:
  github:
    allowed: [issue_read]
---

# Test Without Max Turns

This workflow should not include max-turns.`,
			expectedMaxTurns: "",
			shouldInclude:    false,
		},
		{
			name: "workflow with max-turns and timeout",
			content: `---
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine:
  id: claude
  max-turns: 10
timeout-minutes: 15
strict: false
tools:
  github:
    allowed: [issue_read]
---

# Test Max Turns and Timeout

This workflow tests max-turns with timeout.`,
			expectedMaxTurns: "--max-turns 10",
			shouldInclude:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for the test
			tmpDir := testutil.TempDir(t, "max-turns-test")

			// Create the test workflow file
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
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

			lockContentStr := string(lockContent)

			if tt.shouldInclude {
				// Verify max_turns is included in the generated workflow
				if !strings.Contains(lockContentStr, tt.expectedMaxTurns) {
					t.Errorf("Expected max_turns to be included in generated workflow. Expected: %s\nActual content:\n%s", tt.expectedMaxTurns, lockContentStr)
				}

				// Verify GH_AW_MAX_TURNS environment variable is set
				expectedEnvVar := "GH_AW_MAX_TURNS: " + strings.TrimPrefix(tt.expectedMaxTurns, "--max-turns ")
				if !strings.Contains(lockContentStr, expectedEnvVar) {
					t.Errorf("Expected GH_AW_MAX_TURNS environment variable to be set. Expected: %s\nActual content:\n%s", expectedEnvVar, lockContentStr)
				}

				// Verify it's in the correct context (under the Claude CLI execution)
				if !strings.Contains(lockContentStr, "claude --print") {
					t.Error("Expected to find claude command in generated workflow")
				}

				// Look for max_turns in the claude_args section (v1.0 format)
				lines := strings.Split(lockContentStr, "\n")
				foundAction := false
				foundMaxTurns := false
				for _, line := range lines {
					if strings.Contains(line, "claude --print") {
						foundAction = true
						// Check if --max-turns is in the same line or subsequent run lines
						if strings.Contains(line, "--max-turns") {
							foundMaxTurns = true
							break
						}
					}
					// Also check in run command lines that might span multiple lines
					if foundAction && strings.Contains(line, "--max-turns") {
						foundMaxTurns = true
						break
					}
				}

				if !foundMaxTurns {
					t.Error("Expected to find --max-turns in the CLI command")
				}
			} else {
				// Verify max_turns is NOT included when not specified
				if strings.Contains(lockContentStr, "max_turns:") {
					t.Error("Expected max_turns NOT to be included when not specified in frontmatter")
				}

				// Verify GH_AW_MAX_TURNS is NOT included when not specified
				if strings.Contains(lockContentStr, "GH_AW_MAX_TURNS:") {
					t.Error("Expected GH_AW_MAX_TURNS NOT to be included when max-turns not specified in frontmatter")
				}
			}
		})
	}
}

func TestMaxTurnsValidation(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expectError bool
	}{
		{
			name: "valid integer max-turns",
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

# Valid Max Turns`,
			expectError: false,
		},
		{
			name: "valid string max-turns",
			content: `---
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine:
  id: claude
  max-turns: "5"
---

# Valid String Max Turns`,
			expectError: false,
		},
		{
			name: "zero max-turns",
			content: `---
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine:
  id: claude
  max-turns: 0
---

# Zero Max Turns`,
			expectError: false, // Zero should be valid (might mean unlimited)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for the test
			tmpDir := testutil.TempDir(t, "max-turns-validation-test")

			// Create the test workflow file
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			// Compile the workflow
			compiler := NewCompiler()
			err := compiler.CompileWorkflow(testFile)

			if tt.expectError && err == nil {
				t.Error("Expected compilation to fail but it succeeded")
			} else if !tt.expectError && err != nil {
				t.Errorf("Expected compilation to succeed but it failed: %v", err)
			}
		})
	}
}

func TestCustomEngineWithMaxTurns(t *testing.T) {
	content := `---
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine:
  id: custom
  max-turns: 5
  steps:
    - name: Test step
      run: echo "Testing max-turns with custom engine"
---

# Custom Engine with Max Turns

This tests max-turns feature with custom engine.`

	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "custom-max-turns-test")

	// Create the test workflow file
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow with custom engine and max-turns: %v", err)
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(testFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Verify GH_AW_MAX_TURNS environment variable is set
	expectedEnvVar := "GH_AW_MAX_TURNS: \"5\""
	if !strings.Contains(lockContentStr, expectedEnvVar) {
		t.Errorf("Expected GH_AW_MAX_TURNS environment variable to be set. Expected: %s\nActual content:\n%s", expectedEnvVar, lockContentStr)
	}

	// Verify MCP config is generated for custom engine
	if !strings.Contains(lockContentStr, "/tmp/gh-aw/mcp-config/mcp-servers.json") {
		t.Error("Expected custom engine to generate MCP configuration file")
	}

	// Verify custom steps are included
	if !strings.Contains(lockContentStr, "echo \"Testing max-turns with custom engine\"") {
		t.Error("Expected custom steps to be included in generated workflow")
	}
}
