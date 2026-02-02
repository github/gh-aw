//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestSchemaValidationWithExamplesIntegration(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "schema-validation-test")

	tests := []struct {
		name          string
		workflowYAML  string
		expectedParts []string // Parts we expect in the error message
		shouldPass    bool
	}{
		{
			name: "valid workflow should pass",
			workflowYAML: `---
on: push
permissions:
  contents: read
---

# Valid Workflow
This is a valid workflow.
`,
			shouldPass: true,
		},
		// Note: Most schema violations would be caught during frontmatter parsing
		// before reaching the GitHub Actions schema validation.
		// The schema validation primarily catches issues in the generated YAML
		// that are structurally valid but violate GitHub Actions schema constraints.
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test workflow file
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.workflowYAML), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()
			compiler.SetSkipValidation(false) // Enable schema validation

			err := compiler.CompileWorkflow(testFile)

			if tt.shouldPass && err != nil {
				t.Errorf("Expected workflow to pass validation, but got error: %v", err)
			}

			if !tt.shouldPass && err == nil {
				t.Error("Expected workflow to fail validation, but it passed")
			}

			if err != nil && len(tt.expectedParts) > 0 {
				errStr := err.Error()
				for _, part := range tt.expectedParts {
					if !strings.Contains(errStr, part) {
						t.Errorf("Error message missing expected part %q. Full error: %v", part, err)
					}
				}
			}
		})
	}
}

func TestSchemaValidationExampleQuality(t *testing.T) {
	// Test that our example map has good quality examples
	tests := []struct {
		field    string
		example  string
		required []string // Parts that should be in the example
	}{
		{
			field:    "timeout-minutes",
			example:  getFieldExample("timeout-minutes", &mockValidationError{msg: "test"}),
			required: []string{"Example:", "timeout-minutes:", "10"},
		},
		{
			field:    "engine",
			example:  getFieldExample("engine", &mockValidationError{msg: "test"}),
			required: []string{"Valid engines are:", "copilot", "claude", "codex", "custom"},
		},
		{
			field:    "permissions",
			example:  getFieldExample("permissions", &mockValidationError{msg: "test"}),
			required: []string{"Example:", "permissions:", "contents:", "read", "issues:", "write"},
		},
		{
			field:    "runs-on",
			example:  getFieldExample("runs-on", &mockValidationError{msg: "test"}),
			required: []string{"Example:", "runs-on:", "ubuntu-latest"},
		},
		{
			field:    "concurrency",
			example:  getFieldExample("concurrency", &mockValidationError{msg: "test"}),
			required: []string{"Example:", "concurrency:"},
		},
		{
			field:    "jobs",
			example:  getFieldExample("jobs", &mockValidationError{msg: "test"}),
			required: []string{"Example:", "jobs:", "runs-on:", "steps:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			if tt.example == "" {
				t.Errorf("No example provided for field %q", tt.field)
				return
			}

			for _, required := range tt.required {
				if !strings.Contains(tt.example, required) {
					t.Errorf("Example for %q missing required part %q. Example: %s", tt.field, required, tt.example)
				}
			}
		})
	}
}
