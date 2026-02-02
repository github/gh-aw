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
  issues: write
  pull-requests: read
tools:
  github:
    allowed: [list_issues]
engine: claude
features:
  dangerous-permissions-write: true
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

	// Test 1: Verify aw_info.json is written to /tmp/gh-aw/aw_info.json
	if !strings.Contains(lockStr, "const tmpPath = '/tmp/gh-aw/aw_info.json';") {
		t.Error("Expected tmpPath to be set to '/tmp/gh-aw/aw_info.json' in generated workflow")
	}

	if !strings.Contains(lockStr, "fs.writeFileSync(tmpPath, JSON.stringify(awInfo, null, 2));") {
		t.Error("Expected writeFileSync to use tmpPath variable in generated workflow")
	}

	// Test 2: Verify upload artifact includes /tmp/gh-aw/aw_info.json path
	if !strings.Contains(lockStr, "/tmp/gh-aw/aw_info.json") {
		t.Error("Expected unified artifact upload to include '/tmp/gh-aw/aw_info.json' path in generated workflow")
	}

	// Test 3: Verify the old hardcoded path is not present
	if strings.Contains(lockStr, "fs.writeFileSync('aw_info.json'") {
		t.Error("Found old hardcoded path 'aw_info.json' in generated workflow, should use /tmp/gh-aw/aw_info.json")
	}

	if strings.Contains(lockStr, "path: aw_info.json") && !strings.Contains(lockStr, "path: /tmp/gh-aw/aw_info.json") {
		t.Error("Found old artifact path 'aw_info.json' without /tmp/gh-aw prefix in generated workflow")
	}

	t.Logf("Successfully verified aw_info.json is generated in /tmp/gh-aw directory")
}
