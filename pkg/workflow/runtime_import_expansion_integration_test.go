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

// TestRuntimeImportExpansionWithGitHubFalse verifies the primary regression scenario:
// a workflow with tools.github: false, no expressions, and no {{#if}} blocks must still
// receive the "Interpolate variables and render templates" step so that the
// author prompt and its runtime imports are resolved at runtime.
func TestRuntimeImportExpansionWithGitHubFalse(t *testing.T) {
	tmpDir := testutil.TempDir(t, "runtime-import-expansion-github-false")
	workflowDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0755); err != nil {
		t.Fatalf("failed to create workflow directory: %v", err)
	}

	workflowContent := `---
on: repository_dispatch
permissions:
  contents: read
engine:
  id: claude
tools:
  edit:
  github: false
safe-outputs:
  create-pull-request:
---

Do some important work.
`
	workflowPath := filepath.Join(workflowDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("failed to write workflow file: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("compilation failed: %v", err)
	}

	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("failed to read lock file: %v", err)
	}
	lockContent := string(lockBytes)

	if !strings.Contains(promptRenderPlan(t, lockContent), "{{#author-prompt .github/workflows/test-workflow.md}}") {
		t.Error("prompt render config must load the main workflow as an author file")
	}

	// The interpolation step must be present even without GitHub tools or expressions.
	if !strings.Contains(lockContent, "Interpolate variables and render templates") {
		t.Error("lock file must contain 'Interpolate variables and render templates' step")
	}
	if !strings.Contains(lockContent, "interpolate_prompt.cjs") {
		t.Error("lock file must reference interpolate_prompt.cjs in the interpolation step")
	}
}

// TestRuntimeImportExpansionWithOptionalMacro verifies that a workflow embedding an
// optional {{#runtime-import? ...}} macro in the body also gets the interpolation step,
// even when tools.github is disabled and there are no template expressions.
func TestRuntimeImportExpansionWithOptionalMacro(t *testing.T) {
	tmpDir := testutil.TempDir(t, "runtime-import-expansion-optional")
	workflowDir := filepath.Join(tmpDir, ".github", "workflows")
	sharedDir := filepath.Join(tmpDir, ".github", "shared")
	if err := os.MkdirAll(workflowDir, 0755); err != nil {
		t.Fatalf("failed to create workflow directory: %v", err)
	}
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatalf("failed to create shared directory: %v", err)
	}

	// Optional shared file - its presence is not required for the workflow to compile.
	if err := os.WriteFile(filepath.Join(sharedDir, "extra.md"), []byte("# Extra instructions\n"), 0644); err != nil {
		t.Fatalf("failed to write shared file: %v", err)
	}

	workflowContent := `---
on: issues
permissions:
  contents: read
  issues: read
engine: claude
tools:
  github: false
strict: false
---

Core task description.

{{#runtime-import? .github/shared/extra.md}}
`
	workflowPath := filepath.Join(workflowDir, "optional-import.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("failed to write workflow file: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("compilation failed: %v", err)
	}

	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("failed to read lock file: %v", err)
	}
	lockContent := string(lockBytes)

	// Body macros are loaded from the author file rather than embedded in the lock file.
	if !strings.Contains(promptRenderPlan(t, lockContent), "{{#author-prompt .github/workflows/optional-import.md}}") {
		t.Error("prompt render config must load the author file containing the optional runtime import")
	}

	// The interpolation step must be present so the optional macro is expanded at runtime.
	if !strings.Contains(lockContent, "Interpolate variables and render templates") {
		t.Error("lock file must contain 'Interpolate variables and render templates' step so that {{#runtime-import?}} is resolved")
	}
	if !strings.Contains(lockContent, "interpolate_prompt.cjs") {
		t.Error("lock file must reference interpolate_prompt.cjs in the interpolation step")
	}
}

// TestRuntimeImportExpansionWithMacroAfterText verifies that a {{#runtime-import}} macro
// embedded after other body text in the workflow is detected by strings.Contains (not
// HasPrefix), and the interpolation step is still emitted.
func TestRuntimeImportExpansionWithMacroAfterText(t *testing.T) {
	tmpDir := testutil.TempDir(t, "runtime-import-expansion-after-text")
	workflowDir := filepath.Join(tmpDir, ".github", "workflows")
	sharedDir := filepath.Join(tmpDir, ".github", "shared")
	if err := os.MkdirAll(workflowDir, 0755); err != nil {
		t.Fatalf("failed to create workflow directory: %v", err)
	}
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatalf("failed to create shared directory: %v", err)
	}

	if err := os.WriteFile(filepath.Join(sharedDir, "appendix.md"), []byte("# Appendix\n\nExtra context.\n"), 0644); err != nil {
		t.Fatalf("failed to write shared file: %v", err)
	}

	// Body text comes before the explicit user runtime-import directive.
	workflowContent := `---
on: push
permissions:
  contents: read
engine: claude
tools:
  github: false
strict: false
---

Primary instructions that appear before the import.

{{#runtime-import .github/shared/appendix.md}}
`
	workflowPath := filepath.Join(workflowDir, "after-text.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("failed to write workflow file: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("compilation failed: %v", err)
	}

	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("failed to read lock file: %v", err)
	}
	lockContent := string(lockBytes)

	// The macro must appear in the lock file.
	if !strings.Contains(lockContent, "{{#runtime-import") {
		t.Error("lock file must contain a {{#runtime-import}} macro")
	}

	// The interpolation step must be present regardless of where in the chunk the macro appears.
	if !strings.Contains(lockContent, "Interpolate variables and render templates") {
		t.Error("lock file must contain 'Interpolate variables and render templates' step so that {{#runtime-import}} macros embedded after other text are resolved")
	}
	if !strings.Contains(lockContent, "interpolate_prompt.cjs") {
		t.Error("lock file must reference interpolate_prompt.cjs in the interpolation step")
	}
}
