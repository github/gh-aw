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

func TestIssueIntentExperimentalWarning(t *testing.T) {
	tests := []struct {
		name          string
		issueIntent   string
		expectWarning bool
	}{
		{name: "issue-intent true", issueIntent: "    issue-intent: true\n", expectWarning: true},
		{name: "issue-intent false", issueIntent: "    issue-intent: false\n", expectWarning: false},
		{name: "issue-intent omitted", issueIntent: "", expectWarning: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := "---\non: workflow_dispatch\nengine: copilot\nsafe-outputs:\n  add-labels:\n" + tt.issueIntent + "    max: 3\n---\n\n# Test Workflow\n"
			tmpDir := testutil.TempDir(t, "issue-intent-experimental-warning-test")
			testFile := filepath.Join(tmpDir, "test-workflow.md")
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
				t.Fatalf("expected compilation to succeed but it failed: %v", err)
			}

			const message = "Using experimental feature: issue-intent"
			if got := strings.Contains(stderrOutput, message); got != tt.expectWarning {
				t.Errorf("warning %q present=%v, want %v; stderr:\n%s", message, got, tt.expectWarning, stderrOutput)
			}
			if got := compiler.GetExperimentalWarningCount(); (got == 1) != tt.expectWarning {
				t.Errorf("experimental warning count = %d, expectWarning=%v", got, tt.expectWarning)
			}
		})
	}
}
