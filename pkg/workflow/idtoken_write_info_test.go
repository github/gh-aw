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

func TestIdTokenWriteInfo(t *testing.T) {
	tests := []struct {
		name              string
		content           string
		expectInfo        bool
		expectCompileFail bool
	}{
		{
			name: "id-token write produces info",
			content: `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
  id-token: write
---

# Test Workflow
`,
			expectInfo: true,
		},
		{
			name: "id-token read is invalid and compilation fails",
			content: `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
  id-token: read
---

# Test Workflow
`,
			expectInfo:        false,
			expectCompileFail: true,
		},
		{
			name: "no id-token does not produce info",
			content: `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
  issues: read
---

# Test Workflow
`,
			expectInfo: false,
		},
		{
			name: "id-token write with other permissions produces info",
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
			expectInfo: true,
		},
		{
			name: "id-token write only produces info",
			content: `---
on: workflow_dispatch
engine: copilot
permissions:
  id-token: write
---

# Test Workflow
`,
			expectInfo: true,
		},
		{
			name: "no permissions does not produce info",
			content: `---
on: workflow_dispatch
engine: copilot
---

# Test Workflow
`,
			expectInfo: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "idtoken-info-test")

			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			// Capture stderr to check the diagnostic severity.
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

			// Handle cases where compilation is expected to fail
			if tt.expectCompileFail {
				if err == nil {
					t.Errorf("Expected compilation to fail but it succeeded")
				}
				return
			}

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

			if tt.expectInfo {
				for _, phrase := range expectedPhrases {
					if !strings.Contains(stderrOutput, phrase) {
						t.Errorf("Expected info to contain '%s', got stderr:\n%s", phrase, stderrOutput)
					}
				}
				if !strings.Contains(stderrOutput, "info: This workflow grants id-token: write permission") {
					t.Errorf("Expected id-token info diagnostic in stderr output, got:\n%s", stderrOutput)
				}
				if strings.Contains(stderrOutput, "warning: This workflow grants id-token: write permission") {
					t.Errorf("Did not expect id-token warning in stderr output, got:\n%s", stderrOutput)
				}
			} else {
				for _, phrase := range expectedPhrases {
					if strings.Contains(stderrOutput, phrase) {
						t.Errorf("Did not expect info containing '%s', but got stderr:\n%s", phrase, stderrOutput)
					}
				}
			}

			if tt.expectInfo {
				warningCount := compiler.GetWarningCount()
				if warningCount != 0 {
					t.Errorf("Expected info not to increment warning count, got %d warnings", warningCount)
				}
			}
		})
	}
}

func TestIdTokenWriteInfoMessageFormat(t *testing.T) {
	tmpDir := testutil.TempDir(t, "idtoken-info-format-test")

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

	// Capture stderr to check the diagnostic severity.
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

	expectedLines := []string{
		"info: This workflow grants id-token: write permission",
		"OIDC tokens can authenticate to cloud providers (AWS, Azure, GCP).",
		"Ensure proper audience validation and trust policies are configured.",
	}

	for _, line := range expectedLines {
		if !strings.Contains(stderrOutput, line) {
			t.Errorf("Expected info to contain line '%s', got stderr:\n%s", line, stderrOutput)
		}
	}
}
