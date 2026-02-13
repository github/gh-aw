//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
)

// TestGitHubLockdownExplicitOnly verifies that lockdown mode is explicit only
// and no automatic detection steps are generated
func TestGitHubLockdownExplicitOnly(t *testing.T) {
	tests := []struct {
		name               string
		workflow           string
		expectedLockdown   string // "true" means hardcoded true, "false" means not present, "none" means no lockdown setting at all
		description        string
	}{
		{
			name: "No lockdown when not specified",
			workflow: `---
on: issues
engine: copilot
tools:
  github:
    mode: local
    toolsets: [default]
---

# Test Workflow

Test that lockdown is not enabled without explicit setting.
`,
			expectedLockdown: "none",
			description:      "When lockdown is not specified, no lockdown setting should be present",
		},
		{
			name: "Lockdown enabled when explicitly set to true",
			workflow: `---
on: issues
engine: copilot
tools:
  github:
    mode: local
    lockdown: true
    toolsets: [default]
---

# Test Workflow

Test with explicit lockdown enabled.
`,
			expectedLockdown: "true",
			description:      "When lockdown is explicitly true, lockdown should be hardcoded",
		},
		{
			name: "No lockdown when explicitly set to false",
			workflow: `---
on: issues
engine: copilot
tools:
  github:
    mode: local
    lockdown: false
    toolsets: [default]
---

# Test Workflow

Test with explicit lockdown disabled.
`,
			expectedLockdown: "none",
			description:      "When lockdown is explicitly false, no lockdown setting should be present",
		},
		{
			name: "Auto-determination with remote mode",
			workflow: `---
on: issues
engine: copilot
tools:
  github:
    mode: remote
    toolsets: [default]
---

# Test Workflow

Test auto-determination with remote GitHub MCP.
`,
			expectedDetectStep: true,
			expectedLockdown:   "auto",
			expectIfCondition:  false,
			description:        "Auto-determination should work with remote mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test
			tmpDir, err := os.MkdirTemp("", "lockdown-autodetect-test-*")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			// Write workflow file
			workflowPath := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(workflowPath, []byte(tt.workflow), 0644); err != nil {
				t.Fatalf("Failed to write workflow file: %v", err)
			}

			// Compile workflow
			compiler := NewCompiler()
			if err := compiler.CompileWorkflow(workflowPath); err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			// Read the generated lock file
			lockPath := stringutil.MarkdownToLockFile(workflowPath)
			lockContent, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}
			yaml := string(lockContent)

			// Check if detection step is present
			detectStepPresent := strings.Contains(yaml, "Determine automatic lockdown mode for GitHub MCP server") &&
				strings.Contains(yaml, "determine-automatic-lockdown") &&
				strings.Contains(yaml, "determine_automatic_lockdown.cjs")

			if detectStepPresent != tt.expectedDetectStep {
				t.Errorf("%s: Detection step presence = %v, want %v", tt.description, detectStepPresent, tt.expectedDetectStep)
			}

			// Check lockdown configuration based on expected value
			switch tt.expectedLockdown {
			case "auto":
				// Should use step output expression
				if !strings.Contains(yaml, "steps.determine-automatic-lockdown.outputs.lockdown") {
					t.Errorf("%s: Expected lockdown to use step output expression", tt.description)
				}
			case "true":
				// Should have hardcoded GITHUB_LOCKDOWN_MODE=1 or X-MCP-Lockdown: true
				hasDockerLockdown := strings.Contains(yaml, `"GITHUB_LOCKDOWN_MODE": "1"`)
				hasRemoteLockdown := strings.Contains(yaml, "X-MCP-Lockdown") && strings.Contains(yaml, "\"true\"")
				if !hasDockerLockdown && !hasRemoteLockdown {
					t.Errorf("%s: Expected hardcoded lockdown setting", tt.description)
				}
			case "false":
				// Should not have GITHUB_LOCKDOWN_MODE or X-MCP-Lockdown
				if strings.Contains(yaml, "GITHUB_LOCKDOWN_MODE") || strings.Contains(yaml, "X-MCP-Lockdown") {
					t.Errorf("%s: Expected no lockdown setting", tt.description)
				}
			}
		})
	}
}

func TestGitHubLockdownAutodetectionClaudeEngine(t *testing.T) {
	workflow := `---
on: issues
engine: claude
tools:
  github:
    mode: local
    toolsets: [default]
---

# Test Workflow

Test automatic lockdown determination with Claude.
`

	// Create temporary directory for test
	tmpDir, err := os.MkdirTemp("", "lockdown-autodetect-claude-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Write workflow file
	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflow), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile workflow
	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	yaml := string(lockContent)

	// Check if detection step is present
	detectStepPresent := strings.Contains(yaml, "Determine automatic lockdown mode for GitHub MCP server") &&
		strings.Contains(yaml, "determine-automatic-lockdown")

	if !detectStepPresent {
		t.Error("Determination step should be present for Claude engine")
	}

	// Check if lockdown uses step output expression
	if !strings.Contains(yaml, "steps.determine-automatic-lockdown.outputs.lockdown") {
		t.Error("Expected lockdown to use step output expression for Claude engine")
	}
}
