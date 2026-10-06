//go:build integration

package workflow

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
)

// countInNonCommentLines counts occurrences of a string in non-comment lines
// A comment line is one that starts with '#' (after trimming leading whitespace)
func countInNonCommentLines(content, search string) int {
	count := 0
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		// Skip comment lines
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		count += strings.Count(line, search)
	}
	return count
}

// Note: indexInNonCommentLines is defined in compiler_test.go

func TestRuntimeSetupIntegration(t *testing.T) {
	tests := []struct {
		name             string
		workflowMarkdown string
		expectSetup      []string
		notExpectSetup   []string
	}{
		{
			name: "auto-detects node from npm command",
			workflowMarkdown: `---
on: push
engine: copilot
steps:
  - name: Install dependencies
    run: npm install
---

# Test workflow`,
			expectSetup: []string{
				"Setup Node.js",
				"uses: actions/setup-node@", // SHA varies
				"node-version: '24'",
			},
		},
		{
			name: "auto-detects python from python command",
			workflowMarkdown: `---
on: push
engine: copilot
steps:
  - name: Run script
    run: python test.py
---

# Test workflow`,
			expectSetup: []string{
				"Setup Python",
				"uses: actions/setup-python@", // SHA varies
				"python-version: '3.12'",
			},
		},
		{
			name: "auto-detects uv from uvx command",
			workflowMarkdown: `---
on: push
engine: copilot
steps:
  - name: Run ruff
    run: uvx ruff check
---

# Test workflow`,
			expectSetup: []string{
				"Setup uv",
				"uses: astral-sh/setup-uv@", // SHA varies
			},
		},
		{
			name: "auto-detects multiple runtimes",
			workflowMarkdown: `---
on: push
engine: copilot
steps:
  - name: Install
    run: npm install
  - name: Test
    run: python test.py
---

# Test workflow`,
			expectSetup: []string{
				"Setup Node.js",
				"Setup Python",
			},
		},
		{
			name: "skips auto-detection when setup action exists",
			workflowMarkdown: `---
on: push
engine: copilot
steps:
  - name: Setup Node.js
    uses: actions/setup-node@v4 # SHA will be pinned
    with:
      node-version: '24'
  - name: Install
    run: npm install
---

# Test workflow`,
			expectSetup: []string{
				"node-version:", // Should keep user's version (check for key)
				"24",            // Check for the value (regardless of quote type)
			},
			notExpectSetup: []string{
				// Should not add a second Node.js setup with different version
			},
		},
		{
			name: "detects runtime from MCP server config",
			workflowMarkdown: `---
on: push
engine: copilot
mcp-servers:
  custom-tool:
    command: python
    args: ["-m", "my_server"]
---

# Test workflow`,
			expectSetup: []string{
				"Setup Python",
				"uses: actions/setup-python@", // SHA varies
			},
		},
		{
			name: "no auto-detection for workflows without runtime commands in steps",
			workflowMarkdown: `---
on: push
engine:
  id: claude
steps:
  - name: Echo
    run: echo "Hello"
---

# Test workflow`,
			notExpectSetup: []string{
				"Setup Python",
				"Setup uv",
				"Setup Go",
				"Setup Ruby",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory for test files
			tmpDir := testutil.TempDir(t, "test-*")
			testFile := tmpDir + "/test-workflow.md"

			// Write test workflow
			if err := os.WriteFile(testFile, []byte(tt.workflowMarkdown), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			// Compile the workflow
			compiler := NewCompiler()
			if err := compiler.CompileWorkflow(testFile); err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			// Read the generated lock file
			lockFile := stringutil.MarkdownToLockFile(testFile)
			content, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}

			lockContent := string(content)

			// Check expected setup steps
			for _, expected := range tt.expectSetup {
				if !strings.Contains(lockContent, expected) {
					// Show a snippet of the lock file for context (first 100 lines)
					lines := strings.Split(lockContent, "\n")
					snippet := strings.Join(lines[:min(100, len(lines))], "\n")
					t.Errorf("Expected to find '%s' in lock file but didn't.\nFirst 100 lines:\n%s\n...(truncated)", expected, snippet)
				}
			}

			// Check that unwanted setup steps are not present
			for _, notExpected := range tt.notExpectSetup {
				if strings.Contains(lockContent, notExpected) {
					// Find the line containing the unexpected string for context
					lines := strings.Split(lockContent, "\n")
					var contextLines []string
					for i, line := range lines {
						if strings.Contains(line, notExpected) {
							start := max(0, i-3)
							end := min(len(lines), i+4)
							contextLines = append(contextLines, fmt.Sprintf("Lines %d-%d:", start+1, end))
							contextLines = append(contextLines, lines[start:end]...)
							break
						}
					}
					t.Errorf("Did not expect to find '%s' in lock file but it was present.\nContext:\n%s", notExpected, strings.Join(contextLines, "\n"))
				}
			}
		})
	}
}

func TestRuntimeSetupWithInlineCopilotDriver(t *testing.T) {
	workflowMarkdown := `---
on: workflow_dispatch
engine:
  id: copilot
  driver:
    python: |
      import sys
      print("hello from inline driver", file=sys.stderr)
---

# Test workflow`

	tmpDir := testutil.TempDir(t, "test-inline-driver-*")
	testFile := tmpDir + "/test-workflow.md"

	if err := os.WriteFile(testFile, []byte(workflowMarkdown), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(testFile)
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContent := string(content)
	agentJobSection := extractJobSection(lockContent, "agent")

	expectedStrings := []string{
		"Setup Python",
		"Write Inline Copilot SDK Driver",
		"Install GitHub Copilot SDK (Python)",
		".gh-aw/copilot-sdk/inline-driver.py",
		`print("hello from inline driver", file=sys.stderr)`,
		".gh-aw/copilot-sdk/inline-driver",
		"PYTHONPATH: ${{ github.workspace }}/.gh-aw/copilot-sdk/python",
	}
	for _, expected := range expectedStrings {
		if !strings.Contains(lockContent, expected) {
			t.Errorf("Expected compiled workflow to contain %q", expected)
		}
	}

	setupPythonIndex := indexInNonCommentLines(agentJobSection, "Setup Python")
	writeDriverIndex := indexInNonCommentLines(agentJobSection, "Write Inline Copilot SDK Driver")
	installSDKIndex := indexInNonCommentLines(agentJobSection, "Install GitHub Copilot SDK (Python)")
	if setupPythonIndex == -1 || writeDriverIndex == -1 || installSDKIndex == -1 {
		t.Fatalf("Expected inline driver setup steps in agent job, got:\n%s", agentJobSection)
	}
	if !(setupPythonIndex < writeDriverIndex && writeDriverIndex < installSDKIndex) {
		t.Fatalf("Expected runtime setup, inline driver write, and SDK install ordering in agent job, got:\n%s", agentJobSection)
	}

	if !containsInNonCommentLines(agentJobSection, "${GITHUB_WORKSPACE}/.gh-aw/copilot-sdk/inline-driver") {
		t.Fatalf("Expected agent job to execute the generated inline driver wrapper, got:\n%s", agentJobSection)
	}
}

func TestRuntimeSetupWithEngineNode(t *testing.T) {
	// Test that auto-detected runtime setup works alongside engine requirements
	// Both the auto-detection and engine may add setup steps, which is acceptable
	workflowMarkdown := `---
on: push
engine: claude
steps:
  - name: Install dependencies
    run: npm install
---

# Test workflow`

	tmpDir := testutil.TempDir(t, "test-*")
	testFile := tmpDir + "/test-workflow.md"

	if err := os.WriteFile(testFile, []byte(workflowMarkdown), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(testFile)
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContent := string(content)

	// Should have Node.js setup (from auto-detection or engine, or both)
	if !strings.Contains(lockContent, "Setup Node.js") {
		t.Error("Expected Node.js setup to be present")
	}

	// It's acceptable to have Node.js setup appear twice:
	// - Once from auto-detection for engine steps
	// - Once from engine requirements
	// This is not a problem as GitHub Actions will use the first setup
}

func TestRuntimeSetupPreservesUserVersions(t *testing.T) {
	// Test that when user specifies a version in setup action, we don't override it
	workflowMarkdown := `---
on: push
engine: copilot
steps:
  - name: Setup Python
    uses: actions/setup-python@v5 # SHA will be pinned
    with:
      python-version: '3.9'
  - name: Run script
    run: python test.py
---

# Test workflow`

	tmpDir := testutil.TempDir(t, "test-*")
	testFile := tmpDir + "/test-workflow.md"

	if err := os.WriteFile(testFile, []byte(workflowMarkdown), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(testFile)
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContent := string(content)

	// Should preserve user's version (3.9) - check without quotes since YAML formatting may vary
	if !strings.Contains(lockContent, "python-version") || !strings.Contains(lockContent, "3.9") {
		t.Error("Expected to preserve user's Python version 3.9")
	}

	// Should not add default version (3.12) - check specifically for python-version to avoid
	// false positives from other version strings like AWF version "0.13.12"
	if strings.Contains(lockContent, `python-version: '3.12'`) || strings.Contains(lockContent, `python-version: "3.12"`) {
		t.Error("Should not override user's version with default version")
	}

	// Should only have one Python setup (excluding comment lines where frontmatter is embedded)
	count := countInNonCommentLines(lockContent, "Setup Python")
	if count > 1 {
		t.Errorf("Expected 'Setup Python' to appear once, but found %d occurrences", count)
	}
}

func TestUVDetectionAddsPythonDependency(t *testing.T) {
	// Test that when uv is detected via MCP server, Python is automatically added
	workflowMarkdown := `---
on: push
engine: copilot
mcp-servers:
  serena:
    command: "uvx"
    args:
      - "--from"
      - "git+https://github.com/oraios/serena"
      - "serena"
      - "start-mcp-server"
steps:
  - name: Verify uv
    run: uv --version
---

# Test workflow with uv`

	tmpDir := testutil.TempDir(t, "test-*")
	testFile := tmpDir + "/test-workflow.md"

	if err := os.WriteFile(testFile, []byte(workflowMarkdown), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(testFile)
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContent := string(content)

	// Should have Python setup
	if !strings.Contains(lockContent, "Setup Python") {
		t.Error("Expected 'Setup Python' to be added as dependency for uv")
	}

	// Should have uv setup
	if !strings.Contains(lockContent, "Setup uv") {
		t.Error("Expected 'Setup uv' to be added")
	}

	// Python setup should come before uv setup (in non-comment lines)
	pythonIndex := indexInNonCommentLines(lockContent, "Setup Python")
	uvIndex := indexInNonCommentLines(lockContent, "Setup uv")
	if pythonIndex > uvIndex {
		t.Error("Setup Python should come before Setup uv (Python is a dependency of uv)")
	}

	// Both should come before "Verify uv" step (in non-comment lines)
	verifyIndex := indexInNonCommentLines(lockContent, "Verify uv")
	if pythonIndex > verifyIndex || uvIndex > verifyIndex {
		t.Error("Setup steps should come before 'Verify uv' step")
	}
}

// TestCustomImageRunnerNodeSetupIntegration verifies that Node.js is automatically
// set up in the agent job when a custom image runner is specified.
func TestCustomImageRunnerNodeSetupIntegration(t *testing.T) {
	tests := []struct {
		name              string
		workflowMarkdown  string
		expectNodeSetup   bool
		expectNodeVersion string // if non-empty, assert this node-version appears in the setup step
	}{
		{
			name: "self-hosted runner gets Node.js setup",
			workflowMarkdown: `---
on: push
engine: copilot
runs-on: self-hosted
---

# Test workflow`,
			expectNodeSetup: true,
		},
		{
			name: "custom enterprise runner gets Node.js setup",
			workflowMarkdown: `---
on: push
engine: copilot
runs-on: enterprise-custom-runner
---

# Test workflow`,
			expectNodeSetup: true,
		},
		{
			name: "ubuntu-latest does not get extra Node.js setup from runtime manager",
			workflowMarkdown: `---
on: push
engine: copilot
runs-on: ubuntu-latest
---

# Test workflow`,
			expectNodeSetup: false,
		},
		{
			name: "default runner (no runs-on) does not get extra Node.js setup",
			workflowMarkdown: `---
on: push
engine: copilot
---

# Test workflow`,
			expectNodeSetup: false,
		},
		{
			name: "custom runner with node version override uses overridden version",
			workflowMarkdown: `---
on: push
engine: copilot
runs-on: self-hosted
runtimes:
  node:
    version: '22'
---

# Test workflow`,
			expectNodeSetup:   true,
			expectNodeVersion: "22",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "test-custom-runner-*")
			testFile := tmpDir + "/test-workflow.md"

			if err := os.WriteFile(testFile, []byte(tt.workflowMarkdown), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			compiler := NewCompiler()
			if err := compiler.CompileWorkflow(testFile); err != nil {
				t.Fatalf("Compilation failed: %v", err)
			}

			lockFile := stringutil.MarkdownToLockFile(testFile)
			content, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}
			lockContent := string(content)

			// For the agent job, check whether a Setup Node.js step appears
			// from the runtime manager (before engine installation steps).
			// The Copilot engine uses a binary installer (no npm), so any
			// "Setup Node.js" step in the agent job comes from the runtime manager.
			agentJobSection := extractJobSection(lockContent, "agent")

			hasNodeSetup := strings.Contains(agentJobSection, "Setup Node.js") &&
				strings.Contains(agentJobSection, "actions/setup-node@")

			if tt.expectNodeSetup {
				assert.True(t, hasNodeSetup, "Expected Node.js setup step in agent job for custom runner.\nAgent job:\n%s", agentJobSection)
				// If a specific node version is expected, verify it appears in the setup step.
				if tt.expectNodeVersion != "" {
					assert.Contains(t, agentJobSection, "node-version: '"+tt.expectNodeVersion+"'",
						"Expected node-version '%s' in Setup Node.js step.\nAgent job:\n%s", tt.expectNodeVersion, agentJobSection)
				}
			} else {
				assert.False(t, hasNodeSetup, "Did not expect Node.js setup step from runtime manager in agent job for standard runner.\nAgent job:\n%s", agentJobSection)
			}
		})
	}
}
