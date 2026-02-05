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

// TestCompilerGeneratesUnixPaths tests that the compiler always generates
// Unix-compatible file paths (forward slashes) in .lock.yml files,
// even when running on Windows (with backslash separators)
func TestCompilerGeneratesUnixPaths(t *testing.T) {
	tests := []struct {
		name                 string
		markdownContent      string
		expectedImportPaths  []string
		expectedIncludePaths []string
		expectedSourcePath   string
	}{
		{
			name: "imports with forward slashes",
			markdownContent: `---
on: issues
imports:
  - shared/common.md
  - shared/reporting.md
source: workflows/test-workflow.md
---

# Test Workflow

This is a test workflow with imports.`,
			expectedImportPaths: []string{
				"shared/common.md",
				"shared/reporting.md",
			},
			expectedSourcePath: "workflows/test-workflow.md",
		},
		{
			name: "includes with forward slashes",
			markdownContent: `---
on: pull_request
---

# Test Include Workflow

{{#import shared/tools.md}}

This workflow includes external tools.`,
			expectedIncludePaths: []string{
				"shared/tools.md",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test
			tmpDir := t.TempDir()

			// Create shared directory and files for imports/includes
			sharedDir := filepath.Join(tmpDir, "shared")
			err := os.MkdirAll(sharedDir, 0755)
			require.NoError(t, err, "Failed to create shared directory")

			// Create shared/common.md (shared workflow - minimal valid content)
			commonContent := `# Common Shared Workflow

This is a shared workflow.`
			commonFile := filepath.Join(sharedDir, "common.md")
			err = os.WriteFile(commonFile, []byte(commonContent), 0644)
			require.NoError(t, err, "Failed to create common.md")

			// Create shared/reporting.md (shared workflow - minimal valid content)
			reportingContent := `# Reporting Shared Workflow

This is a shared workflow.`
			reportingFile := filepath.Join(sharedDir, "reporting.md")
			err = os.WriteFile(reportingFile, []byte(reportingContent), 0644)
			require.NoError(t, err, "Failed to create reporting.md")

			// Create shared/tools.md (shared workflow - minimal valid content)
			toolsContent := `# Tools Shared Workflow

This is a shared workflow.`
			toolsFile := filepath.Join(sharedDir, "tools.md")
			err = os.WriteFile(toolsFile, []byte(toolsContent), 0644)
			require.NoError(t, err, "Failed to create tools.md")

			// Create workflows directory for source path
			workflowsDir := filepath.Join(tmpDir, "workflows")
			err = os.MkdirAll(workflowsDir, 0755)
			require.NoError(t, err, "Failed to create workflows directory")

			// Write markdown file
			markdownPath := filepath.Join(tmpDir, "test-workflow.md")
			err = os.WriteFile(markdownPath, []byte(tt.markdownContent), 0644)
			require.NoError(t, err, "Failed to write markdown file")

			// Compile the workflow
			compiler := NewCompiler()
			err = compiler.CompileWorkflow(markdownPath)
			require.NoError(t, err, "Compilation should succeed")

			// Read the generated .lock.yml file
			lockFile := filepath.Join(tmpDir, "test-workflow.lock.yml")
			lockContent, err := os.ReadFile(lockFile)
			require.NoError(t, err, "Failed to read lock file")

			lockYAML := string(lockContent)

			// Verify that file paths in the manifest use forward slashes (Unix-compatible)
			// Note: The ASCII art header contains backslashes, so we only check the manifest section
			manifestStart := strings.Index(lockYAML, "# Resolved workflow manifest:")
			sourceStart := strings.Index(lockYAML, "# Source:")

			// Verify expected import paths are present with forward slashes
			for _, importPath := range tt.expectedImportPaths {
				expectedLine := "#     - " + importPath
				assert.Contains(t, lockYAML, expectedLine, "Lock file should contain import path: %s", importPath)

				// Ensure no backslash version exists
				backslashPath := strings.ReplaceAll(importPath, "/", "\\")
				backslashLine := "#     - " + backslashPath
				assert.NotContains(t, lockYAML, backslashLine, "Lock file should not contain backslash version of: %s", importPath)
			}

			// Verify expected include paths are present with forward slashes
			for _, includePath := range tt.expectedIncludePaths {
				expectedLine := "#     - " + includePath
				assert.Contains(t, lockYAML, expectedLine, "Lock file should contain include path: %s", includePath)

				// Ensure no backslash version exists
				backslashPath := strings.ReplaceAll(includePath, "/", "\\")
				backslashLine := "#     - " + backslashPath
				assert.NotContains(t, lockYAML, backslashLine, "Lock file should not contain backslash version of: %s", includePath)
			}

			// Verify source path uses forward slashes
			if tt.expectedSourcePath != "" {
				expectedLine := "# Source: " + tt.expectedSourcePath
				assert.Contains(t, lockYAML, expectedLine, "Lock file should contain source path: %s", tt.expectedSourcePath)

				// Ensure no backslash version exists
				backslashPath := strings.ReplaceAll(tt.expectedSourcePath, "/", "\\")
				backslashLine := "# Source: " + backslashPath
				assert.NotContains(t, lockYAML, backslashLine, "Lock file should not contain backslash version of source: %s", tt.expectedSourcePath)
			}

			// Verify that manifest section does not contain backslashes in file paths
			if manifestStart >= 0 {
				manifestEnd := strings.Index(lockYAML[manifestStart:], "\n\n")
				if manifestEnd >= 0 {
					manifest := lockYAML[manifestStart : manifestStart+manifestEnd]
					assert.NotContains(t, manifest, "\\", "Lock file manifest should not contain backslashes in file paths")
				}
			}

			// Verify that source section does not contain backslashes in file paths
			if sourceStart >= 0 && tt.expectedSourcePath != "" {
				sourceEnd := strings.Index(lockYAML[sourceStart:], "\n")
				if sourceEnd >= 0 {
					sourceLine := lockYAML[sourceStart : sourceStart+sourceEnd]
					// Check that the source line doesn't contain a Windows path
					backslashPath := strings.ReplaceAll(tt.expectedSourcePath, "/", "\\")
					assert.NotContains(t, sourceLine, backslashPath, "Source line should not contain Windows-style path")
				}
			}
		})
	}
}

// TestPathNormalizationInIncludedFiles tests that included files from ExpandIncludesWithManifest
// are normalized to use forward slashes in the lock file
func TestPathNormalizationInIncludedFiles(t *testing.T) {
	// Create temporary directory structure
	tmpDir := t.TempDir()

	// Create nested directory structure: shared/nested/deep
	deepDir := filepath.Join(tmpDir, "shared", "nested", "deep")
	err := os.MkdirAll(deepDir, 0755)
	require.NoError(t, err, "Failed to create deep directory")

	// Create shared/nested/deep/config.md (shared workflow - minimal valid content)
	configContent := `# Deep Config

This is a deeply nested shared workflow.`
	configFile := filepath.Join(deepDir, "config.md")
	err = os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err, "Failed to create config.md")

	// Create workflow that includes the deep file
	markdownContent := `---
on: push
---

# Deep Include Test

{{#import shared/nested/deep/config.md}}

This workflow includes a deeply nested file.`

	markdownPath := filepath.Join(tmpDir, "test-workflow.md")
	err = os.WriteFile(markdownPath, []byte(markdownContent), 0644)
	require.NoError(t, err, "Failed to write markdown file")

	// Compile the workflow
	compiler := NewCompiler()
	err = compiler.CompileWorkflow(markdownPath)
	require.NoError(t, err, "Compilation should succeed")

	// Read the generated .lock.yml file
	lockFile := filepath.Join(tmpDir, "test-workflow.lock.yml")
	lockContent, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Failed to read lock file")

	lockYAML := string(lockContent)

	// Verify the include path uses forward slashes
	expectedInclude := "#     - shared/nested/deep/config.md"
	assert.Contains(t, lockYAML, expectedInclude, "Lock file should contain nested include with forward slashes")

	// Verify no backslashes exist in file paths (ignore ASCII art in header)
	// Extract the manifest section
	manifestStart := strings.Index(lockYAML, "# Resolved workflow manifest:")
	if manifestStart >= 0 {
		manifestEnd := strings.Index(lockYAML[manifestStart:], "\n\n")
		if manifestEnd >= 0 {
			manifest := lockYAML[manifestStart : manifestStart+manifestEnd]
			assert.NotContains(t, manifest, "\\", "Lock file manifest should not contain any backslashes")
		}
	}

	// Specifically check for Windows-style path with backslashes (should NOT exist)
	windowsPath := "shared\\nested\\deep\\config.md"
	assert.NotContains(t, lockYAML, windowsPath, "Lock file should not contain Windows-style path")
}
