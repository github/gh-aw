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

func TestApproveWorkflowRunNoExperimentalWarning(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name: "approve-workflow-run enabled",
			content: `---
on: workflow_dispatch
engine: copilot
safe-outputs:
  approve-workflow-run:
    allowed-workflows: ["ci.yml"]
    github-token: ${{ secrets.APPROVE_WORKFLOW_RUN_TOKEN }}
---

# Test Workflow
`,
		},
		{
			name: "no approve-workflow-run",
			content: `---
on: workflow_dispatch
engine: copilot
---

# Test Workflow
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "approve-workflow-run-experimental-warning-test")
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.content), 0o644); err != nil {
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
			_, _ = io.Copy(&buf, r)
			stderrOutput := buf.String()

			if err != nil {
				t.Errorf("expected compilation to succeed but it failed: %v", err)
				return
			}

			unexpectedMessage := "Using experimental feature: approve-workflow-run"
			if strings.Contains(stderrOutput, unexpectedMessage) {
				t.Errorf("did not expect warning %q, but got stderr:\n%s", unexpectedMessage, stderrOutput)
			}
			if compiler.GetExperimentalWarningCount() != 0 {
				t.Error("expected no experimental warnings")
			}
		})
	}
}
