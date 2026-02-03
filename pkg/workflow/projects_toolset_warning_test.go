//go:build integration

package workflow

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectsToolsetWarning(t *testing.T) {
	tests := []struct {
		name            string
		workflowMD      string
		shouldWarn      bool
		warningContains string
	}{
		{
			name: "Explicit projects toolset should warn",
			workflowMD: `---
on: push
engine: copilot
tools:
  github:
    toolsets: [projects]
---

# Test Workflow

This workflow explicitly uses projects toolset.
`,
			shouldWarn:      true,
			warningContains: "The 'projects' toolset requires a GitHub token",
		},
		{
			name: "Explicit projects with other toolsets should warn",
			workflowMD: `---
on: push
engine: copilot
tools:
  github:
    toolsets: [repos, issues, projects]
---

# Test Workflow

This workflow uses projects among other toolsets.
`,
			shouldWarn:      true,
			warningContains: "The 'projects' toolset requires a GitHub token",
		},
		{
			name: "All toolset should NOT warn about projects",
			workflowMD: `---
on: push
engine: copilot
tools:
  github:
    toolsets: [all]
---

# Test Workflow

This workflow uses all toolsets (implicit projects).
`,
			shouldWarn: false,
		},
		{
			name: "Default toolsets should NOT warn",
			workflowMD: `---
on: push
engine: copilot
tools:
  github:
    toolsets: [default]
---

# Test Workflow

This workflow uses default toolsets.
`,
			shouldWarn: false,
		},
		{
			name: "No projects toolset should NOT warn",
			workflowMD: `---
on: push
engine: copilot
tools:
  github:
    toolsets: [repos, issues, pull_requests]
---

# Test Workflow

This workflow does not use projects toolset.
`,
			shouldWarn: false,
		},
		{
			name: "All with other toolsets should NOT warn",
			workflowMD: `---
on: push
engine: copilot
tools:
  github:
    toolsets: [all, repos]
---

# Test Workflow

This workflow uses all with redundant repos.
`,
			shouldWarn: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test
			tempDir := testutil.TempDir(t, "test-*")
			mdPath := filepath.Join(tempDir, "test-workflow.md")

			// Write workflow file
			err := os.WriteFile(mdPath, []byte(tt.workflowMD), 0644)
			require.NoError(t, err, "Failed to write test workflow")

			// Capture stderr
			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			// Compile the workflow
			compiler := NewCompiler()
			compileErr := compiler.CompileWorkflow(mdPath)
			require.NoError(t, compileErr, "Failed to compile workflow")

			// Restore stderr and read captured output
			w.Close()
			os.Stderr = oldStderr
			var buf bytes.Buffer
			io.Copy(&buf, r)
			stderrOutput := buf.String()

			// Read the generated YAML to verify it was created successfully
			yamlPath := stringutil.MarkdownToLockFile(mdPath)
			_, err = os.ReadFile(yamlPath)
			require.NoError(t, err, "Failed to read generated YAML")

			// Check if warning was shown
			hasWarning := strings.Contains(stderrOutput, "projects' toolset requires")

			if tt.shouldWarn {
				assert.True(t, hasWarning, "Expected warning about projects toolset, but none found.\nStderr output:\n%s", stderrOutput)
				if tt.warningContains != "" {
					assert.Contains(t, stderrOutput, tt.warningContains, "Warning message should contain expected text")
				}
			} else {
				assert.False(t, hasWarning, "Expected NO warning about projects toolset, but found one.\nStderr output:\n%s", stderrOutput)
			}
		})
	}
}
