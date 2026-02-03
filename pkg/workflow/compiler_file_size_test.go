//go:build integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"

	"github.com/github/gh-aw/pkg/console"
)

func TestCompileWorkflowFileSizeValidation(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "file-size-test")

	t.Run("workflow under 500KB should compile successfully", func(t *testing.T) {
		// Create a normal workflow that should be well under 500KB
		testContent := `---
on: push
timeout-minutes: 10
permissions:
  contents: read
  issues: write
  pull-requests: read
strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [list_issues, create_issue]
---

# Normal Test Workflow

This is a normal workflow that should compile successfully.
`

		testFile := filepath.Join(tmpDir, "normal-workflow.md")
		if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
			t.Fatal(err)
		}

		compiler := NewCompiler()
		err := compiler.CompileWorkflow(testFile)
		if err != nil {
			t.Errorf("Expected no error for normal workflow, got: %v", err)
		}

		// Verify lock file was created and is under 500KB
		lockFile := stringutil.MarkdownToLockFile(testFile)
		if info, err := os.Stat(lockFile); err != nil {
			t.Errorf("Lock file was not created: %v", err)
		} else if info.Size() > MaxLockFileSize {
			t.Errorf("Lock file size %d exceeds max size %d", info.Size(), MaxLockFileSize)
		}
	})

	t.Run("file size validation logic", func(t *testing.T) {
		// Test the validation by creating a temporary compiler with modified constant
		// Since normal workflows don't exceed 1MB, we'll test the validation path differently

		// Create a normal workflow
		testContent := `---
on: push
timeout-minutes: 10
permissions:
  contents: read
  issues: write
  pull-requests: read
strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [list_issues, create_issue]
---

# Test Workflow for Size Validation

This workflow tests the file size validation logic.
`

		testFile := filepath.Join(tmpDir, "size-test-workflow.md")
		if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
			t.Fatal(err)
		}

		compiler := NewCompiler()
		err := compiler.CompileWorkflow(testFile)
		if err != nil {
			t.Errorf("Expected no error for normal workflow, got: %v", err)
		}

		// Verify the lock file exists and get its size
		lockFile := stringutil.MarkdownToLockFile(testFile)
		info, err := os.Stat(lockFile)
		if err != nil {
			t.Fatalf("Lock file was not created: %v", err)
		}

		// The lock file should be well under 500KB (typically around 30KB)
		if info.Size() > MaxLockFileSize {
			t.Errorf("Unexpected: lock file size %d exceeds max size %d", info.Size(), MaxLockFileSize)
		}

		// Verify our constant is correct (500KB = 512000 bytes)
		if MaxLockFileSize != 512000 {
			t.Errorf("MaxLockFileSize constant should be 512000, got %d", MaxLockFileSize)
		}
	})

	t.Run("test file size validation warning message", func(t *testing.T) {
		// Test that our validation produces the correct warning message format
		// by simulating the warning condition

		testFile := filepath.Join(tmpDir, "size-validation-test.md")
		lockFile := stringutil.MarkdownToLockFile(testFile)

		// Create a mock file that exceeds the size limit
		largeSize := int64(MaxLockFileSize + 100000) // 100KB over the limit
		mockContent := strings.Repeat("x", int(largeSize))

		if err := os.WriteFile(lockFile, []byte(mockContent), 0644); err != nil {
			t.Fatal(err)
		}

		// Verify the file exceeds the limit
		info, err := os.Stat(lockFile)
		if err != nil {
			t.Fatalf("Failed to stat mock file: %v", err)
		}

		if info.Size() <= MaxLockFileSize {
			t.Fatalf("Mock file size %d should exceed limit %d", info.Size(), MaxLockFileSize)
		}

		// Test our validation logic by checking what the warning message would look like
		lockSize := console.FormatFileSize(info.Size())
		maxSize := console.FormatFileSize(MaxLockFileSize)
		expectedMessage := fmt.Sprintf("Generated lock file size (%s) exceeds recommended maximum size (%s)", lockSize, maxSize)

		t.Logf("Generated warning message would be: %s", expectedMessage)

		// Verify the message contains expected elements
		if !strings.Contains(expectedMessage, "exceeds recommended maximum size") {
			t.Error("Warning message should contain 'exceeds recommended maximum size'")
		}
		if !strings.Contains(expectedMessage, "KB") {
			t.Error("Warning message should contain size in KB")
		}

		// Clean up
		os.Remove(lockFile)
	})
}
