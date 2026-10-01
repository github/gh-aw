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

func compileWorkflowAndReadLock(t *testing.T, workflow string) string {
	t.Helper()
	testDir := testutil.TempDir(t, "test-model-not-supported-*")
	workflowFile := filepath.Join(testDir, "test-workflow.md")
	if err := os.WriteFile(workflowFile, []byte(workflow), 0644); err != nil {
		t.Fatalf("Failed to write test workflow: %v", err)
	}
	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}
	lockFile := stringutil.MarkdownToLockFile(workflowFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	return string(lockContent)
}

// TestModelNotSupportedErrorDetectionStep tests that engines with detect-agent-errors support
// expose model_not_supported_error from that step.
func TestModelNotSupportedErrorDetectionStep(t *testing.T) {
	t.Parallel()
	engines := []string{"copilot", "codex", "claude", "pi"}
	for _, engine := range engines {
		t.Run(engine, func(t *testing.T) {
			t.Parallel()
			lockStr := compileWorkflowAndReadLock(t, `---
on: workflow_dispatch
engine: `+engine+`
---

Test workflow`)
			if !strings.Contains(lockStr, "id: agentic_execution") {
				t.Error("Expected agent job to have agentic_execution step")
			}
			if !strings.Contains(lockStr, "id: detect-agent-errors") {
				t.Error("Expected agent job to have a separate detect-agent-errors step")
			}
			if !strings.Contains(lockStr, "model_not_supported_error: ${{ steps.detect-agent-errors.outputs.model_not_supported_error || 'false' }}") {
				t.Error("Expected agent job to have model_not_supported_error output from detect-agent-errors step")
			}
		})
	}
}

// TestModelNotSupportedErrorInConclusionJob tests that the conclusion job receives the
// model-not-supported error env var when the engine provides detect-agent-errors support.
func TestModelNotSupportedErrorInConclusionJob(t *testing.T) {
	t.Parallel()
	engines := []string{"copilot", "codex", "claude", "pi"}
	for _, engine := range engines {
		t.Run(engine, func(t *testing.T) {
			t.Parallel()
			lockStr := compileWorkflowAndReadLock(t, `---
on: workflow_dispatch
engine: `+engine+`
safe-outputs:
  add-comment:
    max: 5
---

Test workflow`)
			if !strings.Contains(lockStr, "GH_AW_MODEL_NOT_SUPPORTED_ERROR: ${{ needs.agent.outputs.model_not_supported_error }}") {
				t.Error("Expected conclusion job to receive model_not_supported_error from agent job")
			}
		})
	}
}

// TestModelNotSupportedErrorNotInEngineWithoutDetectionScript tests that engines without
// detect-agent-errors support do not include model_not_supported_error output.
func TestModelNotSupportedErrorNotInEngineWithoutDetectionScript(t *testing.T) {
	lockStr := compileWorkflowAndReadLock(t, `---
on: workflow_dispatch
engine: gemini
---

Test workflow`)
	if strings.Contains(lockStr, "model_not_supported_error:") {
		t.Error("Expected engine without detection script to NOT have model_not_supported_error output")
	}
}
