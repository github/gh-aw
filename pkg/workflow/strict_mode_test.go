//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestStrictModeTimeout(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expectError bool
		errorMsg    string
	}{
		{
			name: "timeout not required in strict mode",
			content: `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
network:
  allowed:
    - "api.example.com"
---

# Test Workflow`,
			expectError: false,
		},
		{
			name: "timeout still valid in strict mode when specified",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "strict-timeout-test")

			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()
			compiler.SetStrictMode(true)
			err := compiler.CompileWorkflow(testFile)

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

func TestStrictModePermissions(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expectError bool
		errorMsg    string
	}{
		{
			name: "read permissions allowed in strict mode",
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
			name: "contents write permission refused in strict mode",
			content: `---
on: push
permissions:
  contents: write
  issues: read
  pull-requests: read
timeout-minutes: 10
engine: copilot
features:
  dangerous-permissions-write: true
---

# Test Workflow`,
			expectError: true,
			errorMsg:    "strict mode: write permission 'contents: write' is not allowed",
		},
		{
			name: "issues write permission refused in strict mode",
			content: `---
on: push
permissions:
  issues: write
timeout-minutes: 10
engine: copilot
features:
  dangerous-permissions-write: true
---

# Test Workflow`,
			expectError: true,
			errorMsg:    "strict mode: write permission 'issues: write' is not allowed",
		},
		{
			name: "pull-requests write permission refused in strict mode",
			content: `---
on: push
permissions:
  pull-requests: write
timeout-minutes: 10
engine: copilot
features:
  dangerous-permissions-write: true
---

# Test Workflow`,
			expectError: true,
			errorMsg:    "strict mode: write permission 'pull-requests: write' is not allowed",
		},
		{
			name: "no permissions specified allowed in strict mode",
			content: `---
on: push
timeout-minutes: 10
engine: copilot
network:
  allowed:
    - "api.example.com"
tools:
  github: false
  playwright:
    allowed_domains: ["example.com"]
---

# Test Workflow`,
			expectError: false,
		},
		{
			name: "shorthand write permission refused in strict mode",
			content: `---
on: push
permissions: write-all
features:
  dangerous-permissions-write: true
timeout-minutes: 10
engine: copilot
---

# Test Workflow`,
			expectError: true,
			errorMsg:    "strict mode: write permission 'contents: write' is not allowed",
		},
		{
			name: "shorthand write-all permission refused in strict mode",
			content: `---
on: push
permissions: write-all
features:
  dangerous-permissions-write: true
timeout-minutes: 10
engine: copilot
---

# Test Workflow`,
			expectError: true,
			errorMsg:    "strict mode: write permission 'contents: write' is not allowed",
		},

		{
			name: "shorthand read-all permission allowed in strict mode",
			content: `---
on: push
permissions: read-all
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
			name: "write permission with inline comment refused in strict mode",
			content: `---
on: push
permissions:
  contents: write # NOT IN STRICT MODE
  issues: read
  pull-requests: read
timeout-minutes: 10
engine: copilot
features:
  dangerous-permissions-write: true
---

# Test Workflow`,
			expectError: true,
			errorMsg:    "strict mode: write permission 'contents: write' is not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "strict-permissions-test")

			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()
			compiler.SetStrictMode(true)
			err := compiler.CompileWorkflow(testFile)

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

func TestStrictModeNetwork(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expectError bool
		errorMsg    string
	}{
		{
			name: "defaults network allowed in strict mode",
			content: `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
timeout-minutes: 10
engine: copilot
network: defaults
---

# Test Workflow`,
			expectError: false,
		},
		{
			name: "specific domains allowed in strict mode",
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
    - "*.trusted.com"
---

# Test Workflow`,
			expectError: false,
		},
		{
			name: "wildcard star refused in strict mode",
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
    - "*"
---

# Test Workflow`,
			expectError: true,
			errorMsg:    "strict mode: wildcard '*' is not allowed in network.allowed domains",
		},
		{
			name: "empty network object allowed in strict mode",
			content: `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
timeout-minutes: 10
engine: copilot
network: {}
---

# Test Workflow`,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "strict-network-test")

			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()
			compiler.SetStrictMode(true)
			err := compiler.CompileWorkflow(testFile)

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

func TestStrictModeMCPNetwork(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expectError bool
		errorMsg    string
	}{
		{
			name: "built-in tools do not require network configuration",
			content: `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
timeout-minutes: 10
engine: copilot
tools:
  github:
    allowed: [issue_read]
  bash: []
---

# Test Workflow`,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "strict-mcp-test")

			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()
			compiler.SetStrictMode(true)
			err := compiler.CompileWorkflow(testFile)

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

func TestStrictModeBashTools(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expectError bool
		errorMsg    string
	}{
		{
			name: "specific bash commands allowed in strict mode",
			content: `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
timeout-minutes: 10
engine: copilot
tools:
  bash: ["echo", "ls", "pwd"]
network:
  allowed:
    - "api.example.com"
---

# Test Workflow`,
			expectError: false,
		},
		{
			name: "bash null allowed in strict mode",
			content: `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
timeout-minutes: 10
engine: copilot
tools:
  bash: []
network:
  allowed:
    - "api.example.com"
---

# Test Workflow`,
			expectError: false,
		},
		{
			name: "bash empty array allowed in strict mode",
			content: `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
timeout-minutes: 10
engine: copilot
tools:
  bash: []
network:
  allowed:
    - "api.example.com"
---

# Test Workflow`,
			expectError: false,
		},
		{
			name: "bash wildcard star allowed in strict mode",
			content: `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
timeout-minutes: 10
engine: copilot
tools:
  bash: ["*"]
network:
  allowed:
    - "api.example.com"
---

# Test Workflow`,
			expectError: false,
		},
		{
			name: "bash wildcard colon-star allowed in strict mode",
			content: `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
timeout-minutes: 10
engine: copilot
tools:
  bash: [":*"]
network:
  allowed:
    - "api.example.com"
---

# Test Workflow`,
			expectError: false,
		},
		{
			name: "bash wildcard star mixed with commands allowed in strict mode",
			content: `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
timeout-minutes: 10
engine: copilot
tools:
  bash: ["echo", "ls", "*", "pwd"]
network:
  allowed:
    - "api.example.com"
---

# Test Workflow`,
			expectError: false,
		},
		{
			name: "bash command wildcards like git:* are allowed in strict mode",
			content: `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
timeout-minutes: 10
engine: copilot
tools:
  bash: ["git:*", "npm:*"]
network:
  allowed:
    - "api.example.com"
---

# Test Workflow`,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "strict-bash-test")

			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()
			compiler.SetStrictMode(true)
			err := compiler.CompileWorkflow(testFile)

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

// Note: Detailed MCP network validation tests are skipped because they require
// complex schema validation setup. The validation logic is implemented in
// validateStrictMCPNetwork() and will be triggered during actual workflow compilation
// when custom MCP servers with containers are detected.

func TestNonStrictModeAllowsAll(t *testing.T) {
	// Verify that non-strict mode (default) allows all configurations
	content := `---
on: push
permissions:
  contents: write
  issues: write
  pull-requests: read
engine: copilot
features:
  dangerous-permissions-write: true
strict: false
network:
  allowed:
    - "*"
---

# Test Workflow`

	tmpDir := testutil.TempDir(t, "non-strict-test")

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	// Do NOT set strict mode - should allow everything
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Errorf("Non-strict mode should allow all configurations, but got error: %v", err)
	}
}

func TestStrictModeFromFrontmatter(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expectError bool
		errorMsg    string
	}{
		{
			name: "strict: true in frontmatter enables strict mode",
			content: `---
on: push
strict: true
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
network:
  allowed:
    - "api.example.com"
---

# Test Workflow`,
			expectError: false,
		},
		{
			name: "strict: false in frontmatter does not enable strict mode",
			content: `---
on: push
strict: false
permissions:
  contents: write
  issues: read
  pull-requests: read
engine: copilot
features:
  dangerous-permissions-write: true
---

# Test Workflow`,
			expectError: false,
		},
		{
			name: "strict: true with valid configuration passes",
			content: `---
on: push
strict: true
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
			name: "no strict field defaults to strict mode",
			content: `---
on: push
permissions:
  contents: write
  issues: read
  pull-requests: read
engine: copilot
features:
  dangerous-permissions-write: true
---

# Test Workflow`,
			expectError: true,
			errorMsg:    "strict mode: write permission",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "frontmatter-strict-test")

			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()
			// Do NOT set strict mode via CLI - let frontmatter control it
			err := compiler.CompileWorkflow(testFile)

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

func TestCLIStrictFlagTakesPrecedence(t *testing.T) {
	// CLI --strict flag should override frontmatter strict: false
	content := `---
on: push
strict: false
permissions:
  contents: write
  issues: read
  pull-requests: read
engine: copilot
features:
  dangerous-permissions-write: true
---

# Test Workflow`

	tmpDir := testutil.TempDir(t, "cli-precedence-test")

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	compiler.SetStrictMode(true) // CLI flag sets strict mode
	err := compiler.CompileWorkflow(testFile)

	// Should fail because CLI flag enforces strict mode and write permission is not allowed
	if err == nil {
		t.Error("Expected compilation to fail with CLI --strict flag, but it succeeded")
	} else if !strings.Contains(err.Error(), "write permission") {
		t.Errorf("Expected write permission error, got: %v", err)
	}
}

func TestStrictModeIsolation(t *testing.T) {
	// Test that strict mode in one workflow doesn't affect other workflows
	tmpDir := testutil.TempDir(t, "strict-isolation-test")

	// First workflow with strict: true (should succeed now without timeout)
	strictWorkflow := `---
on: push
strict: true
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
network:
  allowed:
    - "api.example.com"
---

# Strict Workflow`

	// Second workflow without strict mode (no timeout_minutes - should succeed)
	nonStrictWorkflow := `---
on: push
permissions:
  contents: write
  issues: read
  pull-requests: read
engine: copilot
features:
  dangerous-permissions-write: true
strict: false
---

# Non-Strict Workflow`

	strictFile := filepath.Join(tmpDir, "strict-workflow.md")
	nonStrictFile := filepath.Join(tmpDir, "non-strict-workflow.md")

	if err := os.WriteFile(strictFile, []byte(strictWorkflow), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nonStrictFile, []byte(nonStrictWorkflow), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	// Do NOT set strict mode via CLI - let frontmatter control it

	// Compile strict workflow first - should succeed now
	if err := compiler.CompileWorkflow(strictFile); err != nil {
		t.Errorf("Expected strict workflow to succeed, but it failed: %v", err)
	}

	// Compile non-strict workflow second - should also succeed
	// This tests that strict mode from first workflow doesn't leak
	if err := compiler.CompileWorkflow(nonStrictFile); err != nil {
		t.Errorf("Expected non-strict workflow to succeed, but it failed: %v", err)
	}
}

func TestStrictModeAllowsGitHubWorkflowExpression(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expectError bool
		errorMsg    string
	}{
		{
			name: "github.workflow expression allowed in strict mode",
			content: `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
network:
  allowed:
    - "api.example.com"
---

# Test Workflow with github.workflow

This workflow uses ${{ github.workflow }} expression in the content.
The workflow name is: ${{ github.workflow }}`,
			expectError: false,
		},
		{
			name: "github.workflow expression in complex condition",
			content: `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
network:
  allowed:
    - "api.example.com"
---

# Complex Expression Test

Using github.workflow in a condition: ${{ github.workflow == 'my-workflow' && github.repository == 'owner/repo' }}`,
			expectError: false,
		},
		{
			name: "github.workflow with other allowed expressions",
			content: `---
on: issues
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
network:
  allowed:
    - "api.example.com"
---

# Multiple Expressions

- Workflow: ${{ github.workflow }}
- Repository: ${{ github.repository }}
- Issue: ${{ github.event.issue.number }}
- Actor: ${{ github.actor }}`,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "strict-github-workflow-test")

			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()
			compiler.SetStrictMode(true)
			err := compiler.CompileWorkflow(testFile)

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
