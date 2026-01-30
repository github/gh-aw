//go:build !integration

package cli

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestEnsureGitAttributes(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "gh-aw-gitattributes-test")
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

	tests := []struct {
		name            string
		existingContent string
		expectedContent string
	}{
		{
			name:            "creates new gitattributes file",
			existingContent: "",
			expectedContent: ".github/workflows/*.lock.yml linguist-generated=true merge=ours",
		},
		{
			name:            "adds entry to existing file",
			existingContent: "*.generated linguist-generated=true\n",
			expectedContent: "*.generated linguist-generated=true\n\n.github/workflows/*.lock.yml linguist-generated=true merge=ours",
		},
		{
			name:            "does not duplicate existing entry",
			existingContent: ".github/workflows/*.lock.yml linguist-generated=true merge=ours\n",
			expectedContent: ".github/workflows/*.lock.yml linguist-generated=true merge=ours",
		},
		{
			name:            "does not duplicate entry with different order",
			existingContent: "*.md linguist-documentation=true\n.github/workflows/*.lock.yml linguist-generated=true merge=ours\n*.txt text=auto\n",
			expectedContent: "*.md linguist-documentation=true\n.github/workflows/*.lock.yml linguist-generated=true merge=ours\n*.txt text=auto",
		},
		{
			name:            "updates old format entry",
			existingContent: "*.md linguist-documentation=true\n.github/workflows/*.lock.yml linguist-generated=true\n*.txt text=auto\n",
			expectedContent: "*.md linguist-documentation=true\n.github/workflows/*.lock.yml linguist-generated=true merge=ours\n*.txt text=auto",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gitAttributesPath := ".gitattributes"

			// Remove any existing .gitattributes
			os.Remove(gitAttributesPath)

			// Create initial content if specified
			if tt.existingContent != "" {
				if err := os.WriteFile(gitAttributesPath, []byte(tt.existingContent), 0644); err != nil {
					t.Fatalf("Failed to create initial .gitattributes: %v", err)
				}
			}

			// Call the function
			err := ensureGitAttributes()
			if err != nil {
				t.Fatalf("ensureGitAttributes() returned error: %v", err)
			}

			// Check that file exists
			if _, err := os.Stat(gitAttributesPath); os.IsNotExist(err) {
				t.Fatalf("Expected .gitattributes file to exist")
			}

			// Check content
			content, err := os.ReadFile(gitAttributesPath)
			if err != nil {
				t.Fatalf("Failed to read .gitattributes: %v", err)
			}

			contentStr := strings.TrimSpace(string(content))
			expectedStr := strings.TrimSpace(tt.expectedContent)

			if contentStr != expectedStr {
				t.Errorf("Expected content:\n%s\n\nGot content:\n%s", expectedStr, contentStr)
			}

			// Verify the entry is actually present
			if !strings.Contains(string(content), ".github/workflows/*.lock.yml linguist-generated=true merge=ours") {
				t.Errorf("Expected .gitattributes to contain '.github/workflows/*.lock.yml linguist-generated=true merge=ours'")
			}
			// Verify campaign.g.md entry is NOT present (it's now in .gitignore)
			if strings.Contains(string(content), ".github/workflows/*.campaign.g.md") {
				t.Errorf("Did not expect .gitattributes to contain '.github/workflows/*.campaign.g.md' (should be in .gitignore)")
			}
		})
	}
}

func TestEnsureGitAttributesNotInGitRepo(t *testing.T) {
	// Create a temporary directory for testing (not a git repo)
	tmpDir, err := os.MkdirTemp("", "gh-aw-nogit-test")
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

	// Call ensureGitAttributes in non-git directory
	err = ensureGitAttributes()
	if err == nil {
		t.Errorf("Expected error when not in git repository, got nil")
	}

	// Verify no .gitattributes file was created
	if _, err := os.Stat(".gitattributes"); !os.IsNotExist(err) {
		t.Errorf("Expected no .gitattributes file to be created outside git repository")
	}
}
