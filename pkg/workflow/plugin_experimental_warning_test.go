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

// TestPluginExperimentalWarning tests that the plugins feature
// emits an experimental warning when enabled.
func TestPluginExperimentalWarning(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		expectWarning bool
	}{
		{
			name: "plugins enabled produces experimental warning",
			content: `---
on: workflow_dispatch
engine: copilot
plugins:
  - github/test-plugin
permissions:
  contents: read
---

# Test Workflow
`,
			expectWarning: true,
		},
		{
			name: "no plugins does not produce experimental warning",
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
			name: "empty plugins array does not produce experimental warning",
			content: `---
on: workflow_dispatch
engine: copilot
plugins: []
permissions:
  contents: read
---

# Test Workflow
`,
			expectWarning: false,
		},
		{
			name: "multiple plugins produce experimental warning",
			content: `---
on: workflow_dispatch
engine: copilot
plugins:
  - github/plugin1
  - github/plugin2
permissions:
  contents: read
---

# Test Workflow
`,
			expectWarning: true,
		},
		{
			name: "plugins with object format produces experimental warning",
			content: `---
on: workflow_dispatch
engine: copilot
plugins:
  repos:
    - github/test-plugin
  github-token: ${{ secrets.CUSTOM_TOKEN }}
permissions:
  contents: read
---

# Test Workflow
`,
			expectWarning: true,
		},
		{
			name: "plugins with MCP config produces experimental warning",
			content: `---
on: workflow_dispatch
engine: copilot
plugins:
  - id: github/test-plugin
    mcp:
      env:
        API_KEY: ${{ secrets.API_KEY }}
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
			tmpDir := testutil.TempDir(t, "plugin-experimental-warning-test")

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

			expectedMessage := "Using experimental feature: plugins"

			if tt.expectWarning {
				if !strings.Contains(stderrOutput, expectedMessage) {
					t.Errorf("Expected warning containing '%s', got stderr:\n%s", expectedMessage, stderrOutput)
				}
			} else {
				if strings.Contains(stderrOutput, expectedMessage) {
					t.Errorf("Did not expect warning '%s', but got stderr:\n%s", expectedMessage, stderrOutput)
				}
			}

			// Verify warning count includes plugins warning
			if tt.expectWarning {
				warningCount := compiler.GetWarningCount()
				if warningCount == 0 {
					t.Error("Expected warning count > 0 but got 0")
				}
			}
		})
	}
}
