//go:build integration

package workflow

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPermissionsWithoutGitHubTool tests that when permissions are specified
// but tools.github is NOT configured, no warning is raised and the GitHub MCP
// server will handle permission issues
func TestPermissionsWithoutGitHubTool(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		expectError    bool
		expectWarning  bool
		warningMessage string
	}{
		{
			name: "permissions without github tool - no warning",
			content: `---
on: push
permissions:
  contents: read
  issues: read
---

# Test Workflow
`,
			expectError:   false,
			expectWarning: false,
		},
		{
			name: "permissions with github tool - validates permissions",
			content: `---
on: push
permissions:
  contents: read
tools:
  github:
    toolsets: [repos, issues]
    read-only: false
---

# Test Workflow
`,
			expectError:    false,
			expectWarning:  true,
			warningMessage: "Missing required permissions for github toolsets:",
		},
		{
			name: "no permissions, no github tool - no warning",
			content: `---
on: push
---

# Test Workflow
`,
			expectError:   false,
			expectWarning: false,
		},
		{
			name: "permissions with sufficient github tool config - no warning",
			content: `---
on: push
permissions:
  contents: write
  issues: write
strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    toolsets: [repos, issues]
---

# Test Workflow
`,
			expectError:   false,
			expectWarning: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "permissions-no-github-tool-test")

			testFile := filepath.Join(tmpDir, "test-workflow.md")
			err := os.WriteFile(testFile, []byte(tt.content), 0644)
			require.NoError(t, err, "Failed to write test file")

			// Capture stderr to check for warnings
			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			compiler := NewCompiler()
			compiler.SetStrictMode(false)
			err = compiler.CompileWorkflow(testFile)

			// Restore stderr
			w.Close()
			os.Stderr = oldStderr
			var buf bytes.Buffer
			io.Copy(&buf, r)
			stderrOutput := buf.String()

			// Check error expectation
			if tt.expectError {
				assert.Error(t, err, "Expected compilation to fail")
			} else {
				assert.NoError(t, err, "Expected compilation to succeed")
			}

			// Check warning expectation
			if tt.expectWarning {
				assert.Contains(t, stderrOutput, tt.warningMessage, "Expected warning message not found")
				assert.Contains(t, stderrOutput, "warning:", "Expected 'warning:' prefix in output")
			} else {
				// For non-warning cases, we should not see permission-related warnings
				if tt.warningMessage != "" {
					assert.NotContains(t, stderrOutput, tt.warningMessage, "Unexpected warning in output")
				}
				assert.NotContains(t, stderrOutput, "Missing required permissions", "Unexpected permission warning")
			}
		})
	}
}

// TestPermissionsWithoutGitHubToolStrictMode tests that in strict mode,
// permissions without github tool still doesn't raise validation errors
func TestPermissionsWithoutGitHubToolStrictMode(t *testing.T) {
	tmpDir := testutil.TempDir(t, "permissions-no-github-tool-strict-test")

	content := `---
on: push
strict: true
permissions:
  contents: read
  issues: read
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	err := os.WriteFile(testFile, []byte(content), 0644)
	require.NoError(t, err, "Failed to write test file")

	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	compiler := NewCompiler()
	compiler.SetStrictMode(true)
	err = compiler.CompileWorkflow(testFile)

	// Restore stderr
	w.Close()
	os.Stderr = oldStderr
	var buf bytes.Buffer
	io.Copy(&buf, r)
	stderrOutput := buf.String()

	// Should succeed without permission validation errors
	assert.NoError(t, err, "Expected compilation to succeed in strict mode")
	assert.NotContains(t, stderrOutput, "Missing required permissions", "Should not raise permission warnings")
}

// TestPermissionsWarningOnlyWithGitHubTool ensures that permission validation
// warnings are only raised when tools.github is explicitly configured
func TestPermissionsWarningOnlyWithGitHubTool(t *testing.T) {
	tmpDir := testutil.TempDir(t, "permissions-warning-only-with-github-test")

	tests := []struct {
		name          string
		hasGitHubTool bool
		expectWarning bool
	}{
		{
			name:          "no github tool - no warning",
			hasGitHubTool: false,
			expectWarning: false,
		},
		{
			name:          "with github tool - warning expected",
			hasGitHubTool: true,
			expectWarning: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var content string
			if tt.hasGitHubTool {
				content = `---
on: push
permissions:
  contents: read
tools:
  github:
    toolsets: [repos, issues]
    read-only: false
---

# Test Workflow
`
			} else {
				content = `---
on: push
permissions:
  contents: read
---

# Test Workflow
`
			}

			testFile := filepath.Join(tmpDir, tt.name+"-workflow.md")
			err := os.WriteFile(testFile, []byte(content), 0644)
			require.NoError(t, err, "Failed to write test file")

			// Capture stderr
			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			compiler := NewCompiler()
			compiler.SetStrictMode(false)
			err = compiler.CompileWorkflow(testFile)

			// Restore stderr
			w.Close()
			os.Stderr = oldStderr
			var buf bytes.Buffer
			io.Copy(&buf, r)
			stderrOutput := buf.String()

			assert.NoError(t, err, "Expected compilation to succeed")

			if tt.expectWarning {
				assert.Contains(t, stderrOutput, "Missing required permissions", "Expected permission warning")
			} else {
				assert.NotContains(t, stderrOutput, "Missing required permissions", "Should not have permission warning")
			}
		})
	}
}
