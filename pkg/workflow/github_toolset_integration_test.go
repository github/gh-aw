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

func TestGitHubToolsetIntegration(t *testing.T) {
	tests := []struct {
		name           string
		workflowMD     string
		expectedInYAML []string
		notInYAML      []string
	}{
		{
			name: "Claude engine with toolsets",
			workflowMD: `---
on: push
engine: claude
tools:
  github:
    toolsets: [repos, issues, pull_requests]
---

# Test Workflow

This workflow tests GitHub toolsets.
`,
			expectedInYAML: []string{
				`GITHUB_TOOLSETS`,
				`repos,issues,pull_requests`,
			},
			notInYAML: []string{},
		},
		{
			name: "Copilot engine with array toolsets",
			workflowMD: `---
on: push
engine: copilot
tools:
  github:
    toolsets: [repos, issues, actions]
---

# Test Workflow

This workflow tests GitHub toolsets as array.
`,
			expectedInYAML: []string{
				`GITHUB_TOOLSETS`,
				`repos,issues,actions`,
			},
			notInYAML: []string{},
		},
		{
			name: "Codex engine with all toolset",
			workflowMD: `---
on: push
engine: codex
tools:
  github:
    toolsets: [all]
---

# Test Workflow

This workflow enables all GitHub toolsets.
`,
			expectedInYAML: []string{
				`GITHUB_TOOLSETS`,
				`all`,
			},
			notInYAML: []string{},
		},
		{
			name: "Workflow without toolsets",
			workflowMD: `---
on: push
engine: claude
tools:
  github:
---

# Test Workflow

This workflow has no toolsets configured.
`,
			expectedInYAML: []string{
				`GITHUB_PERSONAL_ACCESS_TOKEN`,
				`GITHUB_TOOLSETS`,
			},
		},
		{
			name: "Toolsets with read-only mode",
			workflowMD: `---
on: push
engine: claude
tools:
  github:
    toolsets: [repos, issues]
    read-only: true
---

# Test Workflow

This workflow combines toolsets with read-only mode.
`,
			expectedInYAML: []string{
				`GITHUB_TOOLSETS`,
				`repos,issues`,
				`GITHUB_READ_ONLY`,
			},
			notInYAML: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test
			tempDir := testutil.TempDir(t, "test-*")
			mdPath := filepath.Join(tempDir, "test-workflow.md")

			// Write workflow file
			err := os.WriteFile(mdPath, []byte(tt.workflowMD), 0644)
			if err != nil {
				t.Fatalf("Failed to write test workflow: %v", err)
			}

			// Compile the workflow
			compiler := NewCompiler()
			compileErr := compiler.CompileWorkflow(mdPath)
			if compileErr != nil {
				t.Fatalf("Failed to compile workflow: %v", compileErr)
			}

			// Read the generated YAML (same directory, .lock.yml extension)
			yamlPath := stringutil.MarkdownToLockFile(mdPath)
			yamlContent, err := os.ReadFile(yamlPath)
			if err != nil {
				t.Fatalf("Failed to read generated YAML: %v", err)
			}

			yamlStr := string(yamlContent)

			// Check expected strings
			for _, expected := range tt.expectedInYAML {
				if !strings.Contains(yamlStr, expected) {
					t.Errorf("Expected YAML to contain %q, but it didn't.\nGenerated YAML:\n%s", expected, yamlStr)
				}
			}

			// Check strings that should not be present
			for _, notExpected := range tt.notInYAML {
				if strings.Contains(yamlStr, notExpected) {
					t.Errorf("Expected YAML to NOT contain %q, but it did.\nGenerated YAML:\n%s", notExpected, yamlStr)
				}
			}
		})
	}
}

func TestGitHubToolsetRemoteMode(t *testing.T) {
	workflowMD := `---
on: push
engine: claude
tools:
  github:
    mode: remote
    toolsets: [repos, issues]
---

# Test Workflow

This workflow tests remote mode with array toolsets.
`

	// Create temporary directory for test
	tempDir := testutil.TempDir(t, "test-*")
	mdPath := filepath.Join(tempDir, "test-workflow.md")

	// Write workflow file
	err := os.WriteFile(mdPath, []byte(workflowMD), 0644)
	if err != nil {
		t.Fatalf("Failed to write test workflow: %v", err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	compileErr := compiler.CompileWorkflow(mdPath)
	if compileErr != nil {
		t.Fatalf("Failed to compile workflow: %v", compileErr)
	}

	// Read the generated YAML (same directory, .lock.yml extension)
	yamlPath := stringutil.MarkdownToLockFile(mdPath)
	yamlContent, readErr := os.ReadFile(yamlPath)
	if readErr != nil {
		t.Fatalf("Failed to read generated YAML: %v", readErr)
	}

	yamlStr := string(yamlContent)

	// In remote mode, toolsets should be passed via X-MCP-Toolsets header
	if !strings.Contains(yamlStr, "https://api.githubcopilot.com/mcp/") {
		t.Errorf("Expected remote mode URL in YAML")
	}

	// Check for X-MCP-Toolsets header with the configured toolsets
	if !strings.Contains(yamlStr, `"X-MCP-Toolsets": "repos,issues"`) {
		t.Errorf("Expected X-MCP-Toolsets header with 'repos,issues' in remote mode, but didn't find it.\nGenerated YAML:\n%s", yamlStr)
	}
}

func TestGitHubToolsetRemoteModeMultipleEngines(t *testing.T) {
	tests := []struct {
		name           string
		workflowMD     string
		expectedHeader string
		engineType     string
	}{
		{
			name: "Claude remote mode with toolsets",
			workflowMD: `---
on: push
engine: claude
tools:
  github:
    mode: remote
    toolsets: [repos, issues, pull_requests]
---

# Test Workflow

Claude remote mode with toolsets.
`,
			expectedHeader: `"X-MCP-Toolsets": "repos,issues,pull_requests"`,
			engineType:     "claude",
		},
		{
			name: "Copilot remote mode with toolsets",
			workflowMD: `---
on: push
engine: copilot
tools:
  github:
    mode: remote
    toolsets: [actions, discussions]
---

# Test Workflow

Copilot remote mode with toolsets.
`,
			expectedHeader: `"X-MCP-Toolsets": "actions,discussions"`,
			engineType:     "copilot",
		},
		{
			name: "Remote mode with all toolsets",
			workflowMD: `---
on: push
engine: claude
tools:
  github:
    mode: remote
    toolsets: [all]
---

# Test Workflow

Remote mode with all toolsets.
`,
			expectedHeader: `"X-MCP-Toolsets": "all"`,
			engineType:     "claude",
		},
		{
			name: "Remote mode without toolsets",
			workflowMD: `---
on: push
engine: claude
tools:
  github:
    mode: remote
---

# Test Workflow

Remote mode without toolsets.
`,
			expectedHeader: `"X-MCP-Toolsets": "context,repos,issues,pull_requests"`, // Defaults to action-friendly toolsets
			engineType:     "claude",
		},
		{
			name: "Remote mode with toolsets and read-only",
			workflowMD: `---
on: push
engine: copilot
tools:
  github:
    mode: remote
    toolsets: [repos, issues]
    read-only: true
---

# Test Workflow

Remote mode with toolsets and read-only.
`,
			expectedHeader: `"X-MCP-Toolsets": "repos,issues"`,
			engineType:     "copilot",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test
			tempDir := testutil.TempDir(t, "test-*")
			mdPath := filepath.Join(tempDir, "test-workflow.md")

			// Write workflow file
			err := os.WriteFile(mdPath, []byte(tt.workflowMD), 0644)
			if err != nil {
				t.Fatalf("Failed to write test workflow: %v", err)
			}

			// Compile the workflow
			compiler := NewCompiler()
			compileErr := compiler.CompileWorkflow(mdPath)
			if compileErr != nil {
				t.Fatalf("Failed to compile workflow: %v", compileErr)
			}

			// Read the generated YAML
			yamlPath := stringutil.MarkdownToLockFile(mdPath)
			yamlContent, readErr := os.ReadFile(yamlPath)
			if readErr != nil {
				t.Fatalf("Failed to read generated YAML: %v", readErr)
			}

			yamlStr := string(yamlContent)

			// Verify remote mode URL
			if !strings.Contains(yamlStr, "https://api.githubcopilot.com/mcp/") {
				t.Errorf("Expected remote mode URL in YAML")
			}

			// Check for expected header
			if tt.expectedHeader != "" {
				if !strings.Contains(yamlStr, tt.expectedHeader) {
					t.Errorf("Expected header %q in YAML but didn't find it.\nGenerated YAML:\n%s", tt.expectedHeader, yamlStr)
				}
			} else {
				// Verify no X-MCP-Toolsets header is present
				if strings.Contains(yamlStr, "X-MCP-Toolsets") {
					t.Errorf("Expected no X-MCP-Toolsets header in YAML but found one.\nGenerated YAML:\n%s", yamlStr)
				}
			}

			// If read-only is in the test name, also verify X-MCP-Readonly header
			if strings.Contains(tt.name, "read-only") {
				if !strings.Contains(yamlStr, `"X-MCP-Readonly": "true"`) {
					t.Errorf("Expected X-MCP-Readonly header in YAML but didn't find it.\nGenerated YAML:\n%s", yamlStr)
				}
			}
		})
	}
}
