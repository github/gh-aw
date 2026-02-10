//go:build !integration

package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestSchemaValidationErrorLocationAtVariousDepths tests that schema validation errors
// are reported at the correct line and column for errors at different nesting depths.
// This is an end-to-end test that validates the integration of schema validation,
// error location detection, and error formatting.
func TestSchemaValidationErrorLocationAtVariousDepths(t *testing.T) {
	tests := []struct {
		name             string
		workflowContent  string
		expectedLine     int
		expectedProperty string
		description      string
	}{
		{
			name: "depth 1 - invalid property at root level",
			workflowContent: `---
on: daily
invalid-root-prop: value
---
Test workflow.`,
			expectedLine:     3,
			expectedProperty: "invalid-root-prop",
			description:      "Error at root level of frontmatter",
		},
		{
			name: "depth 2 - invalid property in safe-outputs",
			workflowContent: `---
on: daily
safe-outputs:
  invalid-handler: true
---
Test workflow.`,
			expectedLine:     4,
			expectedProperty: "invalid-handler",
			description:      "Error at depth 2 in safe-outputs",
		},
		{
			name: "depth 3 - invalid property in safe-outputs handler",
			workflowContent: `---
on: daily
safe-outputs:
  create-issue:
    invalid-field: value
---
Test workflow.`,
			expectedLine:     5,
			expectedProperty: "invalid-field",
			description:      "Error at depth 3 in safe-outputs handler",
		},
		{
			name: "depth 3 - the original bug case",
			workflowContent: `---
on: daily
safe-outputs:
  create-discussion:
  missing-tool:
    create-discussion: true
---
Test workflow.`,
			expectedLine:     6,
			expectedProperty: "create-discussion",
			description:      "Original bug - error in nested missing-tool",
		},
		{
			name: "depth 3 - invalid property in MCP server config",
			workflowContent: `---
on: daily
tools:
  github:
    invalid-config: value
---
Test workflow.`,
			expectedLine:     5,
			expectedProperty: "invalid-config",
			description:      "Error at depth 3 in tools.github",
		},
		{
			name: "depth 2 - invalid property in permissions",
			workflowContent: `---
on: daily
permissions:
  invalid-perm: write
---
Test workflow.`,
			expectedLine:     4,
			expectedProperty: "invalid-perm",
			description:      "Error at depth 2 in permissions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory
			tmpDir := testutil.TempDir(t, "test-*")
			testFile := filepath.Join(tmpDir, "test.md")

			// Write the workflow content
			err := os.WriteFile(testFile, []byte(tt.workflowContent), 0644)
			if err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			// Extract frontmatter from the content
			result, err := ExtractFrontmatterFromContent(tt.workflowContent)
			if err != nil {
				t.Fatalf("Failed to extract frontmatter: %v", err)
			}

			// Validate the frontmatter - this will trigger schema validation with location
			err = ValidateMainWorkflowFrontmatterWithSchemaAndLocation(result.Frontmatter, testFile)

			// We expect an error
			if err == nil {
				t.Fatalf("Expected schema validation error, got nil. %s", tt.description)
			}

			// Check that the error message contains the expected line number
			errorStr := err.Error()
			expectedLocation := fmt.Sprintf("%s:%d:", filepath.Base(testFile), tt.expectedLine)

			if !strings.Contains(errorStr, expectedLocation) {
				t.Errorf("Expected error to contain '%s', got:\n%s\n%s", expectedLocation, errorStr, tt.description)
			}

			// Check that the error message contains the expected property name
			if !strings.Contains(errorStr, tt.expectedProperty) {
				t.Errorf("Expected error to contain property '%s', got:\n%s\n%s", tt.expectedProperty, errorStr, tt.description)
			}

			// Verify the error message is properly formatted with context
			if !strings.Contains(errorStr, "error:") {
				t.Errorf("Expected error to be formatted with 'error:', got:\n%s\n%s", errorStr, tt.description)
			}

			// Log the error for manual verification
			t.Logf("✓ Correct location (line %d) for %s", tt.expectedLine, tt.description)
		})
	}
}
