//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestCopilotRequestsEnableTip(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		expectTip bool
	}{
		{
			name: "copilot engine without copilot-requests emits tip",
			content: `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
---

# Test Workflow
`,
			expectTip: true,
		},
		{
			name: "copilot engine with copilot-requests write does not emit tip",
			content: `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
  copilot-requests: write
---

# Test Workflow
`,
			expectTip: false,
		},
		{
			name: "copilot engine with copilot-requests none does not emit tip",
			content: `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
  copilot-requests: none
---

# Test Workflow
`,
			expectTip: false,
		},
		{
			name: "non-copilot engine does not emit tip",
			content: `---
on: workflow_dispatch
engine: claude
permissions:
  contents: read
---

# Test Workflow
`,
			expectTip: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "copilot-requests-tip-test")
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.content), 0600); err != nil {
				t.Fatal(err)
			}

			var compileErr error
			stderrOutput := testutil.CaptureStderr(t, func() {
				compiler := NewCompiler()
				compiler.SetStrictMode(false)
				compileErr = compiler.CompileWorkflow(testFile)
			})

			if compileErr != nil {
				t.Fatalf("Expected compilation to succeed but it failed: %v", compileErr)
			}

			const tipText = "Tip: set permissions.copilot-requests: write to use GitHub Actions token-based inference"
			const tipLink = "https://github.github.com/gh-aw/reference/billing/"
			const tipOrgNote = "requires that your organization has centralized Copilot billing enabled and may not be available"
			const tipOptOut = "To suppress this tip when using a PAT, set permissions.copilot-requests: none"
			if tt.expectTip && !strings.Contains(stderrOutput, tipText) {
				t.Fatalf("Expected copilot-requests tip in stderr, got:\n%s", stderrOutput)
			}
			if tt.expectTip && !strings.Contains(stderrOutput, tipLink) {
				t.Fatalf("Expected billing link in copilot-requests tip, got:\n%s", stderrOutput)
			}
			if tt.expectTip && !strings.Contains(stderrOutput, tipOrgNote) {
				t.Fatalf("Expected org billing note in copilot-requests tip, got:\n%s", stderrOutput)
			}
			if tt.expectTip && !strings.Contains(stderrOutput, tipOptOut) {
				t.Fatalf("Expected PAT opt-out in copilot-requests tip, got:\n%s", stderrOutput)
			}
			if !tt.expectTip && strings.Contains(stderrOutput, tipText) {
				t.Fatalf("Did not expect copilot-requests tip in stderr, got:\n%s", stderrOutput)
			}
		})
	}
}
