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

// TestIdTokenWriteWarning tests that id-token: write permission emits a warning
func TestIdTokenWriteWarning(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		expectWarning bool
	}{
		{
			name: "id-token write produces warning",
			content: `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
  id-token: write
---

# Test Workflow
`,
			expectWarning: true,
		},
		{
			name: "id-token read does not produce warning",
			content: `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
  id-token: read
---

# Test Workflow
`,
			expectWarning: false,
		},
		{
			name: "no id-token does not produce warning",
			content: `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
  issues: read
---

# Test Workflow
`,
			expectWarning: false,
		},
		{
			name: "id-token write with other permissions produces warning",
			content: `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
  issues: read
  pull-requests: read
  id-token: write
---

# Test Workflow
`,
			expectWarning: true,
		},
		{
			name: "id-token write only produces warning",
			content: `---
on: workflow_dispatch
engine: copilot
permissions:
  id-token: write
---

# Test Workflow
`,
			expectWarning: true,
		},
		{
			name: "no permissions does not produce warning",
			content: `---
on: workflow_dispatch
engine: copilot
---

# Test Workflow
`,
			expectWarning: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "idtoken-warning-test")

			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
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
				t.Errorf("Expected compilation to succeed but it failed: %v", err)
				return
			}

			expectedPhrases := []string{
				"id-token: write",
				"OIDC tokens can authenticate to cloud providers",
				"AWS, Azure, GCP",
				"audience validation",
				"trust policies",
			}

			if tt.expectWarning {
				for _, phrase := range expectedPhrases {
					if !strings.Contains(stderrOutput, phrase) {
						t.Errorf("Expected warning to contain '%s', got stderr:\n%s", phrase, stderrOutput)
					}
				}
				// Check for warning indicator
				if !strings.Contains(stderrOutput, "warning:") {
					t.Errorf("Expected 'warning:' in stderr output, got:\n%s", stderrOutput)
				}
			} else {
				// Should not contain any of the id-token specific warning phrases
				for _, phrase := range expectedPhrases {
					if strings.Contains(stderrOutput, phrase) {
						t.Errorf("Did not expect warning containing '%s', but got stderr:\n%s", phrase, stderrOutput)
					}
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

// TestIdTokenWriteWarningMessageFormat tests that the warning message format
// matches the specified format
func TestIdTokenWriteWarningMessageFormat(t *testing.T) {
	tmpDir := testutil.TempDir(t, "idtoken-warning-format-test")

	content := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
  id-token: write
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

	// Verify the exact warning format
	expectedLines := []string{
		"This workflow grants id-token: write permission",
		"OIDC tokens can authenticate to cloud providers (AWS, Azure, GCP).",
		"Ensure proper audience validation and trust policies are configured.",
	}

	for _, line := range expectedLines {
		if !strings.Contains(stderrOutput, line) {
			t.Errorf("Expected warning to contain line '%s', got stderr:\n%s", line, stderrOutput)
		}
	}
}
