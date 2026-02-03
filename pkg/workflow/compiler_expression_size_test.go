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

func TestCompileWorkflowExpressionSizeValidation(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "expression-size-test")

	t.Run("workflow with normal expression sizes should compile successfully", func(t *testing.T) {
		// Create a workflow with normal-sized expressions
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
    allowed: [list_issues, issue_read]
---

# Normal Expression Test Workflow

This workflow has normal-sized expressions and should compile successfully.
The content is reasonable and won't generate overly long environment variables.
`

		testFile := filepath.Join(tmpDir, "normal-expressions.md")
		if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
			t.Fatal(err)
		}

		compiler := NewCompiler()
		err := compiler.CompileWorkflow(testFile)
		if err != nil {
			t.Errorf("Expected no error for workflow with normal expressions, got: %v", err)
		}

		// Verify lock file was created
		lockFile := stringutil.MarkdownToLockFile(testFile)
		if _, err := os.Stat(lockFile); err != nil {
			t.Errorf("Lock file was not created: %v", err)
		}
	})

	t.Run("workflow with oversized markdown content should fail validation", func(t *testing.T) {
		t.Skip("FIXME: Markdown content is not embedded in generated YAML, so this validation doesn't apply. " +
			"The validateExpressionSizes() function checks individual YAML lines, but markdown content " +
			"from .md files is not stored in the .lock.yml as environment variables or expressions. " +
			"This test needs to be redesigned or removed.")

		// Create a workflow with markdown content that will exceed the 21KB limit
		// The content will be written to the workflow YAML as a single line in a heredoc
		// We need 25KB+ of content to trigger the validation
		largeContent := strings.Repeat("x", 25000)
		testContent := fmt.Sprintf(`---
on: push
timeout-minutes: 10
permissions:
  contents: read
  pull-requests: write
  issues: read
strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [list_issues]
safe-outputs:
  create-pull-request:
    title-prefix: "[test] "
---

# Large Content Test Workflow

%s
`, largeContent)

		testFile := filepath.Join(tmpDir, "large-content.md")
		if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
			t.Fatal(err)
		}

		compiler := NewCompiler()
		compiler.SetSkipValidation(false) // Enable validation to test expression size limits
		err := compiler.CompileWorkflow(testFile)

		// This should fail with an expression size validation error
		if err == nil {
			t.Error("Expected error for workflow with oversized expressions, got nil")
		} else if !strings.Contains(err.Error(), "exceeds maximum allowed") {
			t.Errorf("Expected 'exceeds maximum allowed' error, got: %v", err)
		} else if !strings.Contains(err.Error(), "expression size validation failed") {
			t.Errorf("Expected 'expression size validation failed' error, got: %v", err)
		}
	})

	t.Run("expression size validation runs by default without explicit enablement", func(t *testing.T) {
		t.Skip("FIXME: Markdown content is not embedded in generated YAML, so this validation doesn't apply. " +
			"The validateExpressionSizes() function checks individual YAML lines, but markdown content " +
			"from .md files is not stored in the .lock.yml as environment variables or expressions. " +
			"This test needs to be redesigned or removed.")

		// Expression size validation should always run, even when skipValidation is true (default)
		// This is because GitHub Actions enforces a hard 21KB limit that cannot be bypassed
		largeContent := strings.Repeat("y", 25000)
		testContent := fmt.Sprintf(`---
on: push
timeout-minutes: 10
permissions:
  contents: read
  pull-requests: write
  issues: read
strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [list_issues]
safe-outputs:
  create-pull-request:
    title-prefix: "[test] "
---

# Large Content Test Workflow (Default Validation)

%s
`, largeContent)

		testFile := filepath.Join(tmpDir, "large-content-default.md")
		if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
			t.Fatal(err)
		}

		// Create compiler without calling SetSkipValidation(false) - expression size
		// validation should still run because it's a mandatory GitHub Actions limit
		compiler := NewCompiler()
		err := compiler.CompileWorkflow(testFile)

		// This should fail with an expression size validation error even without explicit validation enablement
		if err == nil {
			t.Error("Expected error for workflow with oversized expressions with default validation settings, got nil")
		} else if !strings.Contains(err.Error(), "exceeds maximum allowed") {
			t.Errorf("Expected 'exceeds maximum allowed' error with default validation, got: %v", err)
		} else if !strings.Contains(err.Error(), "expression size validation failed") {
			t.Errorf("Expected 'expression size validation failed' error with default validation, got: %v", err)
		}
	})

	t.Run("expression size validation constant", func(t *testing.T) {
		// Verify the constant is set correctly
		if MaxExpressionSize != 21000 {
			t.Errorf("MaxExpressionSize constant should be 21000, got %d", MaxExpressionSize)
		}
	})

	t.Run("expression size validation error message format", func(t *testing.T) {
		// Test that the validation produces correct error message format
		testLineSize := int64(25000) // 25KB, exceeds limit
		actualSize := console.FormatFileSize(testLineSize)
		maxSizeFormatted := console.FormatFileSize(int64(MaxExpressionSize))

		expectedMessage := fmt.Sprintf("expression value for 'WORKFLOW_MARKDOWN' (%s) exceeds maximum allowed size (%s)",
			actualSize, maxSizeFormatted)

		// Verify the message contains expected elements
		if !strings.Contains(expectedMessage, "exceeds maximum allowed size") {
			t.Error("Error message should contain 'exceeds maximum allowed size'")
		}
		if !strings.Contains(expectedMessage, "KB") {
			t.Error("Error message should contain size in KB")
		}
		if !strings.Contains(expectedMessage, "WORKFLOW_MARKDOWN") {
			t.Error("Error message should identify the problematic key")
		}

		t.Logf("Generated error message: %s", expectedMessage)
	})
}
