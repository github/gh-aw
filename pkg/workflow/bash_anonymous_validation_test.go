//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompilerRejectsAnonymousBashSyntax(t *testing.T) {
	// Create a temporary directory for test workflows
	tmpDir := t.TempDir()

	// Create a test workflow with anonymous bash syntax
	workflowContent := `---
name: Test Workflow
engine: copilot
on:
  workflow_dispatch:
tools:
  bash:
  github:
---
# Test workflow
This is a test workflow with anonymous bash syntax.
`

	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	err := os.WriteFile(workflowPath, []byte(workflowContent), 0644)
	require.NoError(t, err, "Failed to create test workflow file")

	// Create compiler
	compiler := NewCompiler()
	compiler.SetSkipValidation(false) // Enable validation

	// Try to compile - should fail
	err = compiler.CompileWorkflow(workflowPath)

	// Verify that compilation fails with the expected error
	require.Error(t, err, "Compilation should fail for anonymous bash syntax")
	assert.Contains(t, err.Error(), "anonymous syntax 'bash:' is not supported", "Error should mention anonymous syntax")
	assert.Contains(t, err.Error(), "bash: true", "Error should suggest bash: true")
	assert.Contains(t, err.Error(), "bash: false", "Error should suggest bash: false")
	assert.Contains(t, err.Error(), "gh aw fix", "Error should suggest using gh aw fix")
}

func TestCompilerAcceptsExplicitBashSyntax(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name       string
		bashConfig string
	}{
		{
			name:       "bash: true",
			bashConfig: "bash: true",
		},
		{
			name:       "bash: false",
			bashConfig: "bash: false",
		},
		{
			name:       "bash with array",
			bashConfig: "bash: [\"echo\", \"ls\"]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowContent := `---
name: Test Workflow
engine: copilot
on:
  workflow_dispatch:
tools:
  ` + tt.bashConfig + `
  github:
---
# Test workflow
This is a test workflow.
`

			workflowPath := filepath.Join(tmpDir, strings.ReplaceAll(tt.name, " ", "-")+".md")
			err := os.WriteFile(workflowPath, []byte(workflowContent), 0644)
			require.NoError(t, err, "Failed to create test workflow file")

			compiler := NewCompiler()
			compiler.SetSkipValidation(false)

			// Should compile successfully
			err = compiler.CompileWorkflow(workflowPath)
			assert.NoError(t, err, "Compilation should succeed for explicit bash syntax: %s", tt.bashConfig)
		})
	}
}
