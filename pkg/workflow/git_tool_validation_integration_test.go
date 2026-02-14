//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompilerDetectsGitToolRequirement(t *testing.T) {
	tests := []struct {
		name          string
		workflow      string
		expectError   bool
		errorContains string
	}{
		{
			name: "create-pull-request without bash tool - OK (defaults apply)",
			workflow: `---
name: Test Create PR Without Git
engine: copilot
on: workflow_dispatch
safe-outputs:
  create-pull-request:
    title-prefix: "[auto] "
---

Test workflow that uses create-pull-request without bash tool.
`,
			expectError: false,
		},
		{
			name: "create-pull-request with bash: false - error",
			workflow: `---
name: Test Create PR With Bash False
engine: copilot
on: workflow_dispatch
tools:
  bash: false
safe-outputs:
  create-pull-request:
    title-prefix: "[auto] "
---

Test workflow that uses create-pull-request with bash explicitly disabled.
`,
			expectError:   true,
			errorContains: "create-pull-request but git tool is not allowed",
		},
		{
			name: "create-pull-request with bash: [echo] - error",
			workflow: `---
name: Test Create PR With Limited Bash
engine: copilot
on: workflow_dispatch
tools:
  bash: ["echo", "ls"]
safe-outputs:
  create-pull-request:
    title-prefix: "[auto] "
---

Test workflow that uses create-pull-request without git in allowed commands.
`,
			expectError:   true,
			errorContains: "create-pull-request but git tool is not allowed",
		},
		{
			name: "create-pull-request with bash: true - valid",
			workflow: `---
name: Test Create PR With Bash True
engine: copilot
on: workflow_dispatch
tools:
  bash: true
safe-outputs:
  create-pull-request:
    title-prefix: "[auto] "
---

Test workflow that uses create-pull-request with bash enabled.
`,
			expectError: false,
		},
		{
			name: "create-pull-request with bash: [git] - valid",
			workflow: `---
name: Test Create PR With Git
engine: copilot
on: workflow_dispatch
tools:
  bash: ["git"]
safe-outputs:
  create-pull-request:
    title-prefix: "[auto] "
---

Test workflow that uses create-pull-request with git explicitly allowed.
`,
			expectError: false,
		},
		{
			name: "create-pull-request with bash: [*] - valid",
			workflow: `---
name: Test Create PR With Wildcard
engine: copilot
on: workflow_dispatch
tools:
  bash: ["*"]
safe-outputs:
  create-pull-request:
    title-prefix: "[auto] "
---

Test workflow that uses create-pull-request with wildcard bash.
`,
			expectError: false,
		},
		{
			name: "push-to-pull-request-branch without bash - OK (defaults apply)",
			workflow: `---
name: Test Push To Branch Without Git
engine: copilot
on: workflow_dispatch
safe-outputs:
  push-to-pull-request-branch:
    title-prefix: "[auto] "
---

Test workflow that uses push-to-pull-request-branch without bash tool.
`,
			expectError: false,
		},
		{
			name: "push-to-pull-request-branch with bash: true - valid",
			workflow: `---
name: Test Push To Branch With Bash
engine: copilot
on: workflow_dispatch
tools:
  bash: true
safe-outputs:
  push-to-pull-request-branch:
    title-prefix: "[auto] "
---

Test workflow that uses push-to-pull-request-branch with bash enabled.
`,
			expectError: false,
		},
		{
			name: "both create-pull-request and push-to-pull-request-branch without bash - OK (defaults apply)",
			workflow: `---
name: Test Both PR Features Without Git
engine: copilot
on: workflow_dispatch
safe-outputs:
  create-pull-request:
    title-prefix: "[auto] "
  push-to-pull-request-branch:
    title-prefix: "[auto] "
---

Test workflow that uses both PR features without bash tool.
`,
			expectError: false,
		},
		{
			name: "workflow without PR features - no validation",
			workflow: `---
name: Test Without PR Features
engine: copilot
on: workflow_dispatch
safe-outputs:
  create-issue:
    title-prefix: "[auto] "
---

Test workflow that doesn't use PR features.
`,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for the test
			tmpDir := t.TempDir()
			workflowPath := filepath.Join(tmpDir, "test-workflow.md")

			// Write the workflow file
			err := os.WriteFile(workflowPath, []byte(tt.workflow), 0644)
			require.NoError(t, err, "Failed to write test workflow file")

			// Create a compiler instance
			compiler := NewCompiler()

			// Try to compile the workflow
			_, err = compiler.ParseWorkflowFile(workflowPath)

			if tt.expectError {
				require.Error(t, err, "Expected compilation error")
				assert.Contains(t, err.Error(), tt.errorContains, "Error should contain expected message")

				// Verify error message includes helpful suggestions
				assert.True(t,
					strings.Contains(err.Error(), "bash: true") ||
						strings.Contains(err.Error(), "bash: [\"git\"]") ||
						strings.Contains(err.Error(), "bash: [\"*\"]"),
					"Error message should include helpful suggestions")
			} else {
				assert.NoError(t, err, "Expected successful compilation")
			}
		})
	}
}

func TestCompilerGitToolValidationWithImports(t *testing.T) {
	t.Run("workflow with create-pull-request and imported bash tool", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create an import file with bash tool configuration
		importPath := filepath.Join(tmpDir, "import.md")
		importContent := `---
tools:
  bash: true
---

Imported configuration with bash enabled.
`
		err := os.WriteFile(importPath, []byte(importContent), 0644)
		require.NoError(t, err, "Failed to write import file")

		// Create main workflow that imports the bash configuration
		workflowPath := filepath.Join(tmpDir, "main-workflow.md")
		workflowContent := `---
name: Test With Import
engine: copilot
on: workflow_dispatch
imports:
  - ./import.md
safe-outputs:
  create-pull-request:
    title-prefix: "[auto] "
---

Test workflow that imports bash configuration.
`
		err = os.WriteFile(workflowPath, []byte(workflowContent), 0644)
		require.NoError(t, err, "Failed to write main workflow file")

		// Compile the workflow
		compiler := NewCompiler()
		_, err = compiler.ParseWorkflowFile(workflowPath)

		// Should succeed because bash is imported
		assert.NoError(t, err, "Expected successful compilation with imported bash tool")
	})
}
