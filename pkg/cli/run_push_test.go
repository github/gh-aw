//go:build !integration

package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectWorkflowFiles_SimpleWorkflow(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()

	// Create a simple workflow file
	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	workflowContent := `---
name: Test Workflow
on: workflow_dispatch
---
# Test Workflow
This is a test workflow.
`
	err := os.WriteFile(workflowPath, []byte(workflowContent), 0644)
	require.NoError(t, err)

	// Create the corresponding lock file
	lockFilePath := filepath.Join(tmpDir, "test-workflow.lock.yml")
	lockContent := `name: Test Workflow
on: workflow_dispatch
`
	err = os.WriteFile(lockFilePath, []byte(lockContent), 0644)
	require.NoError(t, err)

	// Test collecting files
	files, err := collectWorkflowFiles(context.Background(), workflowPath, false, false)
	require.NoError(t, err)
	assert.Len(t, files, 2, "Should collect workflow .md and .lock.yml files")

	// Check that both files are in the result
	fileSet := make(map[string]bool)
	for _, file := range files {
		fileSet[file] = true
	}
	assert.True(t, fileSet[workflowPath], "Should include workflow .md file")
	assert.True(t, fileSet[lockFilePath], "Should include lock .yml file")
}

func TestCollectWorkflowFiles_MissingLockMessage(t *testing.T) {
	tmpDir := t.TempDir()
	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	workflowContent := `---
name: Test Workflow
on: workflow_dispatch
---
# Test Workflow
This is a test workflow.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0644))

	var files []string
	_, stderr := captureOutput(t, func() error {
		var err error
		files, err = collectWorkflowFiles(context.Background(), workflowPath, true, false)
		return err
	})

	assert.Contains(t, stderr, "Lock file not found, compiling workflow...")
	assert.Contains(t, files, workflowPath)
	assert.Contains(t, files, filepath.Join(tmpDir, "test-workflow.lock.yml"))
}

func TestCollectWorkflowFiles_WithImports(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()

	// Create a shared file
	sharedPath := filepath.Join(tmpDir, "shared.md")
	sharedContent := `# Shared Content
This is shared content.
`
	err := os.WriteFile(sharedPath, []byte(sharedContent), 0644)
	require.NoError(t, err)

	// Create a workflow file that imports the shared file
	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	workflowContent := `---
name: Test Workflow
on: workflow_dispatch
imports:
  - shared.md
---
# Test Workflow
This workflow imports shared content.
`
	err = os.WriteFile(workflowPath, []byte(workflowContent), 0644)
	require.NoError(t, err)

	// Create the corresponding lock file
	lockFilePath := filepath.Join(tmpDir, "test-workflow.lock.yml")
	lockContent := `name: Test Workflow
on: workflow_dispatch
`
	err = os.WriteFile(lockFilePath, []byte(lockContent), 0644)
	require.NoError(t, err)

	// Test collecting files
	files, err := collectWorkflowFiles(context.Background(), workflowPath, false, false)
	require.NoError(t, err)
	assert.Len(t, files, 3, "Should collect workflow, lock, and imported files")

	// Check that all files are in the result
	fileSet := make(map[string]bool)
	for _, file := range files {
		fileSet[file] = true
	}
	assert.True(t, fileSet[workflowPath], "Should include workflow .md file")
	assert.True(t, fileSet[lockFilePath], "Should include lock .yml file")
	assert.True(t, fileSet[sharedPath], "Should include imported shared.md file")
}

func TestCollectWorkflowFiles_TransitiveImports(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()

	// Create base shared file
	baseSharedPath := filepath.Join(tmpDir, "base-shared.md")
	baseSharedContent := `# Base Shared Content
This is base shared content.
`
	err := os.WriteFile(baseSharedPath, []byte(baseSharedContent), 0644)
	require.NoError(t, err)

	// Create intermediate shared file that imports base
	intermediateSharedPath := filepath.Join(tmpDir, "intermediate-shared.md")
	intermediateSharedContent := `---
imports:
  - base-shared.md
---
# Intermediate Shared Content
This imports base shared.
`
	err = os.WriteFile(intermediateSharedPath, []byte(intermediateSharedContent), 0644)
	require.NoError(t, err)

	// Create a workflow file that imports the intermediate file
	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	workflowContent := `---
name: Test Workflow
on: workflow_dispatch
imports:
  - intermediate-shared.md
---
# Test Workflow
This workflow imports intermediate shared content.
`
	err = os.WriteFile(workflowPath, []byte(workflowContent), 0644)
	require.NoError(t, err)

	// Create the corresponding lock file
	lockFilePath := filepath.Join(tmpDir, "test-workflow.lock.yml")
	lockContent := `name: Test Workflow
on: workflow_dispatch
`
	err = os.WriteFile(lockFilePath, []byte(lockContent), 0644)
	require.NoError(t, err)

	// Test collecting files
	files, err := collectWorkflowFiles(context.Background(), workflowPath, false, false)
	require.NoError(t, err)
	assert.Len(t, files, 4, "Should collect workflow, lock, and all transitive imports")

	// Check that all files are in the result
	fileSet := make(map[string]bool)
	for _, file := range files {
		fileSet[file] = true
	}
	assert.True(t, fileSet[workflowPath], "Should include workflow .md file")
	assert.True(t, fileSet[lockFilePath], "Should include lock .yml file")
	assert.True(t, fileSet[intermediateSharedPath], "Should include intermediate-shared.md file")
	assert.True(t, fileSet[baseSharedPath], "Should include base-shared.md file")
}

func TestResolveImportPath_RunPushOpts_LocalPathSemantics(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()
	baseDir := filepath.Join(tmpDir, "workflows")
	err := os.MkdirAll(baseDir, 0755)
	require.NoError(t, err)

	tests := []struct {
		name       string
		importPath string
		baseDir    string
		expected   string
	}{
		{
			name:       "relative path",
			importPath: "shared/file.md",
			baseDir:    baseDir,
			expected:   filepath.Join(baseDir, "shared/file.md"),
		},
		{
			name:       "path with section",
			importPath: "shared/file.md#section",
			baseDir:    baseDir,
			expected:   filepath.Join(baseDir, "shared/file.md"),
		},
		{
			name:       "workflowspec format with @",
			importPath: "owner/repo/path/file.md@abc123",
			baseDir:    baseDir,
			expected:   "",
		},
		{
			name:       "workflowspec without @ is treated as remote workflowspec",
			importPath: "owner/repo/path/file.md",
			baseDir:    baseDir,
			expected:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveImportPath(tt.importPath, tt.baseDir, importPathRunPushOpts)
			assert.Equal(t, tt.expected, result, "resolveImportPath(%q, %q, importPathRunPushOpts) = %v, want %v", tt.importPath, tt.baseDir, result, tt.expected)
		})
	}
}

func TestCollectWorkflowFiles_WithOutdatedLockFile(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()

	// Create a workflow file
	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	workflowContent := `---
name: Test Workflow
on: workflow_dispatch
---
# Test Workflow
This is a test workflow.
`
	err := os.WriteFile(workflowPath, []byte(workflowContent), 0644)
	require.NoError(t, err)

	// Create an old lock file (simulate outdated)
	lockFilePath := filepath.Join(tmpDir, "test-workflow.lock.yml")
	lockContent := `name: Test Workflow
on: workflow_dispatch
`
	err = os.WriteFile(lockFilePath, []byte(lockContent), 0644)
	require.NoError(t, err)

	// Make the workflow file newer by sleeping and touching it
	time.Sleep(100 * time.Millisecond)
	currentTime := time.Now()
	err = os.Chtimes(workflowPath, currentTime, currentTime)
	require.NoError(t, err)

	// Verify the lock file is older
	mdStat, err := os.Stat(workflowPath)
	require.NoError(t, err)
	lockStat, err := os.Stat(lockFilePath)
	require.NoError(t, err)
	assert.True(t, mdStat.ModTime().After(lockStat.ModTime()), "Workflow file should be newer than lock file")

	// Note: We can't actually test recompilation here without a full compilation setup,
	// but we can verify the detection logic works
	// The actual compilation would happen in an integration test
}

func TestCollectWorkflowFiles_WithFrontmatterHashMismatch(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()

	// Create a workflow file
	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	workflowContent := `---
name: Test Workflow
engine: copilot
on: workflow_dispatch
---
# Test Workflow
This is a test workflow.
Use env variable: ${{ env.MY_VAR }}
`
	err := os.WriteFile(workflowPath, []byte(workflowContent), 0644)
	require.NoError(t, err)

	// Compute the correct hash for this workflow
	cache := parser.NewImportCache("")
	correctHash, err := parser.ComputeFrontmatterHashFromFile(workflowPath, cache)
	require.NoError(t, err)
	require.NotEmpty(t, correctHash)

	// Create a lock file with an incorrect hash (simulating stale frontmatter)
	lockFilePath := filepath.Join(tmpDir, "test-workflow.lock.yml")
	wrongHash := "0000000000000000000000000000000000000000000000000000000000000000"
	lockContent := fmt.Sprintf(`# frontmatter-hash: %s

name: Test Workflow
"on": workflow_dispatch
jobs:
  agent:
    runs-on: ubuntu-latest
    steps:
      - name: Test
        run: echo "test"
`, wrongHash)
	err = os.WriteFile(lockFilePath, []byte(lockContent), 0644)
	require.NoError(t, err)

	// Make the lock file slightly newer than the workflow file
	// This ensures we're testing hash comparison, not mtime comparison
	time.Sleep(100 * time.Millisecond)
	futureTime := time.Now().Add(1 * time.Hour)
	err = os.Chtimes(lockFilePath, futureTime, futureTime)
	require.NoError(t, err)

	// Verify the lock file is newer (mtime check would pass)
	mdStat, err := os.Stat(workflowPath)
	require.NoError(t, err)
	lockStat, err := os.Stat(lockFilePath)
	require.NoError(t, err)
	assert.True(t, lockStat.ModTime().After(mdStat.ModTime()), "Lock file should be newer than workflow file for this test")

	// Test the hash mismatch detection
	mismatch, err := checkFrontmatterHashMismatch(workflowPath, lockFilePath)
	require.NoError(t, err)
	assert.True(t, mismatch, "Should detect hash mismatch")

	// Now test with matching hash
	lockContentCorrect := fmt.Sprintf(`# frontmatter-hash: %s

name: Test Workflow
"on": workflow_dispatch
jobs:
  agent:
    runs-on: ubuntu-latest
    steps:
      - name: Test
        run: echo "test"
`, correctHash)
	err = os.WriteFile(lockFilePath, []byte(lockContentCorrect), 0644)
	require.NoError(t, err)

	// Test that matching hash is detected
	mismatch, err = checkFrontmatterHashMismatch(workflowPath, lockFilePath)
	require.NoError(t, err)
	assert.False(t, mismatch, "Should not detect mismatch when hashes match")
}

func TestCheckFrontmatterHashMismatch_JSONMetadataFormat(t *testing.T) {
	// Test that the new JSON metadata format is correctly parsed
	tmpDir := t.TempDir()

	// Create a workflow file
	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	workflowContent := `---
name: Test Workflow JSON
engine: copilot
on: workflow_dispatch
---
# Test Workflow
This is a test workflow for JSON metadata format.
`
	err := os.WriteFile(workflowPath, []byte(workflowContent), 0644)
	require.NoError(t, err)

	// Compute the correct hash for this workflow
	cache := parser.NewImportCache("")
	correctHash, err := parser.ComputeFrontmatterHashFromFile(workflowPath, cache)
	require.NoError(t, err)
	require.NotEmpty(t, correctHash)

	// Create a lock file with JSON metadata format (as written by current compiler)
	// Uses snake_case field names matching the Go struct JSON tags
	lockFilePath := filepath.Join(tmpDir, "test-workflow.lock.yml")
	lockContent := fmt.Sprintf(`# This workflow was auto-generated by gh-aw
#
# gh-aw-metadata: {"schema_version":"v1","frontmatter_hash":"%s","stop_time":""}

name: Test Workflow JSON
"on": workflow_dispatch
jobs:
  agent:
    runs-on: ubuntu-latest
    steps:
      - name: Test
        run: echo "test"
`, correctHash)
	err = os.WriteFile(lockFilePath, []byte(lockContent), 0644)
	require.NoError(t, err)

	// Test that matching hash in JSON format is correctly detected
	mismatch, err := checkFrontmatterHashMismatch(workflowPath, lockFilePath)
	require.NoError(t, err)
	assert.False(t, mismatch, "Should not detect mismatch when JSON metadata hash matches")

	// Now test with a wrong hash in JSON format
	wrongHash := "0000000000000000000000000000000000000000000000000000000000000000"
	lockContentWrong := fmt.Sprintf(`# This workflow was auto-generated by gh-aw
#
# gh-aw-metadata: {"schema_version":"v1","frontmatter_hash":"%s","stop_time":""}

name: Test Workflow JSON
"on": workflow_dispatch
jobs:
  agent:
    runs-on: ubuntu-latest
    steps:
      - name: Test
        run: echo "test"
`, wrongHash)
	err = os.WriteFile(lockFilePath, []byte(lockContentWrong), 0644)
	require.NoError(t, err)

	// Test that mismatched hash in JSON format is correctly detected
	mismatch, err = checkFrontmatterHashMismatch(workflowPath, lockFilePath)
	require.NoError(t, err)
	assert.True(t, mismatch, "Should detect mismatch when JSON metadata hash differs")
}

func TestPushWorkflowFiles_CancelledContext(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()

	// Initialize a git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	err := cmd.Run()
	require.NoError(t, err)

	// Configure git
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = tmpDir
	err = cmd.Run()
	require.NoError(t, err)

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = tmpDir
	err = cmd.Run()
	require.NoError(t, err)

	// Save current directory and change to tmpDir
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	err = os.Chdir(tmpDir)
	require.NoError(t, err)
	defer os.Chdir(originalDir)

	workflowFile := filepath.Join(tmpDir, "workflow.md")
	err = os.WriteFile(workflowFile, []byte("# Test"), 0644)
	require.NoError(t, err)

	// Pass an already-cancelled context — the first git subprocess should fail with context.Canceled.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = pushWorkflowFiles(ctx, "test-workflow", []string{workflowFile}, "", false)
	require.Error(t, err)
}

func TestPushWorkflowFiles_WithStagedFiles(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()

	// Initialize a git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	err := cmd.Run()
	require.NoError(t, err)

	// Configure git
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = tmpDir
	err = cmd.Run()
	require.NoError(t, err)

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = tmpDir
	err = cmd.Run()
	require.NoError(t, err)

	// Create a test file and stage it
	testFile := filepath.Join(tmpDir, "test-file.txt")
	err = os.WriteFile(testFile, []byte("test content"), 0644)
	require.NoError(t, err)

	cmd = exec.Command("git", "add", "test-file.txt")
	cmd.Dir = tmpDir
	err = cmd.Run()
	require.NoError(t, err)

	// Save current directory and change to tmpDir
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	err = os.Chdir(tmpDir)
	require.NoError(t, err)
	defer os.Chdir(originalDir)

	// Try to push workflow files - should fail due to staged files
	workflowFile := filepath.Join(tmpDir, "workflow.md")
	err = os.WriteFile(workflowFile, []byte("# Test"), 0644)
	require.NoError(t, err)

	err = pushWorkflowFiles(context.Background(), "test-workflow", []string{workflowFile}, "", false)

	// Should return an error about staged files
	require.Error(t, err)
	require.ErrorContains(t, err, "staged files")
}

func TestCollectWorkflowFiles_AlwaysRecompiles(t *testing.T) {
	// Test that workflows are always recompiled, even when frontmatter hash matches
	tmpDir := t.TempDir()

	// Create a workflow file
	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	workflowContent := `---
name: Test Workflow
engine: copilot
on: workflow_dispatch
---
# Test Workflow
This is a test workflow.
`
	err := os.WriteFile(workflowPath, []byte(workflowContent), 0644)
	require.NoError(t, err)

	// Compute the correct hash for this workflow
	cache := parser.NewImportCache("")
	correctHash, err := parser.ComputeFrontmatterHashFromFile(workflowPath, cache)
	require.NoError(t, err)
	require.NotEmpty(t, correctHash)

	// Create a lock file with the CORRECT hash (matching)
	lockFilePath := filepath.Join(tmpDir, "test-workflow.lock.yml")
	lockContent := fmt.Sprintf(`# frontmatter-hash: %s

name: Test Workflow
"on": workflow_dispatch
jobs:
  agent:
    runs-on: ubuntu-latest
    steps:
      - name: Test
        run: echo "test"
`, correctHash)
	err = os.WriteFile(lockFilePath, []byte(lockContent), 0644)
	require.NoError(t, err)

	// Record the modification time of the lock file before collection
	lockStatBefore, err := os.Stat(lockFilePath)
	require.NoError(t, err)
	timeBefore := lockStatBefore.ModTime()

	// Sleep to ensure modification time would change if file is rewritten
	time.Sleep(100 * time.Millisecond)

	// Collect workflow files (which should always trigger recompilation)
	files, err := collectWorkflowFiles(context.Background(), workflowPath, false, false)
	require.NoError(t, err)
	assert.Len(t, files, 2, "Should collect workflow .md and .lock.yml files")

	// Verify the lock file was recompiled (modification time should be newer)
	lockStatAfter, err := os.Stat(lockFilePath)
	require.NoError(t, err)
	timeAfter := lockStatAfter.ModTime()

	// The lock file should have been recompiled even though the hash matched
	assert.True(t, timeAfter.After(timeBefore),
		"Lock file should be recompiled even when frontmatter hash matches (always recompile behavior)")
}
