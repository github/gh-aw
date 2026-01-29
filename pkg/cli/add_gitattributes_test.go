//go:build !integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestAddCommandUpdatesGitAttributes tests that the add command updates .gitattributes by default
func TestAddCommandUpdatesGitAttributes(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "gh-aw-add-gitattributes-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Change to temp directory
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Initialize a git repository
	if err := os.WriteFile("test.txt", []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	if cmd := exec.Command("git", "init"); cmd.Run() != nil {
		t.Skip("Skipping test - git not available")
	}
	if cmd := exec.Command("git", "config", "user.email", "test@example.com"); cmd.Run() != nil {
		t.Skip("Skipping test - git config failed")
	}
	if cmd := exec.Command("git", "config", "user.name", "Test User"); cmd.Run() != nil {
		t.Skip("Skipping test - git config failed")
	}

	// Create .github/workflows directory
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create .github/workflows directory: %v", err)
	}

	// Create a minimal workflow file to simulate a workflow being added
	workflowContent := `---
on: push
permissions:
  contents: read
engine: copilot
---

# Test Workflow

This is a test workflow.`

	workflowPath := filepath.Join(workflowsDir, "test.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to create workflow file: %v", err)
	}

	// Create a minimal WorkflowSpec for testing
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "test/repo",
			Version:  "",
		},
		WorkflowPath: "test.md",
		WorkflowName: "test",
	}

	t.Run("gitattributes_updated_by_default", func(t *testing.T) {
		// Remove any existing .gitattributes
		os.Remove(".gitattributes")

		// Call addWorkflowsNormal with noGitattributes=false
		err := addWorkflowsNormal([]*WorkflowSpec{spec}, 1, false, false, "", "", false, "", false, false, false, "", false, "")
		if err != nil {
			// We expect this to fail because we don't have a full workflow setup,
			// but gitattributes should still be updated before the error
			t.Logf("Expected error during workflow addition: %v", err)
		}

		// Check that .gitattributes was created
		gitAttributesPath := filepath.Join(tmpDir, ".gitattributes")
		if _, err := os.Stat(gitAttributesPath); os.IsNotExist(err) {
			t.Errorf("Expected .gitattributes file to be created")
			return
		}

		// Check content
		content, err := os.ReadFile(gitAttributesPath)
		if err != nil {
			t.Fatalf("Failed to read .gitattributes: %v", err)
		}

		if !strings.Contains(string(content), ".github/workflows/*.lock.yml linguist-generated=true merge=ours") {
			t.Errorf("Expected .gitattributes to contain '.github/workflows/*.lock.yml linguist-generated=true merge=ours'")
		}
	})

	t.Run("gitattributes_not_updated_with_flag", func(t *testing.T) {
		// Remove any existing .gitattributes
		os.Remove(".gitattributes")

		// Call addWorkflowsNormal with noGitattributes=true
		err := addWorkflowsNormal([]*WorkflowSpec{spec}, 1, false, false, "", "", false, "", false, true, false, "", false, "")
		if err != nil {
			// We expect this to fail because we don't have a full workflow setup
			t.Logf("Expected error during workflow addition: %v", err)
		}

		// Check that .gitattributes was NOT created
		gitAttributesPath := filepath.Join(tmpDir, ".gitattributes")
		if _, err := os.Stat(gitAttributesPath); !os.IsNotExist(err) {
			t.Errorf("Expected .gitattributes file NOT to be created when --no-gitattributes flag is set")
		}
	})

	t.Run("existing_gitattributes_not_modified_with_flag", func(t *testing.T) {
		// Create a .gitattributes file with existing content
		existingContent := "*.txt linguist-vendored=true\n"
		gitAttributesPath := filepath.Join(tmpDir, ".gitattributes")
		if err := os.WriteFile(gitAttributesPath, []byte(existingContent), 0644); err != nil {
			t.Fatalf("Failed to create .gitattributes: %v", err)
		}

		// Call addWorkflowsNormal with noGitattributes=true
		err := addWorkflowsNormal([]*WorkflowSpec{spec}, 1, false, false, "", "", false, "", false, true, false, "", false, "")
		if err != nil {
			// We expect this to fail because we don't have a full workflow setup
			t.Logf("Expected error during workflow addition: %v", err)
		}

		// Check that .gitattributes was NOT modified
		content, err := os.ReadFile(gitAttributesPath)
		if err != nil {
			t.Fatalf("Failed to read .gitattributes: %v", err)
		}

		if string(content) != existingContent {
			t.Errorf("Expected .gitattributes to remain unchanged when --no-gitattributes flag is set.\nExpected:\n%s\nGot:\n%s",
				existingContent, string(content))
		}
	})
}
