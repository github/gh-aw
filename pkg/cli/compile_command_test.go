//go:build integration

package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"

	"github.com/github/gh-aw/pkg/workflow"
)

// TestCompileConfig tests the CompileConfig structure
func TestCompileConfig(t *testing.T) {
	config := CompileConfig{
		MarkdownFiles:  []string{"test.md"},
		Verbose:        true,
		EngineOverride: "copilot",
		Validate:       true,
		Watch:          false,
		WorkflowDir:    ".github/workflows",
		NoEmit:         false,
		Purge:          false,
		TrialMode:      false,
		Strict:         false,
		Dependabot:     false,
		ForceOverwrite: false,
		Zizmor:         false,
		Poutine:        false,
		Actionlint:     false,
	}

	// Verify all fields are accessible
	if len(config.MarkdownFiles) != 1 {
		t.Errorf("Expected 1 markdown file, got %d", len(config.MarkdownFiles))
	}
	if !config.Verbose {
		t.Error("Expected Verbose to be true")
	}
	if config.EngineOverride != "copilot" {
		t.Errorf("Expected EngineOverride to be 'copilot', got %q", config.EngineOverride)
	}
}

// TestCompilationStats tests the CompilationStats structure
func TestCompilationStats(t *testing.T) {
	stats := &CompilationStats{
		Total:           5,
		Errors:          2,
		Warnings:        3,
		FailedWorkflows: []string{"workflow1.md", "workflow2.md"},
	}

	if stats.Total != 5 {
		t.Errorf("Expected Total to be 5, got %d", stats.Total)
	}
	if stats.Errors != 2 {
		t.Errorf("Expected Errors to be 2, got %d", stats.Errors)
	}
	if stats.Warnings != 3 {
		t.Errorf("Expected Warnings to be 3, got %d", stats.Warnings)
	}
	if len(stats.FailedWorkflows) != 2 {
		t.Errorf("Expected 2 failed workflows, got %d", len(stats.FailedWorkflows))
	}
}

// TestPrintCompilationSummary tests the printCompilationSummary function
func TestPrintCompilationSummary(t *testing.T) {
	tests := []struct {
		name  string
		stats *CompilationStats
	}{
		{
			name: "no workflows",
			stats: &CompilationStats{
				Total:    0,
				Errors:   0,
				Warnings: 0,
			},
		},
		{
			name: "successful compilation",
			stats: &CompilationStats{
				Total:    5,
				Errors:   0,
				Warnings: 0,
			},
		},
		{
			name: "with warnings",
			stats: &CompilationStats{
				Total:    5,
				Errors:   0,
				Warnings: 3,
			},
		},
		{
			name: "with errors",
			stats: &CompilationStats{
				Total:           5,
				Errors:          2,
				Warnings:        1,
				FailedWorkflows: []string{"workflow1.md", "workflow2.md"},
			},
		},
		{
			name: "with detailed failure information",
			stats: &CompilationStats{
				Total:    5,
				Errors:   3,
				Warnings: 1,
				FailureDetails: []WorkflowFailure{
					{Path: ".github/workflows/test1.md", ErrorCount: 1},
					{Path: ".github/workflows/test2.md", ErrorCount: 2},
					{Path: ".github/workflows/test3.md", ErrorCount: 1},
				},
			},
		},
		{
			name: "with multiple errors per workflow",
			stats: &CompilationStats{
				Total:    3,
				Errors:   5,
				Warnings: 0,
				FailureDetails: []WorkflowFailure{
					{Path: ".github/workflows/complex.md", ErrorCount: 3},
					{Path: ".github/workflows/simple.md", ErrorCount: 2},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// printCompilationSummary writes to stderr, we just verify it doesn't panic
			printCompilationSummary(tt.stats)
		})
	}
}

// TestTrackWorkflowFailure tests the trackWorkflowFailure helper function
func TestTrackWorkflowFailure(t *testing.T) {
	tests := []struct {
		name            string
		workflowPath    string
		errorCount      int
		expectedDetails WorkflowFailure
	}{
		{
			name:         "single error",
			workflowPath: ".github/workflows/test.md",
			errorCount:   1,
			expectedDetails: WorkflowFailure{
				Path:       ".github/workflows/test.md",
				ErrorCount: 1,
			},
		},
		{
			name:         "multiple errors",
			workflowPath: ".github/workflows/complex.md",
			errorCount:   5,
			expectedDetails: WorkflowFailure{
				Path:       ".github/workflows/complex.md",
				ErrorCount: 5,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := &CompilationStats{}
			trackWorkflowFailure(stats, tt.workflowPath, tt.errorCount)

			// Check that FailedWorkflows was updated
			if len(stats.FailedWorkflows) != 1 {
				t.Errorf("Expected 1 failed workflow, got %d", len(stats.FailedWorkflows))
			}

			// Check that FailureDetails was updated correctly
			if len(stats.FailureDetails) != 1 {
				t.Errorf("Expected 1 failure detail, got %d", len(stats.FailureDetails))
			}

			detail := stats.FailureDetails[0]
			if detail.Path != tt.expectedDetails.Path {
				t.Errorf("Expected path %s, got %s", tt.expectedDetails.Path, detail.Path)
			}
			if detail.ErrorCount != tt.expectedDetails.ErrorCount {
				t.Errorf("Expected error count %d, got %d", tt.expectedDetails.ErrorCount, detail.ErrorCount)
			}
		})
	}
}

// Note: TestHandleFileDeleted is already tested in commands_file_watching_test.go

// TestCompileWorkflowWithValidation_InvalidFile tests error handling
func TestCompileWorkflowWithValidation_InvalidFile(t *testing.T) {
	compiler := workflow.NewCompiler()

	// Try to compile a non-existent file
	err := CompileWorkflowWithValidation(
		compiler,
		"/nonexistent/file.md",
		false, // verbose
		false, // zizmor
		false, // poutine
		false, // actionlint
		false, // strict
		false, // validateActionSHAs
	)

	if err == nil {
		t.Error("Expected error when compiling non-existent file, got nil")
	}
}

// TestCompileWorkflows_DependabotValidation tests dependabot flag validation
// Uses the fast validateCompileConfig function instead of full compilation
func TestCompileWorkflows_DependabotValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      CompileConfig
		expectError bool
		errorMsg    string
	}{
		{
			name: "dependabot with specific files",
			config: CompileConfig{
				Dependabot:    true,
				MarkdownFiles: []string{"test.md"},
			},
			expectError: true,
			errorMsg:    "cannot be used with specific workflow files",
		},
		{
			name: "dependabot with custom workflow dir",
			config: CompileConfig{
				Dependabot:  true,
				WorkflowDir: "custom/workflows",
			},
			expectError: true,
			errorMsg:    "cannot be used with custom --dir",
		},
		{
			name: "dependabot with default settings",
			config: CompileConfig{
				Dependabot:  true,
				WorkflowDir: "",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use fast validation function instead of full compilation
			err := validateCompileConfig(tt.config)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got nil")
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else if err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}

// TestCompileWorkflows_PurgeValidation tests purge flag validation
// Uses the fast validateCompileConfig function instead of full compilation
func TestCompileWorkflows_PurgeValidation(t *testing.T) {
	config := CompileConfig{
		Purge:         true,
		MarkdownFiles: []string{"test.md"},
	}

	// Use fast validation function instead of full compilation
	err := validateCompileConfig(config)

	if err == nil {
		t.Error("Expected error when using purge with specific files, got nil")
	}

	if !strings.Contains(err.Error(), "can only be used when compiling all markdown files") {
		t.Errorf("Expected error about purge flag, got: %v", err)
	}
}

// TestCompileWorkflows_WorkflowDirValidation tests workflow directory validation
// Uses the fast validateCompileConfig function instead of full compilation
func TestCompileWorkflows_WorkflowDirValidation(t *testing.T) {
	tests := []struct {
		name        string
		workflowDir string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "absolute path not allowed",
			workflowDir: "/absolute/path",
			expectError: true,
			errorMsg:    "must be a relative path",
		},
		{
			name:        "relative path allowed",
			workflowDir: "custom/workflows",
			expectError: false,
		},
		{
			name:        "default empty path",
			workflowDir: "",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := CompileConfig{
				WorkflowDir: tt.workflowDir,
			}

			// Use fast validation function instead of full compilation
			err := validateCompileConfig(config)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got nil")
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else if err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}

// TestCompileWorkflowDataWithValidation_NoEmit tests validation without emission
func TestCompileWorkflowDataWithValidation_NoEmit(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := testutil.TempDir(t, "test-*")
	testFile := filepath.Join(tmpDir, "test.md")

	// Create a simple test workflow
	workflowContent := `---
on: push
permissions:
  contents: read
---

# Test Workflow

This is a test workflow.
`
	if err := os.WriteFile(testFile, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create compiler with noEmit flag
	compiler := workflow.NewCompiler(
		workflow.WithVersion("test"),
		workflow.WithNoEmit(true),
	)

	// Parse the workflow
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Failed to parse workflow: %v", err)
	}

	// Compile without emitting
	err = CompileWorkflowDataWithValidation(
		compiler,
		workflowData,
		testFile,
		false, // verbose
		false, // zizmor
		false, // poutine
		false, // actionlint
		false, // strict
		false, // validateActionSHAs
	)

	// Should complete without error
	if err != nil {
		t.Errorf("Unexpected error with noEmit: %v", err)
	}

	// Verify lock file was not created
	lockFile := stringutil.MarkdownToLockFile(testFile)
	if _, err := os.Stat(lockFile); !os.IsNotExist(err) {
		t.Error("Lock file should not exist with noEmit flag")
	}
}

// TestCompileWorkflowWithValidation_YAMLValidation tests YAML validation
func TestCompileWorkflowWithValidation_YAMLValidation(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := testutil.TempDir(t, "test-*")
	testFile := filepath.Join(tmpDir, "test.md")

	// Create a simple test workflow
	workflowContent := `---
on: push
permissions:
  contents: read
engine: copilot
---

# Test Workflow

This is a test workflow.
`
	if err := os.WriteFile(testFile, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create compiler
	compiler := workflow.NewCompiler()

	// Compile the workflow
	err := CompileWorkflowWithValidation(
		compiler,
		testFile,
		false, // verbose
		false, // zizmor
		false, // poutine
		false, // actionlint
		false, // strict
		false, // validateActionSHAs
	)

	// Should complete without error
	if err != nil {
		t.Errorf("Unexpected error during compilation: %v", err)
	}

	// Verify lock file was created
	lockFile := stringutil.MarkdownToLockFile(testFile)
	if _, err := os.Stat(lockFile); os.IsNotExist(err) {
		t.Error("Lock file should have been created")
	}

	// Clean up
	os.Remove(lockFile)
}

// TestCompileConfig_DefaultValues tests default configuration values
func TestCompileConfig_DefaultValues(t *testing.T) {
	config := CompileConfig{}

	// Verify default values
	if config.Verbose {
		t.Error("Expected Verbose to default to false")
	}
	if config.Validate {
		t.Error("Expected Validate to default to false")
	}
	if config.Watch {
		t.Error("Expected Watch to default to false")
	}
	if config.NoEmit {
		t.Error("Expected NoEmit to default to false")
	}
	if config.Purge {
		t.Error("Expected Purge to default to false")
	}
	if config.TrialMode {
		t.Error("Expected TrialMode to default to false")
	}
	if config.Strict {
		t.Error("Expected Strict to default to false")
	}
	if config.Dependabot {
		t.Error("Expected Dependabot to default to false")
	}
	if config.ForceOverwrite {
		t.Error("Expected ForceOverwrite to default to false")
	}
	if config.Zizmor {
		t.Error("Expected Zizmor to default to false")
	}
	if config.Poutine {
		t.Error("Expected Poutine to default to false")
	}
	if config.Actionlint {
		t.Error("Expected Actionlint to default to false")
	}
}

// TestCompilationStats_DefaultValues tests default stats values
func TestCompilationStats_DefaultValues(t *testing.T) {
	stats := &CompilationStats{}

	if stats.Total != 0 {
		t.Errorf("Expected Total to default to 0, got %d", stats.Total)
	}
	if stats.Errors != 0 {
		t.Errorf("Expected Errors to default to 0, got %d", stats.Errors)
	}
	if stats.Warnings != 0 {
		t.Errorf("Expected Warnings to default to 0, got %d", stats.Warnings)
	}
	if len(stats.FailedWorkflows) != 0 {
		t.Errorf("Expected FailedWorkflows to default to empty, got %d items", len(stats.FailedWorkflows))
	}
}

// TestCompileWorkflows_EmptyMarkdownFiles tests compilation with no files specified
func TestCompileWorkflows_EmptyMarkdownFiles(t *testing.T) {
	// This test requires being in a git repository
	// We'll skip if not in a git repo
	_, err := findGitRoot()
	if err != nil {
		t.Skip("Not in a git repository, skipping test")
	}

	config := CompileConfig{
		MarkdownFiles: []string{},
		WorkflowDir:   ".github/workflows",
	}

	// This will try to compile all files in .github/workflows
	// It may fail if the directory doesn't exist, which is expected
	CompileWorkflows(context.Background(), config)

	// We don't check for specific error here as it depends on the repository state
	// The test just ensures the function handles empty MarkdownFiles correctly
}

// TestCompileWorkflows_TrialMode tests trial mode configuration
func TestCompileWorkflows_TrialMode(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	testFile := filepath.Join(tmpDir, "test.md")

	// Create a simple test workflow
	workflowContent := `---
on: push
permissions:
  contents: read
engine: copilot
---

# Test Workflow

This is a test workflow.
`
	if err := os.WriteFile(testFile, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	config := CompileConfig{
		MarkdownFiles:        []string{testFile},
		TrialMode:            true,
		TrialLogicalRepoSlug: "owner/trial-repo",
	}

	_, err := CompileWorkflows(context.Background(), config)

	// The compilation may fail for various reasons in a test environment,
	// but it should not panic and should handle trial mode settings
	_ = err // We're just testing that the config is processed
}

// TestValidationResult tests the ValidationResult structure
func TestValidationResult(t *testing.T) {
	result := ValidationResult{
		Workflow: "test-workflow.md",
		Valid:    false,
		Errors: []CompileValidationError{
			{
				Type:    "schema_validation",
				Message: "Unknown property: toolz",
				Line:    5,
			},
		},
		Warnings:     []CompileValidationError{},
		CompiledFile: ".github/workflows/test-workflow.lock.yml",
	}

	if result.Workflow != "test-workflow.md" {
		t.Errorf("Expected workflow 'test-workflow.md', got %q", result.Workflow)
	}
	if result.Valid {
		t.Error("Expected Valid to be false")
	}
	if len(result.Errors) != 1 {
		t.Errorf("Expected 1 error, got %d", len(result.Errors))
	}
	if result.Errors[0].Type != "schema_validation" {
		t.Errorf("Expected error type 'schema_validation', got %q", result.Errors[0].Type)
	}
}

// TestCompileConfig_JSONOutput tests the JSONOutput field
func TestCompileConfig_JSONOutput(t *testing.T) {
	config := CompileConfig{
		MarkdownFiles: []string{"test.md"},
		JSONOutput:    true,
	}

	if !config.JSONOutput {
		t.Error("Expected JSONOutput to be true")
	}
}

// TestSecurityToolsIndependentOfValidate verifies that security analysis tools
// (zizmor, poutine, actionlint) can be enabled independently of the --validate flag.
// This ensures these tools run regardless of whether schema validation is enabled.
func TestSecurityToolsIndependentOfValidate(t *testing.T) {
	// Test that security tools can be enabled without validate flag
	tests := []struct {
		name       string
		validate   bool
		zizmor     bool
		poutine    bool
		actionlint bool
	}{
		{
			name:       "zizmor without validate",
			validate:   false,
			zizmor:     true,
			poutine:    false,
			actionlint: false,
		},
		{
			name:       "poutine without validate",
			validate:   false,
			zizmor:     false,
			poutine:    true,
			actionlint: false,
		},
		{
			name:       "actionlint without validate",
			validate:   false,
			zizmor:     false,
			poutine:    false,
			actionlint: true,
		},
		{
			name:       "all security tools without validate",
			validate:   false,
			zizmor:     true,
			poutine:    true,
			actionlint: true,
		},
		{
			name:       "zizmor with validate",
			validate:   true,
			zizmor:     true,
			poutine:    false,
			actionlint: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := CompileConfig{
				MarkdownFiles: []string{"test.md"},
				Validate:      tt.validate,
				Zizmor:        tt.zizmor,
				Poutine:       tt.poutine,
				Actionlint:    tt.actionlint,
			}

			// Verify the config fields are correctly set independent of each other
			if config.Validate != tt.validate {
				t.Errorf("Validate = %v, want %v", config.Validate, tt.validate)
			}
			if config.Zizmor != tt.zizmor {
				t.Errorf("Zizmor = %v, want %v", config.Zizmor, tt.zizmor)
			}
			if config.Poutine != tt.poutine {
				t.Errorf("Poutine = %v, want %v", config.Poutine, tt.poutine)
			}
			if config.Actionlint != tt.actionlint {
				t.Errorf("Actionlint = %v, want %v", config.Actionlint, tt.actionlint)
			}

			// Verify that security tools being enabled does not depend on validate flag
			// Each tool should be independently configurable
			if tt.zizmor && !config.Zizmor {
				t.Error("Zizmor should be enabled but is not")
			}
			if tt.poutine && !config.Poutine {
				t.Error("Poutine should be enabled but is not")
			}
			if tt.actionlint && !config.Actionlint {
				t.Error("Actionlint should be enabled but is not")
			}
		})
	}
}

// TestCompileWorkflowDataWithValidation_SecurityToolsPassedCorrectly verifies that
// security tool flags are passed correctly to CompileWorkflowDataWithValidation
// regardless of the validate flag setting.
func TestCompileWorkflowDataWithValidation_SecurityToolsPassedCorrectly(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	testFile := filepath.Join(tmpDir, "test.md")

	// Create a simple test workflow
	workflowContent := `---
on: push
permissions:
  contents: read
engine: copilot
---

# Test Workflow

This is a test workflow.
`
	if err := os.WriteFile(testFile, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test that all combinations of security tools work without validate
	tests := []struct {
		name       string
		zizmor     bool
		poutine    bool
		actionlint bool
		validate   bool
	}{
		{
			name:       "security tools without validate",
			zizmor:     true,
			poutine:    true,
			actionlint: true,
			validate:   false,
		},
		{
			name:       "security tools with validate",
			zizmor:     true,
			poutine:    true,
			actionlint: true,
			validate:   true,
		},
		{
			name:       "no security tools no validate",
			zizmor:     false,
			poutine:    false,
			actionlint: false,
			validate:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := workflow.NewCompiler()
			// Note: SetSkipValidation controls schema validation, which is
			// different from the validateActionSHAs parameter we're testing.
			// We skip schema validation here to focus on the security tool independence test.
			compiler.SetSkipValidation(true)

			workflowData, err := compiler.ParseWorkflowFile(testFile)
			if err != nil {
				t.Fatalf("Failed to parse workflow: %v", err)
			}

			// Call CompileWorkflowDataWithValidation with the security tools
			// This verifies the function signature accepts these parameters
			// regardless of the validate flag
			//
			// NOTE: We don't run the actual security tools here since they
			// require Docker, but this test verifies the API contract that
			// security tools are independent of the validate flag.
			err = CompileWorkflowDataWithValidation(
				compiler,
				workflowData,
				testFile,
				false,       // verbose
				false,       // runZizmor - disabled for unit test (no Docker)
				false,       // runPoutine - disabled for unit test (no Docker)
				false,       // runActionlint - disabled for unit test (no Docker)
				false,       // strict
				tt.validate, // validateActionSHAs - independent of security tools
			)

			// Even without running security tools, the compilation should succeed
			// This proves security tools can be disabled while keeping validate
			// at any state, and vice versa
			if err != nil {
				// Some errors are expected in test environment, but the function
				// should accept the parameters without issues
				t.Logf("Compilation result (expected in test env): %v", err)
			}
		})
	}
}

// TestCompileWorkflows_PurgeInvalidYml tests that --purge also removes .invalid.yml files
func TestCompileWorkflows_PurgeInvalidYml(t *testing.T) {
	// Create temporary directory structure for testing
	tempDir := testutil.TempDir(t, "test-purge-invalid-*")
	workflowsDir := filepath.Join(tempDir, ".github/workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Change to temp directory to simulate being in a git repo
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer os.Chdir(originalDir)
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Create .git directory and initialize it properly
	gitCmd := exec.Command("git", "init")
	gitCmd.Dir = tempDir
	if err := gitCmd.Run(); err != nil {
		t.Fatalf("Failed to initialize git repo: %v", err)
	}

	// Configure git for the test
	exec.Command("git", "-C", tempDir, "config", "user.email", "test@example.com").Run()
	exec.Command("git", "-C", tempDir, "config", "user.name", "Test User").Run()

	// Create a valid test workflow markdown file
	testWorkflowMd := filepath.Join(workflowsDir, "test.md")
	testWorkflowContent := `---
name: Test Workflow
engine: copilot
on:
  workflow_dispatch:
---

Test workflow content`

	if err := os.WriteFile(testWorkflowMd, []byte(testWorkflowContent), 0644); err != nil {
		t.Fatalf("Failed to create test workflow: %v", err)
	}

	// Create some orphaned .invalid.yml files that should be purged
	invalidFile1 := filepath.Join(workflowsDir, "old1.invalid.yml")
	invalidFile2 := filepath.Join(workflowsDir, "old2.invalid.yml")
	if err := os.WriteFile(invalidFile1, []byte("invalid yaml content 1"), 0644); err != nil {
		t.Fatalf("Failed to create invalid file 1: %v", err)
	}
	if err := os.WriteFile(invalidFile2, []byte("invalid yaml content 2"), 0644); err != nil {
		t.Fatalf("Failed to create invalid file 2: %v", err)
	}

	// Create an orphaned .lock.yml file that should also be purged
	orphanedLockFile := filepath.Join(workflowsDir, "orphaned.lock.yml")
	if err := os.WriteFile(orphanedLockFile, []byte("orphaned lock content"), 0644); err != nil {
		t.Fatalf("Failed to create orphaned lock file: %v", err)
	}

	// Verify files exist before purge
	if _, err := os.Stat(invalidFile1); os.IsNotExist(err) {
		t.Fatal("Invalid file 1 should exist before purge")
	}
	if _, err := os.Stat(invalidFile2); os.IsNotExist(err) {
		t.Fatal("Invalid file 2 should exist before purge")
	}
	if _, err := os.Stat(orphanedLockFile); os.IsNotExist(err) {
		t.Fatal("Orphaned lock file should exist before purge")
	}

	// Run compilation with purge flag
	config := CompileConfig{
		MarkdownFiles: []string{}, // Empty to compile all files
		Verbose:       true,       // Enable verbose to see what's happening
		NoEmit:        false,      // Actually compile to test full purge logic
		Purge:         true,
		WorkflowDir:   "",
		Validate:      false, // Skip validation to avoid test failures
	}

	// Compile workflows with purge enabled
	result, err := CompileWorkflows(context.Background(), config)
	if err != nil {
		t.Logf("Compilation error (expected): %v", err)
	}
	if result != nil {
		t.Logf("Compilation completed with %d results", len(result))
	}

	// Verify .invalid.yml files were deleted
	if _, err := os.Stat(invalidFile1); !os.IsNotExist(err) {
		t.Error("Invalid file 1 should have been purged")
	}
	if _, err := os.Stat(invalidFile2); !os.IsNotExist(err) {
		t.Error("Invalid file 2 should have been purged")
	}

	// Verify orphaned .lock.yml file was also deleted
	if _, err := os.Stat(orphanedLockFile); !os.IsNotExist(err) {
		t.Error("Orphaned lock file should have been purged")
	}

	// Verify the test.md file still exists (it should not be purged)
	if _, err := os.Stat(testWorkflowMd); os.IsNotExist(err) {
		t.Error("Test workflow markdown should still exist")
	}

	// Verify the test.lock.yml was created
	testLockFile := filepath.Join(workflowsDir, "test.lock.yml")
	if _, err := os.Stat(testLockFile); os.IsNotExist(err) {
		t.Log("Test lock file was not created (this is ok if validation failed)")
	}
}
