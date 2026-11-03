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

func TestDispatchRepositoryNoExperimentalWarning(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name: "dispatch_repository enabled",
			content: `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
safe-outputs:
  dispatch_repository:
    trigger_ci:
      description: Trigger CI
      workflow: ci.yml
      event_type: ci_trigger
      repository: org/target-repo
---

# Test Workflow
`,
		},
		{
			name: "no dispatch_repository",
			content: `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
---

# Test Workflow
`,
		},
		{
			name: "dispatch_repository with allowed_repositories",
			content: `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
safe-outputs:
  dispatch_repository:
    notify_service:
      workflow: notify.yml
      event_type: notify_event
      allowed_repositories:
        - org/service-repo
        - org/backup-repo
---

# Test Workflow
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "dispatch-repository-experimental-warning-test")

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

			unexpectedMessage := "Using experimental feature: dispatch-repository"
			if strings.Contains(stderrOutput, unexpectedMessage) {
				t.Errorf("Did not expect warning '%s', but got stderr:\n%s", unexpectedMessage, stderrOutput)
			}
			if compiler.GetExperimentalWarningCount() != 0 {
				t.Error("Expected no experimental warnings")
			}
		})
	}
}
