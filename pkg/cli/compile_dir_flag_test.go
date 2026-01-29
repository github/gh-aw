//go:build integration

package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestCompileWithDirFlag tests the --dir flag functionality
func TestCompileWithDirFlag(t *testing.T) {
	// Save current directory and defer restoration
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current working directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalWd)
	}()

	// Create a temporary git repository with custom workflow directory
	tmpDir, err := os.MkdirTemp("", "dir-flag-test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Change to temp directory
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Initialize git repository properly
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to initialize git repository: %v", err)
	}

	// Create custom workflow directory
	customDir := "my-workflows"
	if err := os.MkdirAll(customDir, 0755); err != nil {
		t.Fatalf("Failed to create custom workflow directory: %v", err)
	}

	// Create a test workflow file
	workflowContent := `---
on: push
---

# Test Workflow

This is a test workflow in a custom directory.
`
	workflowFile := filepath.Join(customDir, "test.md")
	if err := os.WriteFile(workflowFile, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to create test workflow file: %v", err)
	}

	// Test: Compile with --dir flag should work
	config := CompileConfig{
		MarkdownFiles:        []string{},
		Verbose:              false,
		EngineOverride:       "",
		Validate:             false,
		Watch:                false,
		WorkflowDir:          customDir,
		SkipInstructions:     false,
		NoEmit:               false,
		Purge:                false,
		TrialMode:            false,
		TrialLogicalRepoSlug: "",
	}
	_, err = CompileWorkflows(context.Background(), config)
	if err != nil {
		t.Errorf("CompileWorkflows with --dir should succeed, got error: %v", err)
	}

	// Verify the lock file was created
	lockFile := filepath.Join(customDir, "test.lock.yml")
	if _, err := os.Stat(lockFile); os.IsNotExist(err) {
		t.Error("Expected lock file to be created in custom directory")
	}
}

// TestCompileDirFlagValidation tests the validation of --dir flag
func TestCompileDirFlagValidation(t *testing.T) {
	tests := []struct {
		name        string
		workflowDir string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "relative path is valid",
			workflowDir: "custom/workflows",
			expectError: false,
		},
		{
			name:        "absolute path is invalid",
			workflowDir: "/absolute/path",
			expectError: true,
			errorMsg:    "--dir must be a relative path, got: /absolute/path",
		},
		{
			name:        "empty string defaults to .github/workflows",
			workflowDir: "",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for each test
			tmpDir, err := os.MkdirTemp("", "dir-flag-validation-test")
			if err != nil {
				t.Fatalf("Failed to create temp directory: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			originalWd, err := os.Getwd()
			if err != nil {
				t.Fatalf("Failed to get current working directory: %v", err)
			}
			defer func() {
				_ = os.Chdir(originalWd)
			}()

			if err := os.Chdir(tmpDir); err != nil {
				t.Fatalf("Failed to change to temp directory: %v", err)
			}

			// Initialize git repository properly
			cmd := exec.Command("git", "init")
			cmd.Dir = tmpDir
			if err := cmd.Run(); err != nil {
				t.Fatalf("Failed to initialize git repository: %v", err)
			}

			// For non-error cases, create the expected directory
			if !tt.expectError {
				expectedDir := tt.workflowDir
				if expectedDir == "" {
					expectedDir = ".github/workflows"
				}
				if err := os.MkdirAll(expectedDir, 0755); err != nil {
					t.Fatalf("Failed to create workflow directory: %v", err)
				}
				// Create a placeholder workflow file
				workflowFile := filepath.Join(expectedDir, "test.md")
				workflowContent := `---
on: push
---

# Test Workflow
`
				if err := os.WriteFile(workflowFile, []byte(workflowContent), 0644); err != nil {
					t.Fatalf("Failed to create test workflow file: %v", err)
				}
			}

			// Test the compilation
			config := CompileConfig{
				MarkdownFiles:        []string{},
				Verbose:              false,
				EngineOverride:       "",
				Validate:             false,
				Watch:                false,
				WorkflowDir:          tt.workflowDir,
				SkipInstructions:     false,
				NoEmit:               false,
				Purge:                false,
				TrialMode:            false,
				TrialLogicalRepoSlug: "",
			}
			_, err = CompileWorkflows(context.Background(), config)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for --dir '%s', but got none", tt.workflowDir)
				} else if err.Error() != tt.errorMsg {
					t.Errorf("Expected error message '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for --dir '%s', but got: %v", tt.workflowDir, err)
				}
			}
		})
	}
}
