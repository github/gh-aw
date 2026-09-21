//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
)

func TestCompileWorkflow_GeminiArcDindDoesNotStageCopilotCLI(t *testing.T) {
	workflow := `---
on: workflow_dispatch
engine: gemini
runner:
  topology: arc-dind
network:
  allowed:
    - defaults
---

# Test
`
	testFile := filepath.Join(testutil.TempDir(t, "gemini-arc-dind-test"), "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(workflow), 0644); err != nil {
		t.Fatal(err)
	}

	if err := NewCompiler().CompileWorkflow(testFile); err != nil {
		t.Fatalf("compile Gemini workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(testFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("read lock file: %v", err)
	}
	if strings.Contains(string(lockContent), "Copy Copilot CLI to daemon-visible path") {
		t.Fatal("Gemini workflow must not stage the Copilot CLI")
	}
}
