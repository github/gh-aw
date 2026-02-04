//go:build !integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestEnsureCopilotInstructions(t *testing.T) {
	tests := []struct {
		name            string
		existingContent string
		expectExists    bool
	}{
		{
			name:            "reports missing file without error",
			existingContent: "",
			expectExists:    false,
		},
		{
			name:            "reports existing file",
			existingContent: "# Test content",
			expectExists:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for testing
			tempDir := testutil.TempDir(t, "test-*")

			// Change to temp directory and initialize git repo for findGitRoot to work
			oldWd, _ := os.Getwd()
			defer func() {
				_ = os.Chdir(oldWd)
			}()
			err := os.Chdir(tempDir)
			if err != nil {
				t.Fatalf("Failed to change directory: %v", err)
			}

			// Initialize git repo
			if err := exec.Command("git", "init").Run(); err != nil {
				t.Fatalf("Failed to init git repo: %v", err)
			}

			copilotDir := filepath.Join(tempDir, ".github", "aw")
			copilotInstructionsPath := filepath.Join(copilotDir, "github-agentic-workflows.md")

			// Create initial content if specified
			if tt.existingContent != "" {
				if err := os.MkdirAll(copilotDir, 0755); err != nil {
					t.Fatalf("Failed to create copilot directory: %v", err)
				}
				if err := os.WriteFile(copilotInstructionsPath, []byte(tt.existingContent), 0644); err != nil {
					t.Fatalf("Failed to create initial copilot instructions: %v", err)
				}
			}

			// Call the function with skipInstructions=false to test the functionality
			err = ensureCopilotInstructions(false, false)
			if err != nil {
				t.Fatalf("ensureCopilotInstructions() returned error: %v", err)
			}

			// Check that file exists or not based on test expectation
			_, statErr := os.Stat(copilotInstructionsPath)
			fileExists := statErr == nil

			if fileExists != tt.expectExists {
				if tt.expectExists {
					t.Errorf("Expected copilot instructions file to exist, but it doesn't")
				} else {
					t.Errorf("Expected copilot instructions file to not exist, but it does")
				}
			}
		})
	}
}

func TestEnsureCopilotInstructions_WithSkipInstructionsTrue(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := testutil.TempDir(t, "test-*")

	// Change to temp directory and initialize git repo for findGitRoot to work
	oldWd, _ := os.Getwd()
	defer func() {
		_ = os.Chdir(oldWd)
	}()
	err := os.Chdir(tempDir)
	if err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	// Initialize git repo
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}

	copilotDir := filepath.Join(tempDir, ".github", "aw")
	copilotInstructionsPath := filepath.Join(copilotDir, "github-agentic-workflows.md")

	// Call the function with skipInstructions=true
	err = ensureCopilotInstructions(false, true)
	if err != nil {
		t.Fatalf("ensureCopilotInstructions() returned error: %v", err)
	}

	// Check that file does not exist (no file created when skipInstructions=true)
	if _, err := os.Stat(copilotInstructionsPath); !os.IsNotExist(err) {
		t.Fatalf("Expected copilot instructions file to not exist when skipInstructions=true")
	}
}

func TestEnsureCopilotInstructions_CleansUpOldFile(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := testutil.TempDir(t, "test-*")

	// Change to temp directory and initialize git repo for findGitRoot to work
	oldWd, _ := os.Getwd()
	defer func() {
		_ = os.Chdir(oldWd)
	}()
	err := os.Chdir(tempDir)
	if err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	// Initialize git repo
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}

	// Create the old file location
	oldDir := filepath.Join(tempDir, ".github", "instructions")
	oldPath := filepath.Join(oldDir, "github-agentic-workflows.instructions.md")
	if err := os.MkdirAll(oldDir, 0755); err != nil {
		t.Fatalf("Failed to create old directory: %v", err)
	}
	if err := os.WriteFile(oldPath, []byte("old content"), 0644); err != nil {
		t.Fatalf("Failed to create old file: %v", err)
	}

	// Verify old file exists
	if _, err := os.Stat(oldPath); os.IsNotExist(err) {
		t.Fatalf("Old file should exist before running ensureCopilotInstructions")
	}

	// Call the function
	err = ensureCopilotInstructions(false, false)
	if err != nil {
		t.Fatalf("ensureCopilotInstructions() returned error: %v", err)
	}

	// Verify old file was removed
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("Old file should be removed after ensureCopilotInstructions")
	}

	// New file should not be created by ensureCopilotInstructions anymore
	// (files in .github/aw are source of truth, not created by init)
	newPath := filepath.Join(tempDir, ".github", "aw", "github-agentic-workflows.md")
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Errorf("New file should not be created by ensureCopilotInstructions (files are source of truth)")
	}
}
