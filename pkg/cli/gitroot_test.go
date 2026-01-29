//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindGitRoot(t *testing.T) {
	// Save current directory
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current working directory: %v", err)
	}

	// Try to find the git root from current location
	root, err := findGitRoot()
	if err != nil {
		// If we're not in a git repository, try changing to the project root
		// This handles cases where tests are run from outside the git repo
		projectRoot := filepath.Join(originalWd, "..", "..")
		if err := os.Chdir(projectRoot); err != nil {
			t.Skipf("Cannot find git root and cannot change to project root: %v", err)
		}
		defer func() {
			_ = os.Chdir(originalWd) // Best effort restoration
		}()

		// Try again from project root
		root, err = findGitRoot()
		if err != nil {
			t.Skipf("Expected to find git root, but got error: %v", err)
		}
	}

	if root == "" {
		t.Fatal("Expected non-empty git root")
	}

	// Check that the returned path exists
	if _, err := os.Stat(root); os.IsNotExist(err) {
		t.Fatalf("Git root path does not exist: %s", root)
	}

	// Check that .git directory exists in the root
	gitDir := filepath.Join(root, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		t.Fatalf(".git directory does not exist in reported git root: %s", gitDir)
	}

	t.Logf("Git root found: %s", root)
}
