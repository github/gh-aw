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

func TestAwInfoTmpPath(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "aw-info-tmp-test")

	// Create a test markdown file with minimal frontmatter for Claude engine
	testContent := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
tools:
  github:
    allowed: [list_issues]
source: github/gh-aw/.github/workflows/test-aw-info-tmp.md@main
emoji: "🧪"
engine: claude
strict: false
---

# Test aw_info.json tmp path

This workflow tests that aw_info.json is generated in /tmp directory.
`

	testFile := filepath.Join(tmpDir, "test-aw-info-tmp.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(testFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockStr := string(lockContent)

	// Test 1: Verify the step uses the generate_aw_info.cjs module
	if !strings.Contains(lockStr, "require(path.join(actionsDir, 'generate_aw_info.cjs'))") {
		t.Error("Expected step to require generate_aw_info.cjs module")
	}

	if !strings.Contains(lockStr, "await main(core, context);") {
		t.Error("Expected step to call main(core, context) from generate_aw_info.cjs")
	}

	// Verify setupGlobals is called before main so that global.core is available
	// for modules like staged_preview.cjs that rely on it (fixes staged mode ReferenceError)
	if !strings.Contains(lockStr, "setupGlobals(core, github, context, exec, io, getOctokit)") {
		t.Error("Expected step to call setupGlobals before main to set global.core")
	}

	// Test 2: Verify compile-time env vars are set on the step
	if !strings.Contains(lockStr, "GH_AW_INFO_ENGINE_ID:") {
		t.Error("Expected GH_AW_INFO_ENGINE_ID env var to be set on the step")
	}

	if !strings.Contains(lockStr, "GH_AW_INFO_MODEL:") {
		t.Error("Expected GH_AW_INFO_MODEL env var to be set on the step")
	}

	if !strings.Contains(lockStr, "GH_AW_INFO_ALLOWED_DOMAINS:") {
		t.Error("Expected GH_AW_INFO_ALLOWED_DOMAINS env var to be set on the step")
	}
	if !strings.Contains(lockStr, "GH_AW_INFO_BODY_MODIFIED: \"false\"") {
		t.Error("Expected GH_AW_INFO_BODY_MODIFIED env var to default to false on the step")
	}
	if !strings.Contains(lockStr, `GH_AW_INFO_FRONTMATTER_SOURCE: "github/gh-aw/.github/workflows/test-aw-info-tmp.md@main"`) {
		t.Error("Expected GH_AW_INFO_FRONTMATTER_SOURCE env var to be set on the step")
	}
	if !strings.Contains(lockStr, `GH_AW_INFO_FRONTMATTER_EMOJI: "🧪"`) {
		t.Error("Expected GH_AW_INFO_FRONTMATTER_EMOJI env var to be set on the step")
	}

	// Test 3: Verify upload artifact uses the private runtime path
	if !strings.Contains(lockStr, relocateRuntimeText("/tmp/gh-aw/aw_info.json")) {
		t.Error("Expected artifact upload to include the isolated aw_info.json path in generated workflow")
	}

	// Test 4: Verify the old inline aw_info construction is not present
	if strings.Contains(lockStr, "const awInfo = {") {
		t.Error("Found old inline awInfo construction in generated workflow; should use generate_aw_info.cjs")
	}

	t.Logf("Successfully verified aw_info.json is generated in the private runtime directory")
}
