//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// timestampDifferentiationDelay is the delay used in tests to ensure
// filesystem timestamps would be different if a file is written
const timestampDifferentiationDelay = 100 * time.Millisecond

// TestCompilerSkipsWriteWhenContentUnchanged verifies that the compiler skips writing
// the lock file when the content hasn't changed, preserving the timestamp.
// This prevents unnecessary git diffs when only markdown content (not frontmatter) changes.
func TestCompilerSkipsWriteWhenContentUnchanged(t *testing.T) {
	// Create temporary directory for test
	tmpDir := t.TempDir()
	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	lockPath := filepath.Join(tmpDir, "test-workflow.lock.yml")

	// Create initial workflow with frontmatter
	workflowContent := `---
engine: copilot
on: issues
permissions:
  issues: read
---

# Test Workflow

This is the initial markdown content.
`
	err := os.WriteFile(workflowPath, []byte(workflowContent), 0644)
	require.NoError(t, err, "Failed to create workflow file")

	// Compile initial workflow
	compiler := NewCompiler()
	compiler.SetQuiet(true) // Suppress output for cleaner test logs
	err = compiler.CompileWorkflow(workflowPath)
	require.NoError(t, err, "Initial compilation failed")

	// Verify lock file was created
	require.FileExists(t, lockPath, "Lock file should exist after initial compilation")

	// Get initial lock file info
	initialInfo, err := os.Stat(lockPath)
	require.NoError(t, err, "Failed to stat lock file")
	initialModTime := initialInfo.ModTime()

	// Wait a bit to ensure timestamp would be different if file is written
	time.Sleep(timestampDifferentiationDelay)

	// Change ONLY the markdown content (not frontmatter)
	workflowContentV2 := `---
engine: copilot
on: issues
permissions:
  issues: read
---

# Test Workflow

This is DIFFERENT markdown content that should not affect the lock file.
`
	err = os.WriteFile(workflowPath, []byte(workflowContentV2), 0644)
	require.NoError(t, err, "Failed to update workflow file")

	// Recompile workflow
	compiler2 := NewCompiler()
	compiler2.SetQuiet(true)
	err = compiler2.CompileWorkflow(workflowPath)
	require.NoError(t, err, "Recompilation failed")

	// Check lock file timestamp - should be UNCHANGED
	afterInfo, err := os.Stat(lockPath)
	require.NoError(t, err, "Failed to stat lock file after recompilation")
	afterModTime := afterInfo.ModTime()

	assert.Equal(t, initialModTime, afterModTime,
		"Lock file timestamp should be preserved when content is unchanged")
}

// TestCompilerWritesWhenContentChanged verifies that the compiler DOES write
// the lock file when the frontmatter changes, updating the timestamp.
func TestCompilerWritesWhenContentChanged(t *testing.T) {
	// Create temporary directory for test
	tmpDir := t.TempDir()
	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	lockPath := filepath.Join(tmpDir, "test-workflow.lock.yml")

	// Create initial workflow
	workflowContent := `---
engine: copilot
on: issues
---

# Test Workflow

This is the initial markdown content.
`
	err := os.WriteFile(workflowPath, []byte(workflowContent), 0644)
	require.NoError(t, err, "Failed to create workflow file")

	// Compile initial workflow
	compiler := NewCompiler()
	compiler.SetQuiet(true)
	err = compiler.CompileWorkflow(workflowPath)
	require.NoError(t, err, "Initial compilation failed")

	// Verify lock file was created
	require.FileExists(t, lockPath, "Lock file should exist after initial compilation")

	// Get initial lock file info
	initialInfo, err := os.Stat(lockPath)
	require.NoError(t, err, "Failed to stat lock file")
	initialModTime := initialInfo.ModTime()

	// Wait to ensure timestamp will be different
	time.Sleep(timestampDifferentiationDelay)

	// Change the FRONTMATTER (add permissions)
	workflowContentV2 := `---
engine: copilot
on: issues
permissions:
  issues: read
---

# Test Workflow

This is the initial markdown content.
`
	err = os.WriteFile(workflowPath, []byte(workflowContentV2), 0644)
	require.NoError(t, err, "Failed to update workflow file")

	// Recompile workflow
	compiler2 := NewCompiler()
	compiler2.SetQuiet(true)
	err = compiler2.CompileWorkflow(workflowPath)
	require.NoError(t, err, "Recompilation failed")

	// Check lock file timestamp - should be CHANGED
	afterInfo, err := os.Stat(lockPath)
	require.NoError(t, err, "Failed to stat lock file after recompilation")
	afterModTime := afterInfo.ModTime()

	assert.True(t, afterModTime.After(initialModTime),
		"Lock file timestamp should be updated when content changes")
}

// TestCompilerWritesWhenLockFileMissing verifies that the compiler writes
// the lock file when it doesn't exist.
func TestCompilerWritesWhenLockFileMissing(t *testing.T) {
	// Create temporary directory for test
	tmpDir := t.TempDir()
	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	lockPath := filepath.Join(tmpDir, "test-workflow.lock.yml")

	// Create workflow
	workflowContent := `---
engine: copilot
on: issues
---

# Test Workflow

Initial content.
`
	err := os.WriteFile(workflowPath, []byte(workflowContent), 0644)
	require.NoError(t, err, "Failed to create workflow file")

	// Compile workflow (lock file doesn't exist yet)
	compiler := NewCompiler()
	compiler.SetQuiet(true)
	err = compiler.CompileWorkflow(workflowPath)
	require.NoError(t, err, "Initial compilation failed")

	// Verify lock file was created
	require.FileExists(t, lockPath, "Lock file should be created when it doesn't exist")
}
