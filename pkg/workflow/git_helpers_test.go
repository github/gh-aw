//go:build !integration

package workflow

import (
	"os"
	"testing"
)

func TestGetCurrentGitTag(t *testing.T) {
	// Save original environment
	origRef := os.Getenv("GITHUB_REF")
	defer func() {
		if origRef != "" {
			os.Setenv("GITHUB_REF", origRef)
		} else {
			os.Unsetenv("GITHUB_REF")
		}
	}()

	t.Run("GITHUB_REF with tag", func(t *testing.T) {
		os.Setenv("GITHUB_REF", "refs/tags/v1.2.3")

		tag := GetCurrentGitTag()
		if tag != "v1.2.3" {
			t.Errorf("Expected tag 'v1.2.3', got %q", tag)
		}
	})

	t.Run("GITHUB_REF without tag", func(t *testing.T) {
		os.Setenv("GITHUB_REF", "refs/heads/main")

		tag := GetCurrentGitTag()
		if tag != "" {
			t.Errorf("Expected empty tag on branch, got %q", tag)
		}
	})

	t.Run("no GITHUB_REF", func(t *testing.T) {
		os.Unsetenv("GITHUB_REF")

		tag := GetCurrentGitTag()
		// Will try git describe - may or may not return a tag depending on repo state
		// Just verify it doesn't crash
		t.Logf("Tag from git describe: %q", tag)
	})
}
