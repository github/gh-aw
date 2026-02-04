//go:build !integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestEnsureUpgradeAgenticWorkflowsPrompt(t *testing.T) {
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

			awDir := filepath.Join(tempDir, ".github", "aw")
			promptPath := filepath.Join(awDir, "upgrade-agentic-workflows.md")

			// Create initial content if specified
			if tt.existingContent != "" {
				if err := os.MkdirAll(awDir, 0755); err != nil {
					t.Fatalf("Failed to create aw directory: %v", err)
				}
				if err := os.WriteFile(promptPath, []byte(tt.existingContent), 0644); err != nil {
					t.Fatalf("Failed to create initial prompt: %v", err)
				}
			}

			// Call the function with skipInstructions=false to test the functionality
			err = ensureUpgradeAgenticWorkflowsPrompt(false, false)
			if err != nil {
				t.Fatalf("ensureUpgradeAgenticWorkflowsPrompt() returned error: %v", err)
			}

			// Check that file exists or not based on test expectation
			_, statErr := os.Stat(promptPath)
			fileExists := statErr == nil

			if fileExists != tt.expectExists {
				if tt.expectExists {
					t.Errorf("Expected prompt file to exist, but it doesn't")
				} else {
					t.Errorf("Expected prompt file to not exist, but it does")
				}
			}
		})
	}
}

func TestEnsureUpgradeAgenticWorkflowAgent_WithSkipInstructionsTrue(t *testing.T) {
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

	// Call the function with skipInstructions=true
	err = ensureUpgradeAgenticWorkflowsPrompt(false, true)
	if err != nil {
		t.Fatalf("ensureUpgradeAgenticWorkflowsPrompt() returned error: %v", err)
	}

	// Check that file was NOT created
	awDir := filepath.Join(tempDir, ".github", "aw")
	upgradePromptPath := filepath.Join(awDir, "upgrade-agentic-workflows.md")
	if _, err := os.Stat(upgradePromptPath); !os.IsNotExist(err) {
		t.Fatalf("Expected upgrade prompt file to NOT exist when skipInstructions=true")
	}
}
