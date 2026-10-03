//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
)

// compileWebFetchWorkflow is a shared helper that compiles a workflow with web-fetch enabled
// for the given engine and returns the lock file content.
func compileWebFetchWorkflow(t *testing.T, engine string) string {
	t.Helper()
	tmpDir := testutil.TempDir(t, "test-*")

	workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: ` + engine + `
tools:
  web-fetch:
---

# Test Workflow

Fetch content from the web.
`

	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write test workflow: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	return string(lockData)
}

// TestMCPFetchNotAddedForAnyEngine tests that the mcp/fetch container is never added
// to any compiled workflow, regardless of engine, since the fallback has been removed.
func TestMCPFetchNotAddedForAnyEngine(t *testing.T) {
	engines := []string{"codex", "claude", "copilot", "gemini"}

	for _, engine := range engines {
		t.Run(engine, func(t *testing.T) {
			lockContent := compileWebFetchWorkflow(t, engine)
			if strings.Contains(lockContent, `mcp/fetch`) {
				t.Errorf("Engine %q: expected no mcp/fetch container in compiled workflow, but it was present", engine)
			}
		})
	}
}

// TestWebFetchClaudeAllowedTools tests that a Claude workflow with web-fetch
// includes WebFetch in the allowed tools list.
func TestWebFetchClaudeAllowedTools(t *testing.T) {
	lockContent := compileWebFetchWorkflow(t, "claude")
	if !strings.Contains(lockContent, "WebFetch") {
		t.Errorf("Expected Claude workflow to have WebFetch in allowed tools, but it didn't")
	}
}

// TestWebFetchCodexNativeFetchTool tests that a Codex workflow with web-fetch uses
// the native browsing tool instead of an mcp/fetch container.
func TestWebFetchCodexNativeFetchTool(t *testing.T) {
	lockContent := compileWebFetchWorkflow(t, "codex")
	if strings.Contains(lockContent, `-c fetch="disabled"`) {
		t.Errorf(`Expected Codex workflow with web-fetch to NOT have -c fetch="disabled", but it did`)
	}
	if strings.Contains(lockContent, `-c web_search="disabled"`) {
		t.Error("Expected web-fetch to enable Codex's shared search/browsing tool")
	}
}

// TestCodexFetchDisabledByDefault tests that a Codex workflow without web-fetch
// disables the shared native search/browsing tool.
func TestCodexFetchDisabledByDefault(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: codex
---

# Test Workflow

Run some bash commands.
`

	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write test workflow: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContent := string(lockData)
	if !strings.Contains(lockContent, `-c web_search="disabled"`) {
		t.Error("Expected Codex workflow without web tools to disable native browsing")
	}
}

// TestWebFetchGeminiNativeWebFetchTool tests that a Gemini workflow with web-fetch
// includes web_fetch in its tools.core settings.
func TestWebFetchGeminiNativeWebFetchTool(t *testing.T) {
	lockContent := compileWebFetchWorkflow(t, "gemini")
	if !strings.Contains(lockContent, "web_fetch") {
		t.Errorf("Expected Gemini workflow with web-fetch to include web_fetch in tools.core, but it didn't")
	}
}

// TestNoWebFetchNoMCPFetchServer tests that when a workflow doesn't use web-fetch,
// no web-fetch MCP server configuration is added.
func TestNoWebFetchNoMCPFetchServer(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: codex
---

# Test Workflow

Run some bash commands.
`

	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write test workflow: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContent := string(lockData)
	if strings.Contains(lockContent, `mcp/fetch`) {
		t.Errorf("Expected workflow without web-fetch NOT to contain mcp/fetch, but it did")
	}
}
