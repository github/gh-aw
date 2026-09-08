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

// TestValidateRuntimeImportFiles tests the validateRuntimeImportFiles function
func TestValidateRuntimeImportFiles(t *testing.T) {
	// Create a temporary directory structure for testing
	tmpDir := t.TempDir()
	githubDir := filepath.Join(tmpDir, ".github")
	sharedDir := filepath.Join(githubDir, "shared")
	require.NoError(t, os.MkdirAll(sharedDir, 0755))

	// Create test files with different content
	validFile := filepath.Join(sharedDir, "valid.md")
	validContent := `# Valid Content

This file has safe expressions:
- Actor: ${{ github.actor }}
- Repository: ${{ github.repository }}
- Issue number: ${{ github.event.issue.number }}
`
	require.NoError(t, os.WriteFile(validFile, []byte(validContent), 0644))

	invalidFile := filepath.Join(sharedDir, "invalid.md")
	invalidContent := `# Invalid Content

This file has unsafe expressions:
- Secret: ${{ secrets.MY_TOKEN }}
- Runner: ${{ runner.os }}
`
	require.NoError(t, os.WriteFile(invalidFile, []byte(invalidContent), 0644))

	multilineFile := filepath.Join(sharedDir, "multiline.md")
	multilineContent := `# Multiline Expression

This has a multiline expression:
${{ github.actor
    && github.run_id }}
`
	require.NoError(t, os.WriteFile(multilineFile, []byte(multilineContent), 0644))

	nestedValidFile := filepath.Join(sharedDir, "nested-valid.md")
	require.NoError(t, os.WriteFile(nestedValidFile, []byte("Nested actor: ${{ github.actor }}"), 0644))
	nestedInvalidFile := filepath.Join(sharedDir, "nested-invalid.md")
	require.NoError(t, os.WriteFile(nestedInvalidFile, []byte("Nested secret: ${{ secrets.NESTED_TOKEN }}"), 0644))
	nestedRootFile := filepath.Join(sharedDir, "nested-root.md")
	require.NoError(t, os.WriteFile(nestedRootFile, []byte("Root\n{{#runtime-import shared/nested-valid.md}}\n{{#import: shared/nested-invalid.md}}"), 0644))
	frontmatterSecretFile := filepath.Join(sharedDir, "frontmatter-secret.md")
	require.NoError(t, os.WriteFile(frontmatterSecretFile, []byte(`---
env:
  TOKEN: ${{ secrets.CONFIG_ONLY_TOKEN }}
---
Body actor: ${{ github.actor }}`), 0644))

	tests := []struct {
		name        string
		markdown    string
		expectError bool
		errorText   string
	}{
		{
			name:        "no runtime imports",
			markdown:    "# Simple workflow\n\nNo imports here",
			expectError: false,
		},
		{
			name:        "valid runtime import",
			markdown:    "{{#runtime-import ./shared/valid.md}}",
			expectError: false,
		},
		{
			name:        "invalid runtime import",
			markdown:    "{{#runtime-import ./shared/invalid.md}}",
			expectError: true,
			errorText:   "secrets.MY_TOKEN",
		},
		{
			name:        "multiline expression in import",
			markdown:    "{{#runtime-import ./shared/multiline.md}}",
			expectError: true,
			errorText:   "unauthorized expressions",
		},
		{
			name:        "multiple imports with one invalid",
			markdown:    "{{#runtime-import ./shared/valid.md}}\n{{#runtime-import ./shared/invalid.md}}",
			expectError: true,
			errorText:   "secrets.MY_TOKEN",
		},
		{
			name:        "non-existent file (should skip)",
			markdown:    "{{#runtime-import ./shared/nonexistent.md}}",
			expectError: false, // Should skip validation for non-existent files
		},
		{
			name:        "URL import (should skip)",
			markdown:    "{{#runtime-import https://example.com/remote.md}}",
			expectError: false,
		},
		{
			name:        "nested invalid runtime import",
			markdown:    "{{#runtime-import shared/nested-root.md}}",
			expectError: true,
			errorText:   "secrets.NESTED_TOKEN",
		},
		{
			name:        "traversal path rejected",
			markdown:    "{{#runtime-import ../outside.md}}",
			expectError: true,
			errorText:   "Path must be within .github folder",
		},
		{
			name:        "frontmatter secrets ignored",
			markdown:    "{{#runtime-import shared/frontmatter-secret.md}}",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateRuntimeImportFiles(tt.markdown, tmpDir)

			if tt.expectError {
				require.Error(t, err, "Expected an error")
				if tt.errorText != "" {
					require.ErrorContains(t, err, tt.errorText, "Error should contain expected text")
				}
			} else {
				assert.NoError(t, err, "Expected no error")
			}
		})
	}
}

// TestValidateRuntimeImportFiles_PathNormalization tests path normalization
func TestValidateRuntimeImportFiles_PathNormalization(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()
	githubDir := filepath.Join(tmpDir, ".github")
	sharedDir := filepath.Join(githubDir, "shared")
	require.NoError(t, os.MkdirAll(sharedDir, 0755))

	// Create a valid test file
	validFile := filepath.Join(sharedDir, "test.md")
	validContent := "# Test\n\nActor: ${{ github.actor }}"
	require.NoError(t, os.WriteFile(validFile, []byte(validContent), 0644))

	tests := []struct {
		name        string
		markdown    string
		expectError bool
	}{
		{
			name:        "path with ./",
			markdown:    "{{#runtime-import ./shared/test.md}}",
			expectError: false,
		},
		{
			name:        "path with .github/",
			markdown:    "{{#runtime-import .github/shared/test.md}}",
			expectError: false,
		},
		{
			name:        "path without prefix",
			markdown:    "{{#runtime-import shared/test.md}}",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateRuntimeImportFiles(tt.markdown, tmpDir)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateRuntimeImportFilesRejectsSymlinkEscape(t *testing.T) {
	tmpDir := t.TempDir()
	githubDir := filepath.Join(tmpDir, ".github")
	sharedDir := filepath.Join(githubDir, "shared")
	require.NoError(t, os.MkdirAll(sharedDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "outside.md"), []byte("Outside secret: ${{ secrets.OUTSIDE_TOKEN }}"), 0644))
	require.NoError(t, os.Symlink(filepath.Join(tmpDir, "outside.md"), filepath.Join(sharedDir, "outside-link.md")))

	_, err := validateRuntimeImportFiles("{{#runtime-import shared/outside-link.md}}", tmpDir)

	require.Error(t, err)
	require.ErrorContains(t, err, "must remain within .github folder after resolving symlinks")
}

func TestValidateExpressionsValidatesGeneratedRuntimeImportMacros(t *testing.T) {
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	sharedDir := filepath.Join(tmpDir, ".github", "shared")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))
	require.NoError(t, os.MkdirAll(sharedDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(sharedDir, "unsafe.md"), []byte("Secret: ${{ secrets.FRONTMATTER_IMPORT_TOKEN }}"), 0644))

	compiler := NewCompiler()
	data := &WorkflowData{
		MarkdownContent: "No inline runtime imports.",
		ImportPaths:     []string{".github/shared/unsafe.md"},
	}

	err := compiler.validateExpressions(data, filepath.Join(workflowsDir, "workflow.md"))

	require.Error(t, err)
	require.ErrorContains(t, err, "FRONTMATTER_IMPORT_TOKEN")
}

func TestValidateExpressionsValidatesLegacyRuntimeImportMacros(t *testing.T) {
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	sharedDir := filepath.Join(tmpDir, ".github", "shared")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))
	require.NoError(t, os.MkdirAll(sharedDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(sharedDir, "unsafe.md"), []byte("Secret: ${{ secrets.LEGACY_IMPORT_TOKEN }}"), 0644))

	compiler := NewCompiler()
	data := &WorkflowData{
		MarkdownContent: "{{#import: shared/unsafe.md}}",
	}

	err := compiler.validateExpressions(data, filepath.Join(workflowsDir, "workflow.md"))

	require.Error(t, err)
	require.ErrorContains(t, err, "LEGACY_IMPORT_TOKEN")
}

// TestCompilerIntegration_RuntimeImportValidation tests the compiler integration
func TestCompilerIntegration_RuntimeImportValidation(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()
	githubDir := filepath.Join(tmpDir, ".github")
	workflowsDir := filepath.Join(githubDir, "workflows")
	sharedDir := filepath.Join(githubDir, "shared")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))
	require.NoError(t, os.MkdirAll(sharedDir, 0755))

	// Create a shared file with invalid expression
	sharedFile := filepath.Join(sharedDir, "instructions.md")
	sharedContent := `# Shared Instructions

Use this token: ${{ secrets.GITHUB_TOKEN }}
`
	require.NoError(t, os.WriteFile(sharedFile, []byte(sharedContent), 0644))

	// Create a workflow file that imports the shared file
	workflowFile := filepath.Join(workflowsDir, "test-workflow.md")
	workflowContent := `---
on:
  issues:
    types: [opened]
engine: copilot
---

# Test Workflow

{{#runtime-import ./shared/instructions.md}}

Please process the issue.
`
	require.NoError(t, os.WriteFile(workflowFile, []byte(workflowContent), 0644))

	// Create compiler and attempt to compile
	compiler := NewCompiler()

	err := compiler.CompileWorkflow(workflowFile)

	// Should fail due to invalid expression in runtime-import file
	require.Error(t, err, "Compilation should fail due to invalid expression in runtime-import file")
	require.ErrorContains(t, err, "runtime-import files contain expression errors", "Error should mention runtime-import files")
	require.ErrorContains(t, err, "secrets.GITHUB_TOKEN", "Error should mention the specific invalid expression")
}

// TestCompilerIntegration_RuntimeImportValidation_Valid tests successful compilation
func TestCompilerIntegration_RuntimeImportValidation_Valid(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()
	githubDir := filepath.Join(tmpDir, ".github")
	workflowsDir := filepath.Join(githubDir, "workflows")
	sharedDir := filepath.Join(githubDir, "shared")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))
	require.NoError(t, os.MkdirAll(sharedDir, 0755))

	// Create a shared file with valid expressions
	sharedFile := filepath.Join(sharedDir, "instructions.md")
	sharedContent := `# Shared Instructions

Actor: ${{ github.actor }}
Repository: ${{ github.repository }}
Issue: ${{ github.event.issue.number }}
`
	require.NoError(t, os.WriteFile(sharedFile, []byte(sharedContent), 0644))

	// Create a workflow file that imports the shared file
	workflowFile := filepath.Join(workflowsDir, "test-workflow.md")
	workflowContent := `---
on:
  issues:
    types: [opened]
engine: copilot
---

# Test Workflow

{{#runtime-import ./shared/instructions.md}}

Please process the issue.
`
	require.NoError(t, os.WriteFile(workflowFile, []byte(workflowContent), 0644))

	// Create compiler and compile
	compiler := NewCompiler()

	err := compiler.CompileWorkflow(workflowFile)

	// Should succeed - all expressions are valid
	require.NoError(t, err, "Compilation should succeed with valid expressions in runtime-import file")

	// Clean up lock file if it was created
	if err == nil {
		lockFile := strings.Replace(workflowFile, ".md", ".lock.yml", 1)
		os.Remove(lockFile)
	}
}
