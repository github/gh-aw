//go:build integration

package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/testutil"
)

// Test the CLI functions that are exported from this package

func TestCompileWorkflows(t *testing.T) {
	// Clean up any existing .github/workflows for this test
	defer os.RemoveAll(".github")

	tests := []struct {
		name         string
		markdownFile string
		expectError  bool
	}{
		{
			name:         "nonexistent specific file",
			markdownFile: "nonexistent.md",
			expectError:  true, // Should error when file doesn't exist
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var args []string
			if tt.markdownFile != "" {
				args = []string{tt.markdownFile}
			}
			config := CompileConfig{
				MarkdownFiles:        args,
				Verbose:              false,
				EngineOverride:       "",
				Validate:             false,
				Watch:                false,
				WorkflowDir:          "",
				SkipInstructions:     false,
				NoEmit:               false,
				Purge:                false,
				TrialMode:            false,
				TrialLogicalRepoSlug: "",
			}
			_, err := CompileWorkflows(context.Background(), config)

			if tt.expectError && err == nil {
				t.Errorf("Expected error for test '%s', got nil", tt.name)
			} else if !tt.expectError && err != nil {
				t.Errorf("Unexpected error for test '%s': %v", tt.name, err)
			}
		})
	}
}

func TestCompileWorkflowsPurgeFlag(t *testing.T) {
	t.Run("purge flag validation with specific files", func(t *testing.T) {
		// Test that purge flag is rejected when specific files are provided
		config := CompileConfig{
			MarkdownFiles:        []string{"test.md"},
			Verbose:              false,
			EngineOverride:       "",
			Validate:             false,
			Watch:                false,
			WorkflowDir:          "",
			SkipInstructions:     false,
			NoEmit:               false,
			Purge:                true,
			TrialMode:            false,
			TrialLogicalRepoSlug: "",
		}
		_, err := CompileWorkflows(context.Background(), config)

		if err == nil {
			t.Error("Expected error when using --purge with specific files, got nil")
		}

		expectedMsg := "--purge flag can only be used when compiling all markdown files"
		if !strings.Contains(err.Error(), expectedMsg) {
			t.Errorf("Expected error message to contain '%s', got: %v", expectedMsg, err)
		}
	})

	t.Run("purge flag acceptance without specific files", func(t *testing.T) {
		// Create temporary directory structure for testing
		tempDir := testutil.TempDir(t, "test-*")
		workflowsDir := filepath.Join(tempDir, ".github/workflows")
		os.MkdirAll(workflowsDir, 0755)

		// Change to temp directory to simulate being in a git repo
		originalDir, _ := os.Getwd()
		defer os.Chdir(originalDir)
		os.Chdir(tempDir)

		// Create .git directory to make it look like a git repo
		os.MkdirAll(".git", 0755)

		// Test should not error when no specific files are provided with purge flag
		// Note: This will still error because there are no .md files, but it shouldn't
		// error specifically because of the purge flag validation
		config := CompileConfig{
			MarkdownFiles:        []string{},
			Verbose:              false,
			EngineOverride:       "",
			Validate:             false,
			Watch:                false,
			WorkflowDir:          "",
			SkipInstructions:     false,
			NoEmit:               false,
			Purge:                true,
			TrialMode:            false,
			TrialLogicalRepoSlug: "",
		}
		_, err := CompileWorkflows(context.Background(), config)

		if err != nil {
			// The error should NOT be about purge flag validation
			if strings.Contains(err.Error(), "--purge flag can only be used") {
				t.Errorf("Purge flag validation should not trigger when no specific files provided, got: %v", err)
			}
		}
	})
}

func TestCompileWorkflowsWithNoEmit(t *testing.T) {
	defer os.RemoveAll(".github")

	// Create test directory and workflow
	err := os.MkdirAll(".github/workflows", 0755)
	if err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Create a simple test workflow
	workflowContent := `---
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
---

# Test Workflow for No Emit

This is a test workflow to verify the --no-emit flag functionality.`

	err = os.WriteFile(".github/workflows/no-emit-test.md", []byte(workflowContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test workflow file: %v", err)
	}

	// Test compilation with noEmit = false (should create lock file)
	config := CompileConfig{
		MarkdownFiles:        []string{"no-emit-test"},
		Verbose:              false,
		EngineOverride:       "",
		Validate:             false,
		Watch:                false,
		WorkflowDir:          "",
		SkipInstructions:     false,
		NoEmit:               false,
		Purge:                false,
		TrialMode:            false,
		TrialLogicalRepoSlug: "",
	}
	_, err = CompileWorkflows(context.Background(), config)
	if err != nil {
		t.Errorf("CompileWorkflows with noEmit=false should not error, got: %v", err)
	}

	// Verify lock file was created
	if _, err := os.Stat(".github/workflows/no-emit-test.lock.yml"); os.IsNotExist(err) {
		t.Error("Lock file should have been created when noEmit=false")
	}

	// Remove lock file
	os.Remove(".github/workflows/no-emit-test.lock.yml")

	// Test compilation with noEmit = true (should NOT create lock file)
	config2 := CompileConfig{
		MarkdownFiles:        []string{"no-emit-test"},
		Verbose:              false,
		EngineOverride:       "",
		Validate:             false,
		Watch:                false,
		WorkflowDir:          "",
		SkipInstructions:     false,
		NoEmit:               true,
		Purge:                false,
		TrialMode:            false,
		TrialLogicalRepoSlug: "",
	}
	_, err = CompileWorkflows(context.Background(), config2)
	if err != nil {
		t.Errorf("CompileWorkflows with noEmit=true should not error, got: %v", err)
	}

	// Verify lock file was NOT created
	if _, err := os.Stat(".github/workflows/no-emit-test.lock.yml"); !os.IsNotExist(err) {
		t.Error("Lock file should NOT have been created when noEmit=true")
	}
}

func TestRemoveWorkflows(t *testing.T) {
	err := RemoveWorkflows("test-pattern", false)

	// Should not error since it's a stub implementation
	if err != nil {
		t.Errorf("RemoveWorkflows should not return error for valid input, got: %v", err)
	}
}

func TestStatusWorkflows(t *testing.T) {
	err := StatusWorkflows("test-pattern", false, false, "", "", "")

	// Should not error since it's a stub implementation
	if err != nil {
		t.Errorf("StatusWorkflows should not return error for valid input, got: %v", err)
	}
}

func TestEnableWorkflows(t *testing.T) {
	err := EnableWorkflows("test-pattern")

	// Should now return an error since no workflows can be found to enable
	if err == nil {
		t.Errorf("EnableWorkflows should return error when no workflows found to enable, got nil")
	}

	// The error should indicate workflows couldn't be found
	if !strings.Contains(err.Error(), "workflow") {
		t.Errorf("EnableWorkflows error should mention workflows, got: %v", err)
	}
}

func TestEnableWorkflowsFailureScenarios(t *testing.T) {
	tests := []struct {
		name          string
		pattern       string
		expectError   bool
		errorContains string
		description   string
	}{
		{
			name:          "empty pattern",
			pattern:       "",
			expectError:   true,
			errorContains: "workflow",
			description:   "Should error when no workflows found to enable",
		},
		{
			name:          "nonexistent pattern",
			pattern:       "nonexistent-workflow-pattern",
			expectError:   true,
			errorContains: "workflow",
			description:   "Should error when pattern matches no workflows",
		},
		{
			name:          "wildcard pattern",
			pattern:       "xyz-*",
			expectError:   true,
			errorContains: "workflow",
			description:   "Should error when wildcard pattern matches nothing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := EnableWorkflows(tt.pattern)

			if tt.expectError {
				if err == nil {
					t.Errorf("%s: expected error but got nil", tt.description)
				} else if !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("%s: expected error containing '%s', got: %v", tt.description, tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("%s: unexpected error: %v", tt.description, err)
				}
			}
		})
	}
}

func TestDisableWorkflows(t *testing.T) {
	err := DisableWorkflows("test-pattern")

	// Disable should now also return an error when no workflows found
	if err == nil {
		t.Errorf("DisableWorkflows should return error when no workflows found to disable, got nil")
	}

	// The error should indicate workflows couldn't be found
	if !strings.Contains(err.Error(), "workflow") {
		t.Errorf("DisableWorkflows error should mention workflows, got: %v", err)
	}
}

func TestDisableWorkflowsFailureScenarios(t *testing.T) {
	// Test that disable now also errors on failure scenarios
	tests := []struct {
		name        string
		pattern     string
		description string
	}{
		{
			name:        "empty pattern",
			pattern:     "",
			description: "Should error when no workflows found to disable",
		},
		{
			name:        "nonexistent pattern",
			pattern:     "nonexistent-workflow-pattern",
			description: "Should error when pattern matches no workflows",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := DisableWorkflows(tt.pattern)
			if err == nil {
				t.Errorf("%s: DisableWorkflows should now error, got nil", tt.description)
			}
		})
	}
}

func TestRunWorkflowOnGitHub(t *testing.T) {
	// Test with empty workflow name
	err := RunWorkflowOnGitHub(context.Background(), "", false, "", "", "", false, false, false, false, []string{}, false, false)
	if err == nil {
		t.Error("RunWorkflowOnGitHub should return error for empty workflow name")
	}

	// Test with nonexistent workflow (this will fail but gracefully)
	err = RunWorkflowOnGitHub(context.Background(), "nonexistent-workflow", false, "", "", "", false, false, false, false, []string{}, false, false)
	if err == nil {
		t.Error("RunWorkflowOnGitHub should return error for non-existent workflow")
	}
}

func TestRunWorkflowsOnGitHub(t *testing.T) {
	// Test with empty workflow list
	err := RunWorkflowsOnGitHub(context.Background(), []string{}, 0, false, "", "", "", false, false, false, []string{}, false, false)
	if err == nil {
		t.Error("RunWorkflowsOnGitHub should return error for empty workflow list")
	}

	// Test with workflow list containing empty name
	err = RunWorkflowsOnGitHub(context.Background(), []string{"valid-workflow", ""}, 0, false, "", "", "", false, false, false, []string{}, false, false)
	if err == nil {
		t.Error("RunWorkflowsOnGitHub should return error for workflow list containing empty name")
	}

	// Test with nonexistent workflows (this will fail but gracefully)
	err = RunWorkflowsOnGitHub(context.Background(), []string{"nonexistent-workflow1", "nonexistent-workflow2"}, 0, false, "", "", "", false, false, false, []string{}, false, false)
	if err == nil {
		t.Error("RunWorkflowsOnGitHub should return error for non-existent workflows")
	}

	// Test with negative repeat seconds (should work as 0)
	err = RunWorkflowsOnGitHub(context.Background(), []string{"nonexistent-workflow"}, -1, false, "", "", "", false, false, false, []string{}, false, false)
	if err == nil {
		t.Error("RunWorkflowsOnGitHub should return error for non-existent workflow regardless of repeat value")
	}
}

func TestNormalizeWorkflowID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "workflow ID without extension",
			input:    "my-workflow",
			expected: "my-workflow",
		},
		{
			name:     "workflow ID with .md extension",
			input:    "my-workflow.md",
			expected: "my-workflow",
		},
		{
			name:     "full path with .md extension",
			input:    ".github/workflows/my-workflow.md",
			expected: "my-workflow",
		},
		{
			name:     "absolute path with .md extension",
			input:    "/home/user/project/.github/workflows/my-workflow.md",
			expected: "my-workflow",
		},
		{
			name:     "relative path with .md extension",
			input:    "../../.github/workflows/my-workflow.md",
			expected: "my-workflow",
		},
		{
			name:     "workflow with hyphens",
			input:    ".github/workflows/agent-performance-analyzer.md",
			expected: "agent-performance-analyzer",
		},
		{
			name:     "workflow without path but with .md",
			input:    "test-workflow.md",
			expected: "test-workflow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeWorkflowID(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeWorkflowID(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestAllCommandsExist(t *testing.T) {
	// Create a minimal test environment to avoid expensive workflow compilation
	tempDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tempDir, ".github/workflows")
	os.MkdirAll(workflowsDir, 0755)

	// Change to temp directory
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(tempDir)

	// Create a minimal test workflow to avoid "no workflows found" error
	minimalWorkflow := `---
on: workflow_dispatch
permissions:
  contents: read
---
# Test
Test workflow for command existence.`
	os.WriteFile(filepath.Join(workflowsDir, "test.md"), []byte(minimalWorkflow), 0644)

	// Test that all expected functions exist and can be called
	// This helps ensure the interface is stable

	// Test structure: function, expected to error
	tests := []struct {
		fn          func() error
		expectError bool
		name        string
	}{
		{func() error {
			config := CompileConfig{
				MarkdownFiles:        []string{"test"},
				Verbose:              false,
				EngineOverride:       "",
				Validate:             false,
				Watch:                false,
				WorkflowDir:          "",
				SkipInstructions:     false,
				NoEmit:               true, // Don't emit lock files to save time
				Purge:                false,
				TrialMode:            false,
				TrialLogicalRepoSlug: "",
			}
			_, err := CompileWorkflows(context.Background(), config)
			return err
		}, false, "CompileWorkflows"},
		{func() error { return RemoveWorkflows("nonexistent", false) }, false, "RemoveWorkflows"},                    // Should handle missing directory gracefully
		{func() error { return StatusWorkflows("nonexistent", false, false, "", "", "") }, false, "StatusWorkflows"}, // Should handle missing directory gracefully
		{func() error { return EnableWorkflows("nonexistent") }, true, "EnableWorkflows"},                            // Should now error when no workflows found to enable
		{func() error { return DisableWorkflows("nonexistent") }, true, "DisableWorkflows"},                          // Should now also error when no workflows found to disable
		{func() error {
			return RunWorkflowOnGitHub(context.Background(), "", false, "", "", "", false, false, false, false, []string{}, false, false)
		}, true, "RunWorkflowOnGitHub"}, // Should error with empty workflow name
		{func() error {
			return RunWorkflowsOnGitHub(context.Background(), []string{}, 0, false, "", "", "", false, false, false, []string{}, false, false)
		}, true, "RunWorkflowsOnGitHub"}, // Should error with empty workflow list
	}

	for _, test := range tests {
		err := test.fn()
		if test.expectError && err == nil {
			t.Errorf("%s: expected error but got nil", test.name)
		} else if !test.expectError && err != nil {
			t.Errorf("%s: unexpected error: %v", test.name, err)
		}
	}
}

// TestInstallPackage tests the InstallPackage function
func TestInstallPackage(t *testing.T) {

	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "gh-aw-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Mock the getPackagesDir function by temporarily changing directory
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	tests := []struct {
		name        string
		repoSpec    string
		verbose     bool
		expectError bool
		errorMsg    string
	}{
		{
			name:        "invalid repo spec",
			repoSpec:    "invalid",
			verbose:     false,
			expectError: true,
			errorMsg:    "invalid repository specification",
		},
		{
			name:        "empty repo spec",
			repoSpec:    "",
			verbose:     false,
			expectError: true,
			errorMsg:    "invalid repository specification",
		},
		{
			name:        "valid repo spec but download will fail",
			repoSpec:    "nonexistent/repo",
			verbose:     true,
			expectError: true,
			errorMsg:    "failed to download workflows",
		},
		{
			name:        "valid repo spec with version but download will fail",
			repoSpec:    "nonexistent/repo@v1.0.0",
			verbose:     false,
			expectError: true,
			errorMsg:    "failed to download workflows",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := InstallPackage(tt.repoSpec, tt.verbose)

			if tt.expectError && err == nil {
				t.Errorf("Expected error for test '%s', got nil", tt.name)
			} else if !tt.expectError && err != nil {
				t.Errorf("Unexpected error for test '%s': %v", tt.name, err)
			}

			if tt.expectError && err != nil {
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', got: %v", tt.errorMsg, err)
				}
			}
		})
	}
}

func TestNewWorkflow(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "test-new-workflow-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Change to the temp directory
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Logf("Warning: Failed to restore working directory: %v", err)
		}
	}()

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	tests := []struct {
		name          string
		workflowName  string
		force         bool
		expectedError bool
		setup         func(t *testing.T)
	}{
		{
			name:          "create new workflow",
			workflowName:  "test-workflow",
			force:         false,
			expectedError: false,
		},
		{
			name:          "fail to overwrite existing workflow without force",
			workflowName:  "existing-workflow",
			force:         false,
			expectedError: true,
			setup: func(t *testing.T) {
				// Create an existing workflow file
				os.MkdirAll(".github/workflows", 0755)
				os.WriteFile(".github/workflows/existing-workflow.md", []byte("test"), 0644)
			},
		},
		{
			name:          "overwrite existing workflow with force",
			workflowName:  "force-workflow",
			force:         true,
			expectedError: false,
			setup: func(t *testing.T) {
				// Create an existing workflow file
				if err := os.MkdirAll(".github/workflows", 0755); err != nil {
					t.Fatalf("Failed to create workflows directory: %v", err)
				}
				if err := os.WriteFile(".github/workflows/force-workflow.md", []byte("old content"), 0644); err != nil {
					t.Fatalf("Failed to create existing workflow file: %v", err)
				}
			},
		},
		{
			name:          "create workflow with .md extension (should normalize)",
			workflowName:  "test-with-ext.md",
			force:         false,
			expectedError: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Setup if needed
			if test.setup != nil {
				test.setup(t)
			}

			// Run the function
			err := NewWorkflow(test.workflowName, false, test.force)

			// Check error expectation
			if test.expectedError && err == nil {
				t.Errorf("Expected error but got none")
			} else if !test.expectedError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			// If no error expected, verify the file was created
			if !test.expectedError {
				// Normalize the workflow name for file path (remove .md if present)
				normalizedName := strings.TrimSuffix(test.workflowName, ".md")
				filePath := ".github/workflows/" + normalizedName + ".md"
				if _, err := os.Stat(filePath); os.IsNotExist(err) {
					t.Errorf("Expected workflow file was not created: %s", filePath)
				}

				// Verify the content contains expected template elements
				content, err := os.ReadFile(filePath)
				if err != nil {
					t.Errorf("Failed to read created workflow file: %v", err)
				} else {
					contentStr := string(content)
					// Check for key template elements
					expectedElements := []string{
						"# Trigger - when should this workflow run?",
						"on:",
						"permissions:",
						"safe-outputs:",
						"# " + normalizedName,
						"workflow_dispatch:",
					}
					for _, element := range expectedElements {
						if !strings.Contains(contentStr, element) {
							t.Errorf("Template missing expected element: %s", element)
						}
					}
				}
			}

			// Clean up for next test
			os.RemoveAll(".github")
		})
	}
}

// Test SetVersionInfo and GetVersion functions
func TestSetVersionInfo(t *testing.T) {
	// Save original version to restore after test
	originalVersion := GetVersion()
	defer SetVersionInfo(originalVersion)

	tests := []struct {
		name    string
		version string
	}{
		{
			name:    "normal version",
			version: "1.0.0",
		},
		{
			name:    "empty version",
			version: "",
		},
		{
			name:    "version with pre-release",
			version: "2.0.0-beta.1",
		},
		{
			name:    "version with build metadata",
			version: "1.2.3+20240808",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetVersionInfo(tt.version)
			got := GetVersion()
			if got != tt.version {
				t.Errorf("SetVersionInfo(%q) -> GetVersion() = %q, want %q", tt.version, got, tt.version)
			}
		})
	}
}

// TestCleanupOrphanedIncludes tests that root workflow files are not removed as "orphaned" includes
func TestCleanupOrphanedIncludes(t *testing.T) {
	// Create a temporary directory structure
	tmpDir, err := os.MkdirTemp("", "test-cleanup-orphaned")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create .github/workflows directory
	workflowsDir := filepath.Join(tmpDir, constants.GetWorkflowDir())
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create shared subdirectory
	sharedDir := filepath.Join(workflowsDir, "shared")
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create root workflow files (these should NOT be considered orphaned)
	rootWorkflows := []string{"daily-plan.md", "weekly-research.md", "action-workflow-assessor.md"}
	for _, name := range rootWorkflows {
		content := fmt.Sprintf(`---
on:
  workflow_dispatch:
---

# %s

This is a root workflow.
`, strings.TrimSuffix(name, ".md"))
		if err := os.WriteFile(filepath.Join(workflowsDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Create include files in shared/ directory (these should be considered orphaned if not used)
	includeFiles := []string{"shared/common.md", "shared/tools.md"}
	for _, name := range includeFiles {
		content := `---
tools:
  github:
    allowed: []
---

This is an include file.
`
		if err := os.WriteFile(filepath.Join(workflowsDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Create one root workflow that actually uses an include
	workflowWithInclude := `---
on:
  workflow_dispatch:
---

# Workflow with Include

@include shared/common.md

This workflow uses an include.
`
	if err := os.WriteFile(filepath.Join(workflowsDir, "workflow-with-include.md"), []byte(workflowWithInclude), 0644); err != nil {
		t.Fatal(err)
	}

	// Change to the temporary directory to simulate the git root
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Logf("Warning: Failed to restore working directory: %v", err)
		}
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// Run cleanup
	err = cleanupOrphanedIncludes(true)
	if err != nil {
		t.Fatalf("cleanupOrphanedIncludes failed: %v", err)
	}

	// Verify that root workflow files still exist
	for _, name := range rootWorkflows {
		filePath := filepath.Join(workflowsDir, name)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			t.Errorf("Root workflow file %s was incorrectly removed as orphaned", name)
		}
	}

	// Verify that workflow-with-include.md still exists
	if _, err := os.Stat(filepath.Join(workflowsDir, "workflow-with-include.md")); os.IsNotExist(err) {
		t.Error("Workflow with include was incorrectly removed")
	}

	// Verify that shared/common.md still exists (it's used by workflow-with-include.md)
	if _, err := os.Stat(filepath.Join(workflowsDir, "shared", "common.md")); os.IsNotExist(err) {
		t.Error("Used include file shared/common.md was incorrectly removed")
	}

	// Verify that shared/tools.md was removed (it's truly orphaned)
	if _, err := os.Stat(filepath.Join(workflowsDir, "shared", "tools.md")); !os.IsNotExist(err) {
		t.Error("Orphaned include file shared/tools.md was not removed")
	}
}

// TestPreviewOrphanedIncludes tests the preview functionality for orphaned includes
func TestPreviewOrphanedIncludes(t *testing.T) {
	// Create a temporary directory structure
	tmpDir, err := os.MkdirTemp("", "test-preview-orphaned")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create .github/workflows directory
	workflowsDir := filepath.Join(tmpDir, constants.GetWorkflowDir())
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create shared subdirectory
	sharedDir := filepath.Join(workflowsDir, "shared")
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create workflow files
	workflow1 := `---
on:
  workflow_dispatch:
---

# Workflow 1

@include shared/common.md

This workflow uses common include.
`
	if err := os.WriteFile(filepath.Join(workflowsDir, "workflow1.md"), []byte(workflow1), 0644); err != nil {
		t.Fatal(err)
	}

	workflow2 := `---
on:
  workflow_dispatch:
---

# Workflow 2

@include shared/tools.md

This workflow uses tools include.
`
	if err := os.WriteFile(filepath.Join(workflowsDir, "workflow2.md"), []byte(workflow2), 0644); err != nil {
		t.Fatal(err)
	}

	workflow3 := `---
on:
  workflow_dispatch:
---

# Workflow 3

@include shared/common.md

This workflow also uses common include.
`
	if err := os.WriteFile(filepath.Join(workflowsDir, "workflow3.md"), []byte(workflow3), 0644); err != nil {
		t.Fatal(err)
	}

	// Create include files
	includeFiles := map[string]string{
		"shared/common.md": "Common include content",
		"shared/tools.md":  "Tools include content",
		"shared/unused.md": "Unused include content",
	}
	for name, content := range includeFiles {
		if err := os.WriteFile(filepath.Join(workflowsDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Change to the temporary directory
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Logf("Warning: Failed to restore working directory: %v", err)
		}
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// Test Case 1: Remove workflow2 - should orphan shared/tools.md but not shared/common.md
	// Use relative paths like the real RemoveWorkflows function does
	filesToRemove := []string{".github/workflows/workflow2.md"}
	orphaned, err := previewOrphanedIncludes(filesToRemove, false)
	if err != nil {
		t.Fatalf("previewOrphanedIncludes failed: %v", err)
	}

	expectedOrphaned := []string{"shared/tools.md", "shared/unused.md"}
	if len(orphaned) != len(expectedOrphaned) {
		t.Errorf("Expected %d orphaned includes, got %d: %v", len(expectedOrphaned), len(orphaned), orphaned)
	}

	for _, expected := range expectedOrphaned {
		found := false
		for _, actual := range orphaned {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected %s to be orphaned, but it wasn't found in: %v", expected, orphaned)
		}
	}

	// shared/common.md should NOT be orphaned as it's used by workflow1 and workflow3
	for _, include := range orphaned {
		if include == "shared/common.md" {
			t.Error("shared/common.md should not be orphaned as it's used by remaining workflows")
		}
	}

	// Test Case 2: Remove all workflows - should orphan all includes
	allFiles := []string{
		".github/workflows/workflow1.md",
		".github/workflows/workflow2.md",
		".github/workflows/workflow3.md",
	}
	orphaned, err = previewOrphanedIncludes(allFiles, false)
	if err != nil {
		t.Fatalf("previewOrphanedIncludes failed: %v", err)
	}

	expectedAllOrphaned := []string{"shared/common.md", "shared/tools.md", "shared/unused.md"}
	if len(orphaned) != len(expectedAllOrphaned) {
		t.Errorf("Expected %d orphaned includes when removing all workflows, got %d: %v", len(expectedAllOrphaned), len(orphaned), orphaned)
	}
}

// TestRemoveWorkflowsWithNoOrphansFlag tests that the --keep-orphans flag works correctly
func TestRemoveWorkflowsWithNoOrphansFlag(t *testing.T) {
	// Create a temporary directory structure
	tmpDir, err := os.MkdirTemp("", "test-keep-orphans-flag")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create .github/workflows directory
	workflowsDir := filepath.Join(tmpDir, constants.GetWorkflowDir())
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create shared subdirectory
	sharedDir := filepath.Join(workflowsDir, "shared")
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a workflow that uses an include
	workflowContent := `---
on:
  workflow_dispatch:
---

# Test Workflow

@include shared/common.md

This workflow uses an include.
`
	if err := os.WriteFile(filepath.Join(workflowsDir, "test-workflow.md"), []byte(workflowContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create the include file
	includeContent := `This is a shared include file.`
	if err := os.WriteFile(filepath.Join(sharedDir, "common.md"), []byte(includeContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Change to the temporary directory
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Logf("Warning: Failed to restore working directory: %v", err)
		}
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// Test 1: Verify include file exists before removal
	if _, err := os.Stat(filepath.Join(sharedDir, "common.md")); os.IsNotExist(err) {
		t.Fatal("Include file should exist before removal")
	}

	// Test 2: Check preview shows orphaned includes when flag is not used
	filesToRemove := []string{".github/workflows/test-workflow.md"}
	orphaned, err := previewOrphanedIncludes(filesToRemove, false)
	if err != nil {
		t.Fatalf("previewOrphanedIncludes failed: %v", err)
	}

	if len(orphaned) != 1 || orphaned[0] != "shared/common.md" {
		t.Errorf("Expected shared/common.md to be orphaned, got: %v", orphaned)
	}

	// Note: We can't easily test the actual RemoveWorkflows function with user input
	// since it requires interactive confirmation. The logic is tested through
	// previewOrphanedIncludes and the flag handling is straightforward.
}

// TestCalculateTimeRemaining tests the calculateTimeRemaining function
func TestCalculateTimeRemaining(t *testing.T) {
	tests := []struct {
		name        string
		stopTimeStr string
		expected    string
	}{
		{
			name:        "empty stop time",
			stopTimeStr: "",
			expected:    "N/A",
		},
		{
			name:        "invalid format",
			stopTimeStr: "invalid-date-format",
			expected:    "Invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateTimeRemaining(tt.stopTimeStr)
			if result != tt.expected {
				t.Errorf("calculateTimeRemaining(%q) = %q, want %q", tt.stopTimeStr, result, tt.expected)
			}
		})
	}

	// Test with future time - this will test the logic but the exact result depends on current time
	t.Run("future time formatting", func(t *testing.T) {
		// Create a time 2 hours and 30 minutes in the future
		// Add a small buffer to account for execution time
		futureTime := time.Now().Add(2*time.Hour + 30*time.Minute + 1*time.Second)
		stopTimeStr := futureTime.Format("2006-01-02 15:04:05")

		result := calculateTimeRemaining(stopTimeStr)

		// Should contain "h" and "m" for hours and minutes
		if !strings.Contains(result, "h") || !strings.Contains(result, "m") {
			t.Errorf("calculateTimeRemaining() for future time should contain hours and minutes, got: %q", result)
		}

		// Should not be "Expired", "Invalid", or "N/A"
		if result == "Expired" || result == "Invalid" || result == "N/A" {
			t.Errorf("calculateTimeRemaining() for future time should not be %q", result)
		}
	})

	// Test with past time
	t.Run("past time - expired", func(t *testing.T) {
		// Create a time 1 hour in the past
		pastTime := time.Now().Add(-1 * time.Hour)
		stopTimeStr := pastTime.Format("2006-01-02 15:04:05")

		result := calculateTimeRemaining(stopTimeStr)
		if result != "Expired" {
			t.Errorf("calculateTimeRemaining() for past time = %q, want %q", result, "Expired")
		}
	})
}

func TestRunWorkflowOnGitHubWithEnable(t *testing.T) {
	// Test with enable flag enabled (should not error for basic validation)
	err := RunWorkflowOnGitHub(context.Background(), "nonexistent-workflow", true, "", "", "", false, false, false, false, []string{}, false, false)
	if err == nil {
		t.Error("RunWorkflowOnGitHub should return error for non-existent workflow even with enable flag")
	}

	// Test with empty workflow name and enable flag
	err = RunWorkflowOnGitHub(context.Background(), "", true, "", "", "", false, false, false, false, []string{}, false, false)
	if err == nil {
		t.Error("RunWorkflowOnGitHub should return error for empty workflow name regardless of enable flag")
	}
}

func TestGetWorkflowStatus(t *testing.T) {

	// Test with non-existent workflow
	_, err := getWorkflowStatus("nonexistent-workflow", "", false)
	if err == nil {
		t.Error("getWorkflowStatus should return error for non-existent workflow")
	}

	// Test with empty workflow name
	_, err = getWorkflowStatus("", "", false)
	if err == nil {
		t.Error("getWorkflowStatus should return error for empty workflow name")
	}
}
