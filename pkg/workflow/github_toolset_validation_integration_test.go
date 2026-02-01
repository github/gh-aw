//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitHubToolsetValidationIntegration(t *testing.T) {
	tests := []struct {
		name          string
		frontmatter   string
		expectError   bool
		errorContains []string
	}{
		{
			name: "Valid configuration - tools match toolsets",
			frontmatter: `---
on: issues
engine: copilot
tools:
  github:
    toolsets:
      - repos
      - pull_requests
    allowed:
      - get_file_contents
      - search_pull_requests
---

# Test workflow
Test content.
`,
			expectError: false,
		},
		{
			name: "Valid configuration - default toolsets include required toolsets",
			frontmatter: `---
on: issues
engine: copilot
tools:
  github:
    toolsets:
      - default
    allowed:
      - get_file_contents
      - list_issues
---

# Test workflow
Test content.
`,
			expectError: false,
		},
		{
			name: "Valid configuration - all toolset enables everything",
			frontmatter: `---
on: issues
engine: copilot
tools:
  github:
    toolsets:
      - all
    allowed:
      - get_file_contents
      - list_workflows
      - create_gist
---

# Test workflow
Test content.
`,
			expectError: false,
		},
		{
			name: "Invalid configuration - missing toolset for allowed tool",
			frontmatter: `---
on: issues
engine: copilot
tools:
  github:
    toolsets:
      - repos
    allowed:
      - get_file_contents
      - list_issues
---

# Test workflow
Test content.
`,
			expectError:   true,
			errorContains: []string{"issues", "list_issues", "Toolset 'issues' is required by"},
		},
		{
			name: "Invalid configuration - multiple missing toolsets",
			frontmatter: `---
on: issues
engine: copilot
tools:
  github:
    toolsets:
      - repos
    allowed:
      - get_file_contents
      - list_issues
      - list_workflows
      - search_pull_requests
---

# Test workflow
Test content.
`,
			expectError: true,
			errorContains: []string{
				"issues",
				"actions",
				"pull_requests",
				"list_issues",
				"list_workflows",
				"search_pull_requests",
			},
		},
		{
			name: "Valid configuration - no allowed field means no validation",
			frontmatter: `---
on: issues
engine: copilot
tools:
  github:
    toolsets:
      - repos
---

# Test workflow
Test content.
`,
			expectError: false,
		},
		{
			name: "Valid configuration - empty allowed array means no validation",
			frontmatter: `---
on: issues
engine: copilot
tools:
  github:
    toolsets:
      - repos
    allowed: []
---

# Test workflow
Test content.
`,
			expectError: false,
		},
		{
			name: "Invalid configuration - actions toolset missing",
			frontmatter: `---
on: workflow_dispatch
engine: copilot
tools:
  github:
    toolsets:
      - repos
      - issues
    allowed:
      - list_workflows
      - get_workflow_run
      - download_workflow_run_artifact
---

# Test workflow
Test content.
`,
			expectError: true,
			errorContains: []string{
				"actions",
				"list_workflows",
				"get_workflow_run",
				"download_workflow_run_artifact",
			},
		},
		{
			name: "Valid configuration - default plus additional toolset",
			frontmatter: `---
on: issues
engine: copilot
tools:
  github:
    toolsets:
      - default
      - actions
    allowed:
      - get_file_contents
      - list_issues
      - list_workflows
---

# Test workflow
Test content.
`,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for the test
			tmpDir, err := os.MkdirTemp("", "github-toolset-validation-test-*")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			// Create the .github/workflows directory
			workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
			if err := os.MkdirAll(workflowsDir, 0755); err != nil {
				t.Fatalf("Failed to create workflows dir: %v", err)
			}

			// Write the test workflow
			workflowPath := filepath.Join(workflowsDir, "test-workflow.md")
			if err := os.WriteFile(workflowPath, []byte(tt.frontmatter), 0644); err != nil {
				t.Fatalf("Failed to write workflow file: %v", err)
			}

			// Create a compiler instance
			compiler := NewCompiler()

			// Try to compile the workflow
			err = compiler.CompileWorkflow(workflowPath)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected compilation error but got none")
					return
				}

				errMsg := err.Error()
				for _, expectedSubstr := range tt.errorContains {
					if !strings.Contains(errMsg, expectedSubstr) {
						t.Errorf("Expected error to contain %q, but it didn't.\nError: %s", expectedSubstr, errMsg)
					}
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

func TestGitHubToolsetValidationWithRemoteMode(t *testing.T) {
	frontmatter := `---
on: issues
engine: copilot
tools:
  github:
    mode: remote
    toolsets:
      - repos
    allowed:
      - get_file_contents
      - list_issues
---

# Test workflow
Test content.
`

	// Create a temporary directory for the test
	tmpDir, err := os.MkdirTemp("", "github-toolset-validation-remote-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create the .github/workflows directory
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows dir: %v", err)
	}

	// Write the test workflow
	workflowPath := filepath.Join(workflowsDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(frontmatter), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Create a compiler instance
	compiler := NewCompiler()

	// Try to compile the workflow
	err = compiler.CompileWorkflow(workflowPath)

	// Should fail because 'issues' toolset is missing
	if err == nil {
		t.Error("Expected compilation error but got none")
		return
	}

	errMsg := err.Error()
	expectedSubstrings := []string{"issues", "list_issues"}
	for _, expectedSubstr := range expectedSubstrings {
		if !strings.Contains(errMsg, expectedSubstr) {
			t.Errorf("Expected error to contain %q, but it didn't.\nError: %s", expectedSubstr, errMsg)
		}
	}
}

func TestGitHubToolsetValidationWithClaudeEngine(t *testing.T) {
	frontmatter := `---
on: issues
engine: claude
tools:
  github:
    toolsets:
      - repos
    allowed:
      - get_file_contents
      - create_discussion
---

# Test workflow
Test content.
`

	// Create a temporary directory for the test
	tmpDir, err := os.MkdirTemp("", "github-toolset-validation-claude-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create the .github/workflows directory
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows dir: %v", err)
	}

	// Write the test workflow
	workflowPath := filepath.Join(workflowsDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(frontmatter), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Create a compiler instance
	compiler := NewCompiler()

	// Try to compile the workflow
	err = compiler.CompileWorkflow(workflowPath)

	// Should fail because 'discussions' toolset is missing
	if err == nil {
		t.Error("Expected compilation error but got none")
		return
	}

	errMsg := err.Error()
	expectedSubstrings := []string{"discussions", "create_discussion"}
	for _, expectedSubstr := range expectedSubstrings {
		if !strings.Contains(errMsg, expectedSubstr) {
			t.Errorf("Expected error to contain %q, but it didn't.\nError: %s", expectedSubstr, errMsg)
		}
	}
}
