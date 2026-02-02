//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"

	"github.com/github/gh-aw/pkg/constants"
)

func TestDefaultTimeoutMinutesApplied(t *testing.T) {
	tests := []struct {
		name            string
		frontmatter     string
		expectedTimeout int
		description     string
	}{
		{
			name: "no timeout specified - should use default",
			frontmatter: `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
---`,
			expectedTimeout: int(constants.DefaultAgenticWorkflowTimeout / time.Minute),
			description:     "When timeout-minutes is not specified, default should be applied",
		},
		{
			name: "explicit timeout specified - should use explicit value",
			frontmatter: `---
on: workflow_dispatch
permissions:
  contents: read
timeout-minutes: 30
engine: copilot
---`,
			expectedTimeout: 30,
			description:     "When timeout-minutes is explicitly specified, that value should be used",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test
			tmpDir := testutil.TempDir(t, "timeout-default-test")

			// Create test workflow file
			testContent := tt.frontmatter + "\n\n# Test Workflow\n\nTest workflow for timeout-minutes default behavior.\n"
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
				t.Fatal(err)
			}

			// Compile the workflow
			compiler := NewCompiler()
			if err := compiler.CompileWorkflow(testFile); err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			// Read the generated lock file
			lockFile := stringutil.MarkdownToLockFile(testFile)
			lockContent, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}

			// Check that the expected timeout is present in the lock file
			expectedTimeoutStr := "timeout-minutes: " + string(rune(tt.expectedTimeout+'0'))
			if tt.expectedTimeout >= 10 {
				expectedTimeoutStr = "timeout-minutes: " + intToString(tt.expectedTimeout)
			}

			if !strings.Contains(string(lockContent), expectedTimeoutStr) {
				t.Errorf("%s\nExpected timeout-minutes: %d in compiled workflow, but not found\nLock file content:\n%s",
					tt.description, tt.expectedTimeout, string(lockContent))
			}

			// Verify the timeout appears in the execution step (not just anywhere in the file)
			// The timeout should be in a step, not in comments
			lines := strings.Split(string(lockContent), "\n")
			foundTimeoutInStep := false
			for _, line := range lines {
				trimmed := strings.TrimSpace(line)
				// Skip comments
				if strings.HasPrefix(trimmed, "#") {
					continue
				}
				if strings.Contains(trimmed, expectedTimeoutStr) {
					foundTimeoutInStep = true
					break
				}
			}

			if !foundTimeoutInStep {
				t.Errorf("%s\nExpected timeout-minutes: %d in a workflow step (not in comments)\nLock file content:\n%s",
					tt.description, tt.expectedTimeout, string(lockContent))
			}
		})
	}
}

// Helper function to convert int to string
func intToString(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + intToString(-n)
	}
	result := ""
	for n > 0 {
		result = string(rune('0'+n%10)) + result
		n /= 10
	}
	return result
}

func TestDefaultTimeoutMinutesConstantValue(t *testing.T) {
	// This test ensures the constant is set to the expected value
	// If this test fails, it means the constant was changed and documentation
	// should be updated accordingly
	expectedDefault := 20
	actualDefault := int(constants.DefaultAgenticWorkflowTimeout / time.Minute)
	if actualDefault != expectedDefault {
		t.Errorf("DefaultAgenticWorkflowTimeout constant is %d minutes, but test expects %d. "+
			"If you changed the constant, please update the schema documentation in pkg/parser/schemas/main_workflow_schema.json",
			actualDefault, expectedDefault)
	}
}

func TestSchemaDocumentationMatchesConstant(t *testing.T) {
	// Read the schema file
	schemaPath := filepath.Join("..", "parser", "schemas", "main_workflow_schema.json")
	schemaContent, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("Failed to read schema file: %v", err)
	}

	// Check that the schema mentions the correct default value
	expectedText := "Defaults to 20 minutes for agentic workflows"
	if !strings.Contains(string(schemaContent), expectedText) {
		t.Errorf("Schema documentation does not mention the correct default timeout.\n"+
			"Expected to find: %q\n"+
			"Please update the timeout-minutes description in %s to match DefaultAgenticWorkflowTimeout constant (%d minutes)",
			expectedText, schemaPath, int(constants.DefaultAgenticWorkflowTimeout/time.Minute))
	}

	// Count occurrences - should appear exactly once (only timeout-minutes, timeout_minutes removed)
	occurrences := strings.Count(string(schemaContent), expectedText)
	if occurrences != 1 {
		t.Errorf("Expected to find exactly 1 occurrence of %q in schema (for timeout-minutes field only), but found %d",
			expectedText, occurrences)
	}
}
