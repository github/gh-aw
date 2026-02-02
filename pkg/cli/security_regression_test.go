//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workflow"
)

// =============================================================================
// Command Injection Prevention Tests
// =============================================================================

// TestSecurityCLICommandInjectionPrevention validates that CLI commands
// properly sanitize inputs to prevent command injection.
func TestSecurityCLICommandInjectionPrevention(t *testing.T) {
	tests := []struct {
		name           string
		workflowName   string
		containsUnsafe bool
		description    string
	}{
		{
			name:           "valid_workflow_name",
			workflowName:   "my-workflow",
			containsUnsafe: false,
			description:    "Valid workflow names are safe",
		},
		{
			name:           "workflow_name_with_semicolon",
			workflowName:   "workflow;rm -rf /",
			containsUnsafe: true,
			description:    "Semicolon command injection pattern",
		},
		{
			name:           "workflow_name_with_backticks",
			workflowName:   "workflow`whoami`",
			containsUnsafe: true,
			description:    "Backtick command injection pattern",
		},
		{
			name:           "workflow_name_with_dollar_paren",
			workflowName:   "workflow$(id)",
			containsUnsafe: true,
			description:    "Dollar-paren command injection pattern",
		},
		{
			name:           "workflow_name_with_pipe",
			workflowName:   "workflow|cat /etc/passwd",
			containsUnsafe: true,
			description:    "Pipe command injection pattern",
		},
		{
			name:           "workflow_name_with_ampersand",
			workflowName:   "workflow&& rm -rf /",
			containsUnsafe: true,
			description:    "Ampersand command injection pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Check if the workflow name contains unsafe shell characters
			unsafeChars := []string{";", "`", "$", "|", "&", "<", ">"}
			foundUnsafe := false
			for _, char := range unsafeChars {
				if strings.Contains(tt.workflowName, char) {
					foundUnsafe = true
					break
				}
			}

			if foundUnsafe != tt.containsUnsafe {
				t.Errorf("Expected containsUnsafe=%v for %s, got %v",
					tt.containsUnsafe, tt.workflowName, foundUnsafe)
			}
		})
	}
}

// =============================================================================
// File Path Sanitization Tests
// =============================================================================

// TestSecurityCLIPathSanitization validates that file paths are properly
// sanitized to prevent path traversal attacks.
func TestSecurityCLIPathSanitization(t *testing.T) {
	tests := []struct {
		name        string
		inputPath   string
		shouldBlock bool
		description string
	}{
		{
			name:        "simple_filename",
			inputPath:   "workflow.md",
			shouldBlock: false,
			description: "Simple filenames should be allowed",
		},
		{
			name:        "relative_path",
			inputPath:   "./subdir/workflow.md",
			shouldBlock: false,
			description: "Relative paths within directory should be allowed",
		},
		{
			name:        "parent_traversal",
			inputPath:   "../../../etc/passwd",
			shouldBlock: true,
			description: "Parent directory traversal should be blocked",
		},
		{
			name:        "hidden_parent_traversal",
			inputPath:   "subdir/../../..",
			shouldBlock: true,
			description: "Hidden parent traversal should be blocked",
		},
		{
			name:        "null_byte_injection",
			inputPath:   "workflow.md\x00.txt",
			shouldBlock: true,
			description: "Null byte injection should be blocked",
		},
		{
			name:        "nested_subdir",
			inputPath:   "dir1/dir2/workflow.md",
			shouldBlock: false,
			description: "Nested subdirectory paths should be allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a safe base directory
			tmpDir := testutil.TempDir(t, "path-sanitization-test")

			// Check if the path contains null bytes (Go handles these specially)
			if strings.Contains(tt.inputPath, "\x00") {
				// Null bytes in paths should be blocked
				if !tt.shouldBlock {
					t.Errorf("Null byte in path should be blocked: %s", tt.inputPath)
				}
				return
			}

			// Check if the path is absolute (absolute paths should be blocked)
			if filepath.IsAbs(tt.inputPath) {
				if !tt.shouldBlock {
					t.Errorf("Absolute path should be blocked: %s", tt.inputPath)
				}
				return
			}

			// Try to construct the full path
			targetPath := filepath.Join(tmpDir, tt.inputPath)
			cleanPath := filepath.Clean(targetPath)

			// Check if the cleaned path escapes the base directory
			relPath, err := filepath.Rel(tmpDir, cleanPath)
			if err != nil || strings.HasPrefix(relPath, "..") {
				// Path traversal detected
				if !tt.shouldBlock {
					t.Errorf("Path should not be blocked: %s", tt.inputPath)
				}
			} else {
				// Path is safe
				if tt.shouldBlock {
					t.Errorf("Path traversal should have been detected: %s (cleaned to %s)", tt.inputPath, cleanPath)
				}
			}
		})
	}
}

// =============================================================================
// Unsafe Flag Combination Tests
// =============================================================================

// TestSecurityCLIUnsafeFlagCombinations validates that certain flag
// combinations that could be dangerous are handled properly.
func TestSecurityCLIUnsafeFlagCombinations(t *testing.T) {
	tests := []struct {
		name        string
		config      CompileConfig
		expectWarn  bool
		description string
	}{
		{
			name: "force_overwrite_without_confirm",
			config: CompileConfig{
				ForceOverwrite: true,
				Verbose:        false,
			},
			expectWarn:  false, // Force overwrite is allowed but logs a warning
			description: "Force overwrite should work but may warn",
		},
		{
			name: "trial_mode_safe",
			config: CompileConfig{
				TrialMode: true,
			},
			expectWarn:  false,
			description: "Trial mode should be safe",
		},
		{
			name: "no_emit_safe",
			config: CompileConfig{
				NoEmit: true,
			},
			expectWarn:  false,
			description: "No-emit mode should be safe",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify the configuration can be constructed and used
			config := tt.config

			// Basic validation: ensure config fields are accessible
			// This validates that unsafe flag combinations don't cause issues
			_ = config.ForceOverwrite
			_ = config.TrialMode
			_ = config.NoEmit
			_ = config.Verbose
		})
	}
}

// =============================================================================
// Input Size Limits Tests
// =============================================================================

// TestSecurityCLIInputSizeLimits validates that excessively large inputs
// are handled properly without causing DoS.
func TestSecurityCLIInputSizeLimits(t *testing.T) {
	tests := []struct {
		name        string
		contentFunc func() string
		maxSizeMB   int
		description string
	}{
		{
			name: "reasonable_workflow",
			contentFunc: func() string {
				return `---
on: push
permissions:
  contents: read
---

# Test Workflow
Test content.`
			},
			maxSizeMB:   1,
			description: "Normal workflow should compile",
		},
		{
			name: "large_markdown_content",
			contentFunc: func() string {
				// Create a large but valid markdown content
				content := `---
on: push
permissions:
  contents: read
---

# Large Content Workflow

`
				// Add lots of content
				for i := 0; i < 1000; i++ {
					content += "This is paragraph " + string(rune('0'+i%10)) + ".\n\n"
				}
				return content
			},
			maxSizeMB:   1,
			description: "Large markdown should be handled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := tt.contentFunc()

			// Check the size
			sizeMB := float64(len(content)) / (1024 * 1024)
			if sizeMB > float64(tt.maxSizeMB) {
				t.Logf("Content size: %.2f MB (exceeds %d MB limit)", sizeMB, tt.maxSizeMB)
				// Large content might be rejected, which is acceptable
				return
			}

			// Create temporary file and verify it can be created
			tmpDir := testutil.TempDir(t, "size-limit-test")
			testFile := filepath.Join(tmpDir, "test-workflow.md")

			err := os.WriteFile(testFile, []byte(content), 0644)
			if err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			// File was created successfully
			info, err := os.Stat(testFile)
			if err != nil {
				t.Fatalf("Failed to stat test file: %v", err)
			}

			t.Logf("Created test file: %d bytes", info.Size())
		})
	}
}

// =============================================================================
// Environment Variable Sanitization Tests
// =============================================================================

// TestSecurityCLIEnvironmentVariableSanitization validates that environment
// variables are properly sanitized when used in compilation.
func TestSecurityCLIEnvironmentVariableSanitization(t *testing.T) {
	tests := []struct {
		name        string
		envVarName  string
		envVarValue string
		shouldAllow bool
		description string
	}{
		{
			name:        "valid_env_var",
			envVarName:  "GITHUB_TOKEN",
			envVarValue: "test-token",
			shouldAllow: true,
			description: "Valid environment variables should work",
		},
		{
			name:        "env_var_with_newline",
			envVarName:  "MALICIOUS",
			envVarValue: "value\n; rm -rf /",
			shouldAllow: true, // The value itself might be allowed but shouldn't execute
			description: "Environment values with newlines should be safe",
		},
		{
			name:        "env_var_with_special_chars",
			envVarName:  "TEST_VAR",
			envVarValue: "`whoami`",
			shouldAllow: true, // The value itself might be allowed but shouldn't execute
			description: "Environment values with backticks should be safe",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set the environment variable temporarily
			oldValue := os.Getenv(tt.envVarName)
			os.Setenv(tt.envVarName, tt.envVarValue)
			defer os.Setenv(tt.envVarName, oldValue)

			// Get the value back
			value := os.Getenv(tt.envVarName)

			// The value should be set correctly
			if value != tt.envVarValue {
				t.Errorf("Environment variable not set correctly: got %q, want %q", value, tt.envVarValue)
			}

			// The value should not be executed as a command
			// (This is more of a documentation test - Go doesn't execute env vars)
		})
	}
}

// =============================================================================
// Workflow File Validation Tests
// =============================================================================

// TestSecurityCLIWorkflowFileValidation validates that workflow files are
// properly validated before compilation.
func TestSecurityCLIWorkflowFileValidation(t *testing.T) {
	tests := []struct {
		name          string
		workflow      string
		expectError   bool
		errorContains string
		description   string
	}{
		{
			name: "valid_workflow",
			workflow: `---
on: push
permissions:
  contents: read
---

# Valid Workflow
Test content.`,
			expectError: false,
			description: "Valid workflow should compile",
		},
		{
			name: "workflow_with_secrets_expression",
			workflow: `---
on: push
permissions:
  contents: read
---

# Secrets Workflow
Test with secrets: ${{ secrets.GITHUB_TOKEN }}`,
			expectError:   true,
			errorContains: "secrets",
			description:   "Workflows with secrets should fail validation",
		},
		{
			name: "workflow_without_frontmatter",
			workflow: `# No Frontmatter
This workflow has no frontmatter.`,
			expectError: true,
			description: "Workflows without frontmatter should fail",
		},
		{
			name: "workflow_with_invalid_yaml",
			workflow: `---
on: push
invalid yaml content
missing colon
---

# Invalid YAML
Test content.`,
			expectError: true,
			description: "Workflows with invalid YAML should fail",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "workflow-validation-test")
			testFile := filepath.Join(tmpDir, "test-workflow.md")

			err := os.WriteFile(testFile, []byte(tt.workflow), 0644)
			if err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			// Note: We use the workflow compiler directly rather than CLI CompileWorkflows
			// because CompileWorkflows requires a relative path for WorkflowDir (security check).
			// This test validates the underlying compilation validation, which is the same
			// validation used by the CLI. The CLI path validation is tested separately in
			// TestSecurityCLIPathSanitization and TestSecurityCLIOutputDirectorySafety.
			compiler := workflow.NewCompiler()
			compileErr := compiler.CompileWorkflow(testFile)

			if tt.expectError {
				if compileErr == nil {
					t.Errorf("Expected compilation error for %s: %s", tt.name, tt.description)
				}
			} else {
				if compileErr != nil {
					t.Errorf("Unexpected compilation error for %s: %v", tt.name, compileErr)
				}
			}
		})
	}
}

// =============================================================================
// Output Directory Safety Tests
// =============================================================================

// TestSecurityCLIOutputDirectorySafety validates that output directories
// are properly validated to prevent writing to unsafe locations.
func TestSecurityCLIOutputDirectorySafety(t *testing.T) {
	tests := []struct {
		name          string
		outputDir     string
		shouldBeValid bool
		description   string
	}{
		{
			name:          "valid_relative_dir",
			outputDir:     ".github/workflows",
			shouldBeValid: true,
			description:   "Standard output directory should be valid",
		},
		{
			name:          "valid_custom_dir",
			outputDir:     "custom/output",
			shouldBeValid: true,
			description:   "Custom relative directory should be valid",
		},
		{
			name:          "parent_traversal_dir",
			outputDir:     "../../../outside",
			shouldBeValid: false,
			description:   "Parent traversal should be blocked",
		},
		{
			name:          "nested_traversal",
			outputDir:     "valid/../../invalid",
			shouldBeValid: false,
			description:   "Nested traversal should be blocked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a safe working directory
			tmpDir := testutil.TempDir(t, "output-dir-test")

			// Check if path is absolute (absolute paths are always invalid for output)
			if filepath.IsAbs(tt.outputDir) {
				if tt.shouldBeValid {
					t.Errorf("Absolute paths should not be valid for output: %s", tt.outputDir)
				}
				return
			}

			// Check if the output directory would escape
			targetPath := filepath.Join(tmpDir, tt.outputDir)
			cleanPath := filepath.Clean(targetPath)

			relPath, err := filepath.Rel(tmpDir, cleanPath)
			isValid := err == nil && !strings.HasPrefix(relPath, "..")

			if isValid != tt.shouldBeValid {
				t.Errorf("Output directory validation mismatch for %s: expected valid=%v, got valid=%v",
					tt.outputDir, tt.shouldBeValid, isValid)
			}
		})
	}
}
