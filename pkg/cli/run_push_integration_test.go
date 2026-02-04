//go:build integration

package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCollectWorkflowFiles_NoLockFile tests that when a workflow has no lock file,
// the collectWorkflowFiles function compiles the workflow and creates one.
// This is an integration test because it invokes the full workflow compilation.
func TestCollectWorkflowFiles_NoLockFile(t *testing.T) {

	// Create a temporary directory for testing
	tmpDir := t.TempDir()

	// Create a simple workflow file without a lock file
	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	workflowContent := `---
name: Test Workflow
on: workflow_dispatch
---
# Test Workflow
This is a test workflow without a lock file.
`
	err := os.WriteFile(workflowPath, []byte(workflowContent), 0644)
	require.NoError(t, err)

	// Test collecting files - should now compile the workflow and create lock file
	files, err := collectWorkflowFiles(context.Background(), workflowPath, false)
	require.NoError(t, err)
	assert.Len(t, files, 2, "Should collect workflow .md file and auto-generate lock file")

	// Check that both workflow file and lock file are in the result
	fileSet := make(map[string]bool)
	for _, file := range files {
		fileSet[file] = true
	}
	assert.True(t, fileSet[workflowPath], "Should include workflow .md file")

	lockFilePath := stringutil.MarkdownToLockFile(workflowPath)
	assert.True(t, fileSet[lockFilePath], "Should include auto-generated lock .yml file")
}
