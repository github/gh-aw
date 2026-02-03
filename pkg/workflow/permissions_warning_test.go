//go:build integration

package workflow

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestPermissionsWarningInNonStrictMode tests that under-provisioned permissions
// produce warnings in non-strict mode rather than errors
func TestPermissionsWarningInNonStrictMode(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		strict         bool
		expectError    bool
		expectWarning  bool
		warningMessage string
	}{
		{
			name: "missing permissions in non-strict mode produces warning",
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
			strict:         false,
			expectError:    false,
			expectWarning:  true,
			warningMessage: "Missing required permissions for github toolsets:",
		},
		{
			name: "missing permissions in strict mode produces error",
			content: `---
on: push
permissions:
  contents: read
engine: copilot
strict: false
network:
  allowed:
    - "api.example.com"
tools:
  github:
    toolsets: [repos, issues]
    read-only: false
---

# Test Workflow
`,
			strict:         true,
			expectError:    true,
			expectWarning:  false,
			warningMessage: "",
		},
		{
			name: "sufficient permissions in non-strict mode produces no warning",
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
			strict:        false,
			expectError:   false,
			expectWarning: false,
		},
		{
			name: "sufficient permissions in strict mode produces no error",
			content: `---
on: push
permissions:
  contents: read
  issues: read
engine: copilot
strict: false
network:
  allowed:
    - "api.example.com"
tools:
  github:
    toolsets: [repos, issues]
    read-only: true
---

# Test Workflow
`,
			strict:        true,
			expectError:   false,
			expectWarning: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "permissions-warning-test")

			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			// Capture stderr to check for warnings
			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			compiler := NewCompiler()
			compiler.SetStrictMode(tt.strict)
			err := compiler.CompileWorkflow(testFile)

			// Restore stderr
			w.Close()
			os.Stderr = oldStderr
			var buf bytes.Buffer
			io.Copy(&buf, r)
			stderrOutput := buf.String()

			// Check error expectation
			if tt.expectError && err == nil {
				t.Error("Expected compilation to fail but it succeeded")
			} else if !tt.expectError && err != nil {
				t.Errorf("Expected compilation to succeed but it failed: %v", err)
			}

			// Check warning expectation
			if tt.expectWarning {
				if !strings.Contains(stderrOutput, tt.warningMessage) {
					t.Errorf("Expected warning containing '%s', got stderr:\n%s", tt.warningMessage, stderrOutput)
				}
				if !strings.Contains(stderrOutput, "warning:") {
					t.Errorf("Expected 'warning:' in stderr output, got:\n%s", stderrOutput)
				}
				// Check for the new suggestion format
				if !strings.Contains(stderrOutput, "Option 1: Add missing permissions") {
					t.Errorf("Expected 'Option 1: Add missing permissions' in warning, got:\n%s", stderrOutput)
				}
				if !strings.Contains(stderrOutput, "Option 2: Reduce the required toolsets") {
					t.Errorf("Expected 'Option 2: Reduce the required toolsets' in warning, got:\n%s", stderrOutput)
				}
			} else {
				// For non-warning cases, we should not see the warning message content
				if tt.warningMessage != "" && strings.Contains(stderrOutput, tt.warningMessage) {
					t.Errorf("Unexpected warning in stderr output:\n%s", stderrOutput)
				}
			}

			// Verify warning count
			if tt.expectWarning {
				warningCount := compiler.GetWarningCount()
				if warningCount == 0 {
					t.Error("Expected warning count > 0 but got 0")
				}
			}
		})
	}
}

// TestPermissionsWarningMessageFormat tests that the warning message format
// includes both options for fixing the issue
func TestPermissionsWarningMessageFormat(t *testing.T) {
	tmpDir := testutil.TempDir(t, "permissions-warning-format-test")

	content := `---
on: push
permissions:
  contents: read
tools:
  github:
    toolsets: [repos, issues, pull_requests]
    read-only: false
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Capture stderr to check for warnings
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	compiler := NewCompiler()
	compiler.SetStrictMode(false)
	err := compiler.CompileWorkflow(testFile)

	// Restore stderr
	w.Close()
	os.Stderr = oldStderr
	var buf bytes.Buffer
	io.Copy(&buf, r)
	stderrOutput := buf.String()

	if err != nil {
		t.Fatalf("Expected compilation to succeed but it failed: %v", err)
	}

	// Check that the warning includes both options
	expectedPhrases := []string{
		"Missing required permissions for github toolsets:",
		"Option 1: Add missing permissions to your workflow frontmatter:",
		"Option 2: Reduce the required toolsets in your workflow:",
		"issues",
		"pull_requests",
		"repos",
		"contents: write",
		"issues: write",
		"pull-requests: write",
	}

	for _, phrase := range expectedPhrases {
		if !strings.Contains(stderrOutput, phrase) {
			t.Errorf("Expected warning to contain '%s', got:\n%s", phrase, stderrOutput)
		}
	}
}
