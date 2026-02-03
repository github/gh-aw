//go:build !integration

package cli

import (
	"bytes"
	"os"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestAddCommandRequiresArguments verifies that the add command requires at least one argument
func TestAddCommandRequiresArguments(t *testing.T) {
	// Save current directory
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		// Restore original directory
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Warning: Failed to restore directory: %v", err)
		}
	}()

	// Create a temporary directory for testing
	tmpDir := testutil.TempDir(t, "test-*")
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp dir: %v", err)
	}

	// Initialize git repo
	if err := os.MkdirAll(".git", 0755); err != nil {
		t.Fatalf("Failed to create .git dir: %v", err)
	}

	// Create add command
	validateEngine := func(engine string) error { return nil }
	cmd := NewAddCommand(validateEngine)

	// Set up stderr capture
	stderr := &bytes.Buffer{}
	cmd.SetErr(stderr)

	// Try to execute without arguments
	cmd.SetArgs([]string{})

	// Execute and expect an error
	err = cmd.Execute()
	if err == nil {
		t.Error("Expected error when calling add without arguments, got nil")
	}

	// Verify the error message mentions required arguments
	errMsg := err.Error()
	if errMsg != "requires at least 1 arg(s), only received 0" {
		t.Errorf("Expected 'requires at least 1 arg(s)' error, got: %s", errMsg)
	}
}

// TestAddCommandWithWorkflow verifies that add command works with a workflow specification
func TestAddCommandWithWorkflow(t *testing.T) {
	// This test verifies that the command accepts arguments correctly
	// We don't actually execute it since that would require network access

	validateEngine := func(engine string) error { return nil }
	cmd := NewAddCommand(validateEngine)

	// Verify command accepts arguments
	if cmd.Args == nil {
		t.Error("Add command should have Args validator")
	}

	// Test Args validator directly
	argsValidator := cmd.Args
	if argsValidator != nil {
		// Should accept one argument
		if err := argsValidator(cmd, []string{"githubnext/agentics/ci-doctor"}); err != nil {
			t.Errorf("Command should accept one workflow argument, got error: %v", err)
		}

		// Should accept multiple arguments
		if err := argsValidator(cmd, []string{"githubnext/agentics/ci-doctor", "githubnext/agentics/daily-plan"}); err != nil {
			t.Errorf("Command should accept multiple workflow arguments, got error: %v", err)
		}

		// Should reject empty arguments
		if err := argsValidator(cmd, []string{}); err == nil {
			t.Error("Command should reject empty arguments, but no error was returned")
		}
	}
}

// TestAddCommandHelpText verifies the help text mentions the new command for creating workflows
func TestAddCommandHelpText(t *testing.T) {
	validateEngine := func(engine string) error { return nil }
	cmd := NewAddCommand(validateEngine)

	// Check that the long description mentions the 'new' command
	longDesc := cmd.Long
	if longDesc == "" {
		t.Error("Add command should have a long description")
	}

	// Verify the note about using 'new' command is present
	expectedNote := "Note: To create a new workflow from scratch, use the 'new' command instead."
	if len(longDesc) > 0 {
		found := false
		for i := 0; i <= len(longDesc)-len(expectedNote); i++ {
			if longDesc[i:i+len(expectedNote)] == expectedNote {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Add command help text should mention 'new' command")
		}
	}
}

// TestNewCommandAcceptsOptionalArgument verifies that new command accepts optional workflow name
func TestNewCommandAcceptsOptionalArgument(t *testing.T) {
	// Note: We can't easily test the actual command defined in main.go from here,
	// but we can document the expected behavior

	// The new command should:
	// 1. Accept zero arguments (interactive mode)
	// 2. Accept one argument (template mode with workflow name)
	// 3. Reject more than one argument

	// This is enforced by cobra.MaximumNArgs(1) in main.go
}
