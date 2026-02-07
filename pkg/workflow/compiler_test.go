//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCompileWorkflow_ValidWorkflow tests successful compilation of a valid workflow
func TestCompileWorkflow_ValidWorkflow(t *testing.T) {
	tmpDir := testutil.TempDir(t, "compiler-test")

	testContent := `---
on: push
timeout-minutes: 10
permissions:
  contents: read
  pull-requests: read
engine: copilot
strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [list_issues, create_issue]
  bash: ["echo", "ls"]
---

# Test Workflow

This is a test workflow for compilation.
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "Valid workflow should compile without errors")

	// Verify lock file was created
	lockFile := stringutil.MarkdownToLockFile(testFile)
	_, err = os.Stat(lockFile)
	require.NoError(t, err, "Lock file should be created")

	// Verify lock file contains expected content
	lockContent, err := os.ReadFile(lockFile)
	require.NoError(t, err)
	lockStr := string(lockContent)

	// Verify basic workflow structure
	assert.Contains(t, lockStr, "name:", "Lock file should contain workflow name")
	assert.Contains(t, lockStr, "on:", "Lock file should contain 'on' trigger")
	assert.Contains(t, lockStr, "jobs:", "Lock file should contain jobs section")
}

// TestCompileWorkflow_NonexistentFile tests error handling for missing files
func TestCompileWorkflow_NonexistentFile(t *testing.T) {
	compiler := NewCompiler()
	err := compiler.CompileWorkflow("/nonexistent/file.md")
	require.Error(t, err, "Should error with nonexistent file")
	assert.Contains(t, err.Error(), "failed to read file", "Error should mention file read failure")
}

// TestCompileWorkflow_EmptyPath tests error handling for empty path
func TestCompileWorkflow_EmptyPath(t *testing.T) {
	compiler := NewCompiler()
	err := compiler.CompileWorkflow("")
	require.Error(t, err, "Should error with empty path")
}

// TestCompileWorkflow_MissingFrontmatter tests error handling for files without frontmatter
func TestCompileWorkflow_MissingFrontmatter(t *testing.T) {
	tmpDir := testutil.TempDir(t, "compiler-missing-frontmatter")

	// File with no frontmatter
	testContent := `# Test Workflow

This workflow has no frontmatter.
`

	testFile := filepath.Join(tmpDir, "no-frontmatter.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(testFile)
	require.Error(t, err, "Should error when frontmatter is missing")
	assert.Contains(t, err.Error(), "frontmatter", "Error should mention frontmatter")
}

// TestCompileWorkflow_InvalidFrontmatter tests error handling for invalid YAML frontmatter
func TestCompileWorkflow_InvalidFrontmatter(t *testing.T) {
	tmpDir := testutil.TempDir(t, "compiler-invalid-frontmatter")

	// Invalid YAML in frontmatter
	testContent := `---
on: push
invalid yaml: [unclosed bracket
---

# Test Workflow

Content here.
`

	testFile := filepath.Join(tmpDir, "invalid-frontmatter.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(testFile)
	require.Error(t, err, "Should error with invalid YAML frontmatter")
}

// TestCompileWorkflow_MissingMarkdownContent tests error handling for workflows with no markdown content
func TestCompileWorkflow_MissingMarkdownContent(t *testing.T) {
	tmpDir := testutil.TempDir(t, "compiler-no-markdown")

	// Frontmatter only, no markdown
	testContent := `---
on: push
engine: copilot
---
`

	testFile := filepath.Join(tmpDir, "no-markdown.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(testFile)
	require.Error(t, err, "Should error when markdown content is missing")
	assert.Contains(t, err.Error(), "markdown content", "Error should mention markdown content")
}

// TestCompileWorkflowData_Success tests CompileWorkflowData with valid workflow data
func TestCompileWorkflowData_Success(t *testing.T) {
	tmpDir := testutil.TempDir(t, "compiler-data-test")

	workflowData := &WorkflowData{
		Name:            "Test Workflow",
		Command:         []string{"echo", "test"},
		MarkdownContent: "# Test\n\nTest content",
		AI:              "copilot",
	}

	markdownPath := filepath.Join(tmpDir, "test.md")
	// Create the markdown file (needed for lock file generation)
	testContent := `---
on: push
engine: copilot
---

# Test

Test content
`
	require.NoError(t, os.WriteFile(markdownPath, []byte(testContent), 0644))

	compiler := NewCompiler()
	err := compiler.CompileWorkflowData(workflowData, markdownPath)
	require.NoError(t, err, "CompileWorkflowData should succeed with valid data")

	// Verify lock file was created
	lockFile := stringutil.MarkdownToLockFile(markdownPath)
	_, err = os.Stat(lockFile)
	require.NoError(t, err, "Lock file should be created")
}

// TestCompileWorkflow_LockFileSize tests that generated lock files don't exceed size limits
func TestCompileWorkflow_LockFileSize(t *testing.T) {
	tmpDir := testutil.TempDir(t, "compiler-size-test")

	testContent := `---
on: push
engine: copilot
strict: false
features:
  dangerous-permissions-write: true
---

# Size Test Workflow

This is a normal workflow that should generate a reasonable-sized lock file.
`

	testFile := filepath.Join(tmpDir, "size-test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "Workflow should compile")

	// Check lock file size
	lockFile := stringutil.MarkdownToLockFile(testFile)
	info, err := os.Stat(lockFile)
	require.NoError(t, err)

	// Verify size is reasonable (under MaxLockFileSize)
	assert.LessOrEqual(t, info.Size(), int64(MaxLockFileSize),
		"Lock file should not exceed MaxLockFileSize (%d bytes)", MaxLockFileSize)
}

// TestCompileWorkflow_ErrorFormatting tests that compilation errors are properly formatted
func TestCompileWorkflow_ErrorFormatting(t *testing.T) {
	tmpDir := testutil.TempDir(t, "compiler-error-format")

	// Create a workflow with a validation error (missing required 'on' field in main workflow)
	testContent := `---
engine: copilot
---

# Invalid Workflow

This workflow is missing the required 'on' field.
`

	testFile := filepath.Join(tmpDir, "invalid.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(testFile)
	require.Error(t, err, "Should error with validation issues")

	// Error should contain file reference
	errorStr := err.Error()
	assert.True(t, strings.Contains(errorStr, "invalid.md") || strings.Contains(errorStr, "error"),
		"Error should reference the file or contain 'error'")
}

// TestCompileWorkflow_PathTraversal tests that path traversal attempts are handled safely
func TestCompileWorkflow_PathTraversal(t *testing.T) {
	compiler := NewCompiler()

	// Try a path with traversal elements
	err := compiler.CompileWorkflow("../../etc/passwd")
	require.Error(t, err, "Should error (file doesn't exist or is rejected)")
}

// TestCompileWorkflowData_ArtifactManagerReset tests that artifact manager is reset between compilations
func TestCompileWorkflowData_ArtifactManagerReset(t *testing.T) {
	tmpDir := testutil.TempDir(t, "compiler-artifact-reset")

	workflowData := &WorkflowData{
		Name:            "Test Workflow 1",
		Command:         []string{"echo", "test"},
		MarkdownContent: "# Test 1",
		AI:              "copilot",
	}

	markdownPath := filepath.Join(tmpDir, "test1.md")
	testContent := `---
on: push
engine: copilot
---

# Test 1
`
	require.NoError(t, os.WriteFile(markdownPath, []byte(testContent), 0644))

	compiler := NewCompiler()

	// First compilation
	err := compiler.CompileWorkflowData(workflowData, markdownPath)
	require.NoError(t, err)

	// Artifact manager should exist
	require.NotNil(t, compiler.artifactManager, "Artifact manager should be initialized")

	// Second compilation with different data
	workflowData2 := &WorkflowData{
		Name:            "Test Workflow 2",
		Command:         []string{"echo", "test2"},
		MarkdownContent: "# Test 2",
		AI:              "copilot",
	}

	markdownPath2 := filepath.Join(tmpDir, "test2.md")
	testContent2 := `---
on: push
engine: copilot
---

# Test 2
`
	require.NoError(t, os.WriteFile(markdownPath2, []byte(testContent2), 0644))

	err = compiler.CompileWorkflowData(workflowData2, markdownPath2)
	require.NoError(t, err)

	// Artifact manager should still exist (it's reset, not recreated to nil)
	require.NotNil(t, compiler.artifactManager, "Artifact manager should persist after reset")
}

// TestValidateWorkflowData tests the validateWorkflowData function
func TestValidateWorkflowData(t *testing.T) {
	tests := []struct {
		name          string
		workflowData  *WorkflowData
		strictMode    bool
		shouldError   bool
		errorContains string
	}{
		{
			name: "valid workflow",
			workflowData: &WorkflowData{
				Name:            "Valid Workflow",
				Command:         []string{"echo", "test"},
				MarkdownContent: "# Test",
				AI:              "copilot",
			},
			strictMode:  false,
			shouldError: false,
		},
		{
			name: "invalid action-mode feature flag",
			workflowData: &WorkflowData{
				Name:            "Invalid Action Mode",
				Command:         []string{"echo", "test"},
				MarkdownContent: "# Test",
				AI:              "copilot",
				Features: map[string]any{
					"action-mode": "invalid-mode",
				},
			},
			strictMode:    false,
			shouldError:   true,
			errorContains: "invalid action-mode feature flag",
		},
		{
			name: "missing permissions for agentic-workflows tool",
			workflowData: &WorkflowData{
				Name:            "Missing Permissions",
				Command:         []string{"echo", "test"},
				MarkdownContent: "# Test",
				AI:              "copilot",
				Tools: map[string]any{
					"agentic-workflows": map[string]any{},
				},
				Permissions: "", // No permissions
			},
			strictMode:    false,
			shouldError:   true,
			errorContains: "Missing required permission for agentic-workflows tool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "validate-test")
			markdownPath := filepath.Join(tmpDir, "test.md")

			compiler := NewCompiler()
			compiler.strictMode = tt.strictMode
			err := compiler.validateWorkflowData(tt.workflowData, markdownPath)

			if tt.shouldError {
				require.Error(t, err, "Expected validation to fail")
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains, "Error should contain expected message")
				}
			} else {
				require.NoError(t, err, "Expected validation to pass")
			}
		})
	}
}

// TestGenerateAndValidateYAML tests the generateAndValidateYAML function
func TestGenerateAndValidateYAML(t *testing.T) {
	tests := []struct {
		name          string
		workflowData  *WorkflowData
		shouldError   bool
		errorContains string
	}{
		{
			name: "valid workflow generates YAML",
			workflowData: &WorkflowData{
				Name:            "Test Workflow",
				Command:         []string{"echo", "test"},
				MarkdownContent: "# Test",
				AI:              "copilot",
			},
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "yaml-test")
			markdownPath := filepath.Join(tmpDir, "test.md")
			lockFile := stringutil.MarkdownToLockFile(markdownPath)

			compiler := NewCompiler()
			// Initialize required state
			compiler.markdownPath = markdownPath
			compiler.stepOrderTracker = NewStepOrderTracker()
			compiler.artifactManager = NewArtifactManager()

			yamlContent, err := compiler.generateAndValidateYAML(tt.workflowData, markdownPath, lockFile)

			if tt.shouldError {
				require.Error(t, err, "Expected YAML generation to fail")
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains, "Error should contain expected message")
				}
			} else {
				require.NoError(t, err, "Expected YAML generation to pass")
				assert.NotEmpty(t, yamlContent, "YAML content should not be empty")
				assert.Contains(t, yamlContent, "name:", "YAML should contain workflow name")
				assert.Contains(t, yamlContent, "jobs:", "YAML should contain jobs section")
			}
		})
	}
}

// TestWriteWorkflowOutput tests the writeWorkflowOutput function
func TestWriteWorkflowOutput(t *testing.T) {
	tests := []struct {
		name              string
		yamlContent       string
		noEmit            bool
		quiet             bool
		shouldError       bool
		expectFileWritten bool
	}{
		{
			name:              "write valid YAML",
			yamlContent:       "name: test\non: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo test\n",
			noEmit:            false,
			quiet:             false,
			shouldError:       false,
			expectFileWritten: true,
		},
		{
			name:              "no emit mode",
			yamlContent:       "name: test\non: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo test\n",
			noEmit:            true,
			quiet:             false,
			shouldError:       false,
			expectFileWritten: false,
		},
		{
			name:              "quiet mode",
			yamlContent:       "name: test\non: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo test\n",
			noEmit:            false,
			quiet:             true,
			shouldError:       false,
			expectFileWritten: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "output-test")
			markdownPath := filepath.Join(tmpDir, "test.md")
			lockFile := stringutil.MarkdownToLockFile(markdownPath)

			compiler := NewCompiler()
			compiler.noEmit = tt.noEmit
			compiler.quiet = tt.quiet

			err := compiler.writeWorkflowOutput(lockFile, tt.yamlContent, markdownPath)

			if tt.shouldError {
				require.Error(t, err, "Expected write to fail")
			} else {
				require.NoError(t, err, "Expected write to pass")

				if tt.expectFileWritten {
					// Verify file was created
					_, err := os.Stat(lockFile)
					require.NoError(t, err, "Lock file should be created")

					// Verify content
					content, err := os.ReadFile(lockFile)
					require.NoError(t, err, "Should be able to read lock file")
					assert.Equal(t, tt.yamlContent, string(content), "File content should match")
				} else {
					// Verify file was NOT created in noEmit mode
					_, err := os.Stat(lockFile)
					assert.True(t, os.IsNotExist(err), "Lock file should not exist in noEmit mode")
				}
			}
		})
	}
}

// TestWriteWorkflowOutput_ContentUnchanged tests that the file is not rewritten if content hasn't changed
func TestWriteWorkflowOutput_ContentUnchanged(t *testing.T) {
	tmpDir := testutil.TempDir(t, "unchanged-test")
	markdownPath := filepath.Join(tmpDir, "test.md")
	lockFile := stringutil.MarkdownToLockFile(markdownPath)

	yamlContent := "name: test\non: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n"

	// Write initial content
	require.NoError(t, os.WriteFile(lockFile, []byte(yamlContent), 0644))

	// Get initial modification time
	initialInfo, err := os.Stat(lockFile)
	require.NoError(t, err)
	initialModTime := initialInfo.ModTime()

	// Sleep to ensure filesystem mtime resolution is exceeded
	// Most filesystems have 1-2 second resolution for mtime
	time.Sleep(2 * time.Second)

	// Write same content again
	compiler := NewCompiler()
	err = compiler.writeWorkflowOutput(lockFile, yamlContent, markdownPath)
	require.NoError(t, err)

	// Check that modification time hasn't changed (file wasn't rewritten)
	finalInfo, err := os.Stat(lockFile)
	require.NoError(t, err)
	finalModTime := finalInfo.ModTime()

	assert.Equal(t, initialModTime, finalModTime, "File should not be rewritten if content is unchanged")
}
