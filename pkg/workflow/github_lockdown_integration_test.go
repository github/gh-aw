//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
)

func TestGitHubLockdownIntegration(t *testing.T) {
	tests := []struct {
		name        string
		workflow    string
		engine      string
		expected    []string
		notExpected []string
		description string
	}{
		{
			name:   "copilot engine with lockdown enabled in local mode",
			engine: "copilot",
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

Test lockdown mode with local GitHub MCP.
`,
			expected: []string{
				`"type": "stdio"`,
				`"GITHUB_LOCKDOWN_MODE": "1"`,
				`"ghcr.io/github/github-mcp-server:`,
			},
			notExpected: []string{},
			description: "Copilot with local mode and lockdown should render GITHUB_LOCKDOWN_MODE=1",
		},
		{
			name:   "copilot engine with lockdown enabled in remote mode",
			engine: "copilot",
			workflow: `---
on: issues
engine: copilot
tools:
  github:
    mode: remote
    lockdown: true
    toolsets: [default]
---

# Test Workflow

Test lockdown mode with remote GitHub MCP.
`,
			expected: []string{
				`"type": "http"`,
				`"X-MCP-Lockdown": "true"`,
				`"Authorization":`,
			},
			notExpected: []string{
				`"GITHUB_LOCKDOWN_MODE": "1"`,
			},
			description: "Copilot with remote mode and lockdown should render X-MCP-Lockdown header",
		},
		{
			name:   "claude engine with lockdown enabled",
			engine: "claude",
			workflow: `---
on: issues
engine: claude
tools:
  github:
    mode: local
    lockdown: true
    toolsets: [default]
---

# Test Workflow

Test lockdown mode with Claude engine.
`,
			expected: []string{
				`"GITHUB_LOCKDOWN_MODE": "1"`,
				`"ghcr.io/github/github-mcp-server:`,
			},
			notExpected: []string{
				`"type": "stdio"`, // Claude doesn't include type field
			},
			description: "Claude with lockdown should render GITHUB_LOCKDOWN_MODE=1",
		},
		{
			name:   "codex engine with lockdown enabled",
			engine: "codex",
			workflow: `---
on: issues
engine: codex
tools:
  github:
    mode: local
    lockdown: true
    toolsets: [default]
---

# Test Workflow

Test lockdown mode with Codex engine.
`,
			expected: []string{
				`"GITHUB_LOCKDOWN_MODE" = "1"`,
				`ghcr.io/github/github-mcp-server:`,
			},
			notExpected: []string{},
			description: "Codex (TOML) with lockdown should render GITHUB_LOCKDOWN_MODE=1",
		},
		{
			name:   "lockdown with read-only both enabled",
			engine: "copilot",
			workflow: `---
on: issues
engine: copilot
tools:
  github:
    mode: local
    lockdown: true
    read-only: true
    toolsets: [repos]
---

# Test Workflow

Test lockdown and read-only modes together.
`,
			expected: []string{
				`"GITHUB_LOCKDOWN_MODE": "1"`,
				`"GITHUB_READ_ONLY": "1"`,
			},
			notExpected: []string{},
			description: "Both lockdown and read-only can be enabled together",
		},
		{
			name:   "default workflow without lockdown",
			engine: "copilot",
			workflow: `---
on: issues
engine: copilot
tools:
  github:
    mode: local
    toolsets: [default]
---

# Test Workflow

Test default behavior without lockdown.
`,
			expected: []string{
				`"ghcr.io/github/github-mcp-server:`,
			},
			notExpected: []string{
				`"GITHUB_LOCKDOWN_MODE": "1"`,
				`"X-MCP-Lockdown"`,
			},
			description: "Without lockdown field, no lockdown mode should be rendered",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test
			tmpDir, err := os.MkdirTemp("", "lockdown-test-*")
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

			// Check expected strings
			for _, expected := range tt.expected {
				if !strings.Contains(yaml, expected) {
					t.Errorf("%s: Expected output to contain %q, but it doesn't", tt.description, expected)
				}
			}

			// Check strings that should NOT be present
			for _, notExpected := range tt.notExpected {
				if strings.Contains(yaml, notExpected) {
					t.Errorf("%s: Expected output NOT to contain %q, but it does", tt.description, notExpected)
				}
			}
		})
	}
}
