//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

// TestActivationJobAlwaysDeclaresCommentOutputs ensures that the activation job
// always declares comment_id and comment_repo outputs to avoid actionlint errors
func TestActivationJobAlwaysDeclaresCommentOutputs(t *testing.T) {
	tests := []struct {
		name       string
		aiReaction string
	}{
		{
			name:       "workflow with reaction",
			aiReaction: "eyes",
		},
		{
			name:       "workflow without reaction",
			aiReaction: "",
		},
		{
			name:       "workflow with reaction=none",
			aiReaction: "none",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for test files
			tempDir := t.TempDir()

			// Create a test workflow with safe outputs
			workflowContent := `---
on:
  issues:
    types: [opened]
`
			if tt.aiReaction != "" {
				workflowContent += "  reaction: " + tt.aiReaction + "\n"
			}
			workflowContent += `permissions:
  contents: read
safe-outputs:
  create-issue:
---

# Test workflow

Test workflow content.
`

			workflowPath := filepath.Join(tempDir, "test-workflow.md")
			err := os.WriteFile(workflowPath, []byte(workflowContent), 0644)
			require.NoError(t, err)

			// Compile the workflow
			compiler := NewCompiler()
			err = compiler.CompileWorkflow(workflowPath)
			require.NoError(t, err)

			lockPath := stringutil.MarkdownToLockFile(workflowPath)

			// Read the compiled lock file
			lockContent, err := os.ReadFile(lockPath)
			require.NoError(t, err)

			// Parse the YAML
			var workflow map[string]any
			err = yaml.Unmarshal(lockContent, &workflow)
			require.NoError(t, err)

			// Get the jobs
			jobs, ok := workflow["jobs"].(map[string]any)
			require.True(t, ok, "jobs should be a map")

			// Get the activation job
			activationJob, ok := jobs["activation"].(map[string]any)
			require.True(t, ok, "activation job should exist")

			// Get the outputs
			outputs, ok := activationJob["outputs"].(map[string]any)
			require.True(t, ok, "activation job should have outputs")

			// Check that comment_id and comment_repo are declared
			_, hasCommentID := outputs["comment_id"]
			require.True(t, hasCommentID, "activation job should declare comment_id output")

			_, hasCommentRepo := outputs["comment_repo"]
			require.True(t, hasCommentRepo, "activation job should declare comment_repo output")

			// Verify the lock file doesn't have actionlint errors
			lockContentStr := string(lockContent)
			require.NotContains(t, lockContentStr, "property \"comment_id\" is not defined")
			require.NotContains(t, lockContentStr, "property \"comment_repo\" is not defined")
		})
	}
}
