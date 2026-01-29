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

func TestEnsureCreateWorkflowPrompt(t *testing.T) {
	tests := []struct {
		name            string
		existingContent string
		expectedContent string
	}{
		{
			name:            "creates new workflow creation prompt file",
			existingContent: "",
			expectedContent: strings.TrimSpace(createWorkflowPromptTemplate),
		},
		{
			name:            "does not modify existing correct file",
			existingContent: createWorkflowPromptTemplate,
			expectedContent: strings.TrimSpace(createWorkflowPromptTemplate),
		},
		{
			name:            "updates modified file",
			existingContent: "# Modified Workflow Creation Prompt\n\nThis is a modified version.",
			expectedContent: strings.TrimSpace(createWorkflowPromptTemplate),
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
			promptPath := filepath.Join(awDir, "create-agentic-workflow.md")

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
			err = ensureCreateWorkflowPrompt(false, false)
			if err != nil {
				t.Fatalf("ensureCreateWorkflowPrompt() returned error: %v", err)
			}

			// Check that file exists
			if _, err := os.Stat(promptPath); os.IsNotExist(err) {
				t.Fatalf("Expected prompt file to exist")
			}

			// Check content
			content, err := os.ReadFile(promptPath)
			if err != nil {
				t.Fatalf("Failed to read prompt: %v", err)
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

func TestEnsureCreateWorkflowPrompt_WithSkipInstructionsTrue(t *testing.T) {
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
	err = ensureCreateWorkflowPrompt(false, true)
	if err != nil {
		t.Fatalf("ensureCreateWorkflowPrompt() returned error: %v", err)
	}

	// Check that file was NOT created
	awDir := filepath.Join(tempDir, ".github", "aw")
	promptPath := filepath.Join(awDir, "create-agentic-workflow.md")
	if _, err := os.Stat(promptPath); !os.IsNotExist(err) {
		t.Fatalf("Expected prompt file to NOT exist when skipInstructions=true")
	}
}

func TestEnsureCreateWorkflowPrompt_CreatesNewFile(t *testing.T) {
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

	// Call the function to create prompt
	err = ensureCreateWorkflowPrompt(false, false)
	if err != nil {
		t.Fatalf("ensureCreateWorkflowPrompt() returned error: %v", err)
	}

	// Check that new prompt file was created in .github/aw/
	awDir := filepath.Join(tempDir, ".github", "aw")
	newPromptPath := filepath.Join(awDir, "create-agentic-workflow.md")
	if _, err := os.Stat(newPromptPath); os.IsNotExist(err) {
		t.Fatalf("Expected new prompt file to be created in .github/aw/")
	}
}
