//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
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
	// Set to dev mode to trigger checkout (dev is also the default)
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

	// Extract detection job section using existing helper
	detectionSection := extractJobSection(yaml, "detection")
	require.NotEmpty(t, detectionSection, "Detection job section should not be empty")

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
	// Set to release mode - no checkout needed
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

	// Extract detection job section using existing helper
	detectionSection := extractJobSection(yaml, "detection")
	require.NotEmpty(t, detectionSection, "Detection job section should not be empty")

	// In release mode, checkout should not be present in detection job
	assert.NotContains(t, detectionSection, "Checkout actions folder", "Detection job should not have checkout step in release mode")

	// Empty permissions are acceptable when no checkout is needed
	assert.Contains(t, detectionSection, "permissions: {}", "Detection job can have empty permissions in release mode")
}
