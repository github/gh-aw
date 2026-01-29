//go:build !integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/githubnext/gh-aw/pkg/testutil"
)

func TestEnsureCopilotInstructions(t *testing.T) {
	tests := []struct {
		name            string
		existingContent string
		expectedContent string
	}{
		{
			name:            "creates new copilot instructions file",
			existingContent: "",
			expectedContent: strings.TrimSpace(copilotInstructionsTemplate),
		},
		{
			name:            "does not modify existing correct file",
			existingContent: copilotInstructionsTemplate,
			expectedContent: strings.TrimSpace(copilotInstructionsTemplate),
		},
		{
			name:            "updates modified file",
			existingContent: "# Modified GitHub Agentic Workflows - Copilot Instructions\n\nThis is a modified version.",
			expectedContent: strings.TrimSpace(copilotInstructionsTemplate),
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

			// Check that file exists
			if _, err := os.Stat(copilotInstructionsPath); os.IsNotExist(err) {
				t.Fatalf("Expected copilot instructions file to exist")
			}

			// Check content
			content, err := os.ReadFile(copilotInstructionsPath)
			if err != nil {
				t.Fatalf("Failed to read copilot instructions: %v", err)
			}

			contentStr := strings.TrimSpace(string(content))
			expectedStr := strings.TrimSpace(tt.expectedContent)

			if contentStr != expectedStr {
				t.Errorf("Expected content does not match.\nExpected first 100 chars: %q\nActual first 100 chars: %q",
					expectedStr[:min(100, len(expectedStr))],
					contentStr[:min(100, len(contentStr))])
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

	// Check that file does not exist
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

	// Verify new file was created
	newPath := filepath.Join(tempDir, ".github", "aw", "github-agentic-workflows.md")
	if _, err := os.Stat(newPath); os.IsNotExist(err) {
		t.Errorf("New file should exist after ensureCopilotInstructions")
	}

	// Verify new file has correct content
	content, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("Failed to read new file: %v", err)
	}

	contentStr := strings.TrimSpace(string(content))
	expectedStr := strings.TrimSpace(copilotInstructionsTemplate)

	if contentStr != expectedStr {
		t.Errorf("New file content does not match template")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
