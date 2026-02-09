//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDetectionJobPermissionsWithCheckout verifies that detection job has
// contents: read permission when it includes a checkout step (dev/script mode)
func TestDetectionJobPermissionsWithCheckout(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	workflowPath := filepath.Join(tmpDir, "test-workflow.md")

	frontmatter := `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
safe-outputs:
  create-issue:
---

# Test

Create an issue.
`

	err := os.WriteFile(workflowPath, []byte(frontmatter), 0644)
	require.NoError(t, err, "Failed to write workflow file")

	compiler := NewCompiler()
	// Set to dev mode to trigger checkout
	compiler.actionMode = ActionModeDev

	err = compiler.CompileWorkflow(workflowPath)
	require.NoError(t, err, "Failed to compile workflow")

	// Read the compiled YAML
	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	yamlBytes, err := os.ReadFile(lockPath)
	require.NoError(t, err, "Failed to read compiled YAML")
	yaml := string(yamlBytes)

	// Check that detection job exists
	assert.Contains(t, yaml, "detection:", "Detection job not found in compiled YAML")

	// Check that detection job has checkout step
	assert.Contains(t, yaml, "Checkout actions folder", "Detection job should have checkout step in dev mode")

	// Extract detection job section
	detectionStart := strings.Index(yaml, "  detection:")
	require.Greater(t, detectionStart, 0, "Detection job not found")

	// Find the next job by looking for a line that starts with "  " followed by a lowercase letter and ":"
	// This matches job definitions like "  agent:", "  safe_outputs:", etc.
	searchStart := detectionStart + len("  detection:")
	nextJobPattern := "\n  "
	var detectionSection string

	// Search for the next job
	remaining := yaml[searchStart:]
	for {
		nextPos := strings.Index(remaining, nextJobPattern)
		if nextPos == -1 {
			// No more jobs, use rest of file
			detectionSection = yaml[detectionStart:]
			break
		}

		// Check if this is actually a job (starts with lowercase letter and has colon)
		lineStart := searchStart + nextPos + len(nextJobPattern)
		if lineStart < len(yaml) {
			nextChar := yaml[lineStart]
			// Check if it looks like a job name (lowercase letter)
			if nextChar >= 'a' && nextChar <= 'z' {
				// Found next job
				detectionSection = yaml[detectionStart : searchStart+nextPos]
				break
			}
		}

		// Keep searching
		remaining = remaining[nextPos+1:]
		searchStart += nextPos + 1
	}

	// Verify that detection job has contents: read permission
	assert.Contains(t, detectionSection, "permissions:", "Detection job should have permissions field")
	assert.Contains(t, detectionSection, "contents: read", "Detection job should have contents: read permission when checkout is needed")

	// Verify it's NOT using empty permissions
	assert.NotContains(t, detectionSection, "permissions: {}", "Detection job should not have empty permissions when checkout is needed")
}

// TestDetectionJobPermissionsWithoutCheckout verifies that detection job has
// empty permissions when no checkout is needed (release mode)
func TestDetectionJobPermissionsWithoutCheckout(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	workflowPath := filepath.Join(tmpDir, "test-workflow.md")

	frontmatter := `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
safe-outputs:
  create-issue:
---

# Test

Create an issue.
`

	err := os.WriteFile(workflowPath, []byte(frontmatter), 0644)
	require.NoError(t, err, "Failed to write workflow file")

	compiler := NewCompiler()
	// Set to release mode (default) - no checkout needed
	compiler.actionMode = ActionModeRelease

	err = compiler.CompileWorkflow(workflowPath)
	require.NoError(t, err, "Failed to compile workflow")

	// Read the compiled YAML
	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	yamlBytes, err := os.ReadFile(lockPath)
	require.NoError(t, err, "Failed to read compiled YAML")
	yaml := string(yamlBytes)

	// Check that detection job exists
	assert.Contains(t, yaml, "detection:", "Detection job not found in compiled YAML")

	// Check that detection job does NOT have checkout step in release mode
	detectionStart := strings.Index(yaml, "  detection:")
	require.Greater(t, detectionStart, 0, "Detection job not found")

	// Find the next job by looking for a line that starts with "  " followed by a lowercase letter and ":"
	searchStart := detectionStart + len("  detection:")
	nextJobPattern := "\n  "
	var detectionSection string

	// Search for the next job
	remaining := yaml[searchStart:]
	for {
		nextPos := strings.Index(remaining, nextJobPattern)
		if nextPos == -1 {
			// No more jobs, use rest of file
			detectionSection = yaml[detectionStart:]
			break
		}

		// Check if this is actually a job (starts with lowercase letter and has colon)
		lineStart := searchStart + nextPos + len(nextJobPattern)
		if lineStart < len(yaml) {
			nextChar := yaml[lineStart]
			// Check if it looks like a job name (lowercase letter)
			if nextChar >= 'a' && nextChar <= 'z' {
				// Found next job
				detectionSection = yaml[detectionStart : searchStart+nextPos]
				break
			}
		}

		// Keep searching
		remaining = remaining[nextPos+1:]
		searchStart += nextPos + 1
	}

	// In release mode, checkout should not be present in detection job
	assert.NotContains(t, detectionSection, "Checkout actions folder", "Detection job should not have checkout step in release mode")

	// Empty permissions are acceptable when no checkout is needed
	assert.Contains(t, detectionSection, "permissions: {}", "Detection job can have empty permissions in release mode")
}
