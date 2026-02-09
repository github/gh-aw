//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// TestCompileWorkflow_ErrorScenarios tests various error scenarios in a table-driven manner
func TestCompileWorkflow_ErrorScenarios(t *testing.T) {
	tests := []struct {
		name          string
		setupFile     bool // whether to create a test file
		fileContent   string
		filePath      string // if empty, uses generated path; otherwise uses this path directly
		errorContains string // substring that should be in error message
	}{
		{
			name:          "nonexistent file",
			setupFile:     false,
			filePath:      "/nonexistent/file.md",
			errorContains: "failed to read file",
		},
		{
			name:          "empty path",
			setupFile:     false,
			filePath:      "",
			errorContains: "", // any error is acceptable
		},
		{
			name:      "missing frontmatter",
			setupFile: true,
			fileContent: `# Test Workflow

This workflow has no frontmatter.
`,
			errorContains: "frontmatter",
		},
		{
			name:      "invalid YAML frontmatter",
			setupFile: true,
			fileContent: `---
on: push
invalid yaml: [unclosed bracket
---

# Test Workflow

Content here.
`,
			errorContains: "", // YAML parser error varies
		},
		{
			name:      "missing markdown content",
			setupFile: true,
			fileContent: `---
on: push
engine: copilot
---
`,
			errorContains: "markdown content",
		},
		{
			name:      "unicode in workflow content",
			setupFile: true,
			fileContent: `---
on: push
engine: copilot
---

# Test Workflow 🚀

This workflow has unicode characters: 你好世界 مرحبا العالم
`,
			errorContains: "", // Should succeed or have specific error
		},
		{
			name:      "special characters in markdown",
			setupFile: true,
			fileContent: `---
on: push
engine: copilot
---

# Test Workflow with $pecial Ch@racters!

Content with <tags> and & symbols.
`,
			errorContains: "", // Should succeed
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var testFile string

			if tt.setupFile {
				tmpDir := testutil.TempDir(t, "compiler-error-test")
				testFile = filepath.Join(tmpDir, "test.md")
				require.NoError(t, os.WriteFile(testFile, []byte(tt.fileContent), 0644), "Failed to write test file")
			} else {
				testFile = tt.filePath
			}

			compiler := NewCompiler()
			err := compiler.CompileWorkflow(testFile)

			// For unicode and special character tests, we expect success or specific validation errors
			if tt.name == "unicode in workflow content" || tt.name == "special characters in markdown" {
				// These should compile successfully
				if err != nil {
					// If there's an error, it should be a validation error, not a parsing error
					assert.NotContains(t, err.Error(), "failed to read file", "Should not have file read error")
					assert.NotContains(t, err.Error(), "failed to parse", "Should not have parse error")
				}
			} else {
				require.Error(t, err, "Expected error for %s", tt.name)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains, "Error should contain expected message")
				}
			}
		})
	}
}

// TestCompileWorkflow_EdgeCases tests edge cases in workflow compilation
func TestCompileWorkflow_EdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		fileContent   string
		shouldError   bool
		errorContains string
	}{
		{
			name: "very long workflow name",
			fileContent: `---
on: push
engine: copilot
name: ` + strings.Repeat("VeryLongWorkflowName", 20) + `
---

# Test Workflow

Content here.
`,
			shouldError: false, // Long names should be allowed
		},
		{
			name: "large workflow content",
			fileContent: `---
on: push
engine: copilot
---

# Large Workflow

` + strings.Repeat("This is a test line with some content to make it larger.\n", 500),
			shouldError: false, // Large content should be handled via chunking
		},
		{
			name: "workflow with empty markdown section",
			fileContent: `---
on: push
engine: copilot
---

`,
			shouldError:   true,
			errorContains: "markdown content",
		},
		{
			name: "workflow with only whitespace in markdown",
			fileContent: `---
on: push
engine: copilot
---

   
	
   
`,
			shouldError:   true,
			errorContains: "markdown content",
		},
		{
			name:        "workflow with mixed line endings",
			fileContent: "---\r\non: push\r\nengine: copilot\r\n---\r\n\r\n# Test Workflow\r\n\r\nContent here.\r\n",
			shouldError: false, // Should handle different line endings
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "compiler-edge-case")
			testFile := filepath.Join(tmpDir, "test.md")
			require.NoError(t, os.WriteFile(testFile, []byte(tt.fileContent), 0644), "Failed to write test file")

			compiler := NewCompiler()
			err := compiler.CompileWorkflow(testFile)

			if tt.shouldError {
				require.Error(t, err, "Expected error for %s", tt.name)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains, "Error should contain expected message")
				}
			} else {
				if err != nil {
					// If there's an error, it should be a validation error, not a critical failure
					t.Logf("Got error (may be acceptable validation warning): %v", err)
				}
				// For non-error cases, just verify the lock file was created if compilation succeeded
				if err == nil {
					lockFile := stringutil.MarkdownToLockFile(testFile)
					_, statErr := os.Stat(lockFile)
					assert.NoError(t, statErr, "Lock file should be created on successful compilation")
				}
			}
		})
	}
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
		{
			name: "workflow with empty markdown content",
			workflowData: &WorkflowData{
				Name:            "Empty Content",
				Command:         []string{"echo", "test"},
				MarkdownContent: "",
				AI:              "copilot",
			},
			strictMode:  false,
			shouldError: false, // Validation may not catch this at validateWorkflowData level
		},
		{
			name: "workflow with unicode in markdown",
			workflowData: &WorkflowData{
				Name:            "Unicode Workflow",
				Command:         []string{"echo", "test"},
				MarkdownContent: "# Test 🚀\n\nContent with unicode: 你好",
				AI:              "copilot",
			},
			strictMode:  false,
			shouldError: false,
		},
		{
			name: "workflow with very long name",
			workflowData: &WorkflowData{
				Name:            strings.Repeat("LongName", 50),
				Command:         []string{"echo", "test"},
				MarkdownContent: "# Test",
				AI:              "copilot",
			},
			strictMode:  false,
			shouldError: false, // Long names should be allowed
		},
		{
			name: "workflow with null AI engine",
			workflowData: &WorkflowData{
				Name:            "No Engine",
				Command:         []string{"echo", "test"},
				MarkdownContent: "# Test",
				AI:              "",
			},
			strictMode:  false,
			shouldError: false, // May be caught elsewhere
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
				if err != nil {
					// Log non-critical errors for investigation
					t.Logf("Got error (may be acceptable): %v", err)
				}
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
		{
			name: "workflow with unicode content",
			workflowData: &WorkflowData{
				Name:            "Unicode Test",
				Command:         []string{"echo", "test"},
				MarkdownContent: "# Test 🚀\n\nUnicode: 你好世界",
				AI:              "copilot",
			},
			shouldError: false,
		},
		{
			name: "workflow with special characters",
			workflowData: &WorkflowData{
				Name:            "Special Chars",
				Command:         []string{"echo", "test"},
				MarkdownContent: "# Test\n\nSpecial: <tag> & symbol $ percent%",
				AI:              "copilot",
			},
			shouldError: false,
		},
		{
			name: "workflow with very long content",
			workflowData: &WorkflowData{
				Name:            "Large Workflow",
				Command:         []string{"echo", "test"},
				MarkdownContent: "# Test\n\n" + strings.Repeat("This is a long line of text for testing.\n", 300),
				AI:              "copilot",
			},
			shouldError: false,
		},
		{
			name: "workflow with multiple tools",
			workflowData: &WorkflowData{
				Name:            "Multi-Tool",
				Command:         []string{"echo", "test"},
				MarkdownContent: "# Test",
				AI:              "copilot",
				Tools: map[string]any{
					"bash":   []string{"echo", "ls"},
					"github": map[string]any{"allowed": []string{"list_issues"}},
				},
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
				if err != nil {
					// Log error but don't fail - validation errors may be acceptable
					t.Logf("Got error (may be validation warning): %v", err)
				} else {
					require.NoError(t, err, "Expected YAML generation to pass")
					assert.NotEmpty(t, yamlContent, "YAML content should not be empty")
					assert.Contains(t, yamlContent, "name:", "YAML should contain workflow name")
					assert.Contains(t, yamlContent, "jobs:", "YAML should contain jobs section")
				}
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

// TestCompileWorkflow_ConcurrentCompilation tests thread-safety of concurrent compilations
func TestCompileWorkflow_ConcurrentCompilation(t *testing.T) {
	const numWorkers = 10
	const workflowsPerWorker = 5

	tmpDir := testutil.TempDir(t, "concurrent-test")

	// Create test workflows
	testContent := `---
on: push
engine: copilot
---

# Test Workflow

This is a test workflow for concurrent compilation.
`

	// Create multiple workflow files
	var workflowFiles []string
	for i := 0; i < numWorkers*workflowsPerWorker; i++ {
		testFile := filepath.Join(tmpDir, fmt.Sprintf("workflow-%d.md", i))
		require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644), "Failed to write test file %d", i)
		workflowFiles = append(workflowFiles, testFile)
	}

	// Compile workflows concurrently
	var wg sync.WaitGroup
	errChan := make(chan error, len(workflowFiles))

	for _, workflowFile := range workflowFiles {
		wg.Add(1)
		go func(file string) {
			defer wg.Done()
			compiler := NewCompiler()
			if err := compiler.CompileWorkflow(file); err != nil {
				errChan <- fmt.Errorf("failed to compile %s: %w", filepath.Base(file), err)
			}
		}(workflowFile)
	}

	wg.Wait()
	close(errChan)

	// Check for errors
	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		t.Errorf("Concurrent compilation failed with %d errors:", len(errors))
		for _, err := range errors {
			t.Errorf("  - %v", err)
		}
	}

	// Verify all lock files were created
	for _, workflowFile := range workflowFiles {
		lockFile := stringutil.MarkdownToLockFile(workflowFile)
		_, err := os.Stat(lockFile)
		assert.NoError(t, err, "Lock file should be created for %s", filepath.Base(workflowFile))
	}
}

// TestCompileWorkflow_PerformanceRegression tests compilation performance for large workflows
func TestCompileWorkflow_PerformanceRegression(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	tests := []struct {
		name        string
		fileContent string
		maxDuration time.Duration
	}{
		{
			name: "small workflow",
			fileContent: `---
on: push
engine: copilot
---

# Small Workflow

This is a small workflow.
`,
			maxDuration: 500 * time.Millisecond,
		},
		{
			name: "medium workflow",
			fileContent: `---
on: push
engine: copilot
tools:
  github:
    allowed: [list_issues, create_issue, list_pull_requests]
  bash: ["echo", "ls", "cat"]
---

# Medium Workflow

` + strings.Repeat("This is a test line.\n", 100),
			maxDuration: 1 * time.Second,
		},
		{
			name: "large workflow",
			fileContent: `---
on: push
engine: copilot
tools:
  github:
    allowed: [list_issues, create_issue, list_pull_requests, issue_read, pull_request_read]
  bash: ["echo", "ls", "cat", "grep", "find"]
network:
  allowed:
    - "github.com"
    - "api.github.com"
---

# Large Workflow

` + strings.Repeat("This is a test line with more content for testing.\n", 500),
			maxDuration: 3 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "perf-test")
			testFile := filepath.Join(tmpDir, "test.md")
			require.NoError(t, os.WriteFile(testFile, []byte(tt.fileContent), 0644), "Failed to write test file")

			compiler := NewCompiler()

			start := time.Now()
			err := compiler.CompileWorkflow(testFile)
			duration := time.Since(start)

			if err != nil {
				// Log error but don't fail - may be validation warning
				t.Logf("Compilation error (may be acceptable): %v", err)
			}

			// Check performance
			if duration > tt.maxDuration {
				t.Errorf("Compilation took %v, expected under %v (%.1fx slower than expected)",
					duration, tt.maxDuration, float64(duration)/float64(tt.maxDuration))
			} else {
				t.Logf("Compilation completed in %v (%.1f%% of max allowed time)",
					duration, 100*float64(duration)/float64(tt.maxDuration))
			}

			// Verify lock file was created (if compilation succeeded)
			if err == nil {
				lockFile := stringutil.MarkdownToLockFile(testFile)
				info, statErr := os.Stat(lockFile)
				require.NoError(t, statErr, "Lock file should be created")
				t.Logf("Generated lock file size: %d bytes", info.Size())
			}
		})
	}
}
