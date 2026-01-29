//go:build !integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

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
	files, err := collectWorkflowFiles(workflowPath, false)
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
	files, err := collectWorkflowFiles(workflowPath, false)
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
	files, err := collectWorkflowFiles(workflowPath, false)
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

func TestIsWorkflowSpecFormatLocal(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "workflowspec with SHA",
			path:     "owner/repo/path/file.md@abc123",
			expected: true,
		},
		{
			name:     "workflowspec without SHA",
			path:     "owner/repo/path/file.md",
			expected: true,
		},
		{
			name:     "relative path with ./",
			path:     "./shared/file.md",
			expected: false,
		},
		{
			name:     "relative path without ./",
			path:     "shared/file.md",
			expected: false,
		},
		{
			name:     "absolute path",
			path:     "/shared/file.md",
			expected: false,
		},
		{
			name:     "workflowspec with section",
			path:     "owner/repo/path/file.md#section",
			expected: true,
		},
		{
			name:     "simple filename",
			path:     "file.md",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isWorkflowSpecFormatLocal(tt.path)
			assert.Equal(t, tt.expected, result, "isWorkflowSpecFormatLocal(%q) = %v, want %v", tt.path, result, tt.expected)
		})
	}
}

func TestResolveImportPathLocal(t *testing.T) {
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
			name:       "workflowspec format without @",
			importPath: "owner/repo/path/file.md",
			baseDir:    baseDir,
			expected:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveImportPathLocal(tt.importPath, tt.baseDir)
			assert.Equal(t, tt.expected, result, "resolveImportPathLocal(%q, %q) = %v, want %v", tt.importPath, tt.baseDir, result, tt.expected)
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

	err = pushWorkflowFiles("test-workflow", []string{workflowFile}, "", false)

	// Should return an error about staged files
	require.Error(t, err)
	assert.Contains(t, err.Error(), "staged files")
}
