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

// TestExperimentsNoExperimentalWarning tests that the experiments feature
// does not emit an experimental warning, as the feature is no longer
// considered experimental.
func TestExperimentsNoExperimentalWarning(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name: "experiments enabled does not produce experimental warning",
			content: `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
experiments:
  prompt_style:
    - concise
    - verbose
---

# Test Workflow
`,
		},
		{
			name: "no experiments does not produce experimental warning",
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
			name: "multiple experiments do not produce experimental warning",
			content: `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
experiments:
  prompt_style:
    - concise
    - verbose
  model_temp:
    - low
    - high
---

# Test Workflow
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "experiments-experimental-warning-test")

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

			unexpectedMessage := "Using experimental feature: experiments"
			if strings.Contains(stderrOutput, unexpectedMessage) {
				t.Errorf("Did not expect experimental warning '%s', but got stderr:\n%s", unexpectedMessage, stderrOutput)
			}
		})
	}
}

func TestContinualExperimentsNoExperimentalWarning(t *testing.T) {
	tmpDir := testutil.TempDir(t, "continual-experiments-experimental-warning-test")
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	content := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
evals:
  - id: quality
    question: Did the workflow produce a useful result?
experiments:
  optimization:
    variants: [control, candidate]
    continual:
      seed: stable-seed
      ramp: [10, 25, 50]
---

# Test Workflow
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
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
		t.Fatalf("Expected compilation to succeed but it failed: %v", err)
	}
	unexpectedMessage := "Using experimental feature: continual experiments"
	if strings.Contains(stderrOutput, unexpectedMessage) {
		t.Errorf("Did not expect warning containing %q, got stderr:\n%s", unexpectedMessage, stderrOutput)
	}
	if compiler.GetExperimentalWarningCount() != 0 {
		t.Error("Expected no experimental warnings")
	}
}
