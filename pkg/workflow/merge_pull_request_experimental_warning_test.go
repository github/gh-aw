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

func TestMergePullRequestExperimentalWarning(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		expectWarning bool
	}{
		{
			name: "merge-pull-request enabled produces experimental warning",
			content: `---
on: workflow_dispatch
engine: copilot
safe-outputs:
  merge-pull-request:
---

# Test Workflow
`,
			expectWarning: true,
		},
		{
			name: "no merge-pull-request does not produce experimental warning",
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
			tmpDir := testutil.TempDir(t, "merge-pull-request-experimental-warning-test")

			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			compiler := NewCompiler()
			compiler.SetStrictMode(false)
			err := compiler.CompileWorkflow(testFile)

			w.Close()
			os.Stderr = oldStderr
			var buf bytes.Buffer
			io.Copy(&buf, r)
			stderrOutput := buf.String()

			if err != nil {
				t.Errorf("expected compilation to succeed but it failed: %v", err)
				return
			}

			expectedMessage := "Using experimental feature: merge-pull-request"
			if tt.expectWarning {
				if !strings.Contains(stderrOutput, expectedMessage) {
					t.Errorf("expected warning containing %q, got stderr:\n%s", expectedMessage, stderrOutput)
				}
				if compiler.GetWarningCount() == 0 {
					t.Error("expected warning count > 0 but got 0")
				}
				return
			}

			if strings.Contains(stderrOutput, expectedMessage) {
				t.Errorf("did not expect warning %q, but got stderr:\n%s", expectedMessage, stderrOutput)
			}
		})
	}
}
