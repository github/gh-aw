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

// TestSafeInputsExperimentalWarning tests that the safe-inputs feature
// emits an experimental warning when enabled.
func TestSafeInputsExperimentalWarning(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		expectWarning bool
	}{
		{
			name: "safe-inputs enabled produces experimental warning",
			content: `---
on: workflow_dispatch
engine: copilot
safe-inputs:
  greet-user:
    description: "Greet a user by name"
    inputs:
      name:
        type: string
        required: true
    script: |
      return { message: 'Hello, ' + name + '!' };
permissions:
  contents: read
---

# Test Workflow
`,
			expectWarning: true,
		},
		{
			name: "no safe-inputs does not produce experimental warning",
			content: `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
---

# Test Workflow
`,
			expectWarning: false,
		},
		{
			name: "empty safe-inputs does not produce experimental warning",
			content: `---
on: workflow_dispatch
engine: copilot
safe-inputs: {}
permissions:
  contents: read
---

# Test Workflow
`,
			expectWarning: false,
		},
		{
			name: "safe-inputs with shell tool produces experimental warning",
			content: `---
on: workflow_dispatch
engine: copilot
safe-inputs:
  list-files:
    description: "List files in current directory"
    run: |
      ls -la
permissions:
  contents: read
---

# Test Workflow
`,
			expectWarning: true,
		},
		{
			name: "safe-inputs with python tool produces experimental warning",
			content: `---
on: workflow_dispatch
engine: copilot
safe-inputs:
  analyze-data:
    description: "Analyze data with Python"
    inputs:
      numbers:
        type: string
        required: true
    py: |
      import json
      numbers_str = inputs.get('numbers', '')
      print(json.dumps({"count": len(numbers_str.split(','))}))
permissions:
  contents: read
---

# Test Workflow
`,
			expectWarning: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "safe-inputs-experimental-warning-test")

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

			expectedMessage := "Using experimental feature: safe-inputs"

			if tt.expectWarning {
				if !strings.Contains(stderrOutput, expectedMessage) {
					t.Errorf("Expected warning containing '%s', got stderr:\n%s", expectedMessage, stderrOutput)
				}
			} else {
				if strings.Contains(stderrOutput, expectedMessage) {
					t.Errorf("Did not expect warning '%s', but got stderr:\n%s", expectedMessage, stderrOutput)
				}
			}

			// Verify warning count includes safe-inputs warning
			if tt.expectWarning {
				warningCount := compiler.GetWarningCount()
				if warningCount == 0 {
					t.Error("Expected warning count > 0 but got 0")
				}
			}
		})
	}
}
