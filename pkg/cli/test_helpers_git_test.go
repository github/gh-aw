package cli

import (
	"os"
	"path/filepath"
)

// initTestGitRepo initializes a git repository in the test directory.
// This is a shared helper used by both unit and integration tests.
func initTestGitRepo(dir string) error {
	// Create .git directory structure to simulate being in a git repo
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		return err
	}

	// Create subdirectories
	subdirs := []string{"objects", "refs", "refs/heads", "refs/tags"}
	for _, subdir := range subdirs {
		if err := os.MkdirAll(filepath.Join(gitDir, subdir), 0755); err != nil {
			return err
		}
	}

	// Create HEAD file pointing to main branch
	headFile := filepath.Join(gitDir, "HEAD")
	if err := os.WriteFile(headFile, []byte("ref: refs/heads/main\n"), 0644); err != nil {
		return err
	}

	// Create a minimal git config
	configFile := filepath.Join(gitDir, "config")
	configContent := `[core]
	repositoryformatversion = 0
	filemode = true
	bare = false
	logallrefupdates = true
[user]
	name = Test User
	email = test@example.com`

	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		return err
	}

	// Create description file
	descFile := filepath.Join(gitDir, "description")
	if err := os.WriteFile(descFile, []byte("Test repository"), 0644); err != nil {
		return err
	}

	return nil
}
