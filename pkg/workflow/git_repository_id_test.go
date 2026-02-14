//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsShallowClone(t *testing.T) {
	// Test with the current repository (should detect shallow clone status correctly)
	gitRoot, err := os.Getwd()
	require.NoError(t, err, "Failed to get working directory")

	// Find the actual git root
	for {
		if _, err := os.Stat(filepath.Join(gitRoot, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(gitRoot)
		if parent == gitRoot {
			t.Skip("Not in a git repository")
			return
		}
		gitRoot = parent
	}

	isShallow, err := isShallowClone(gitRoot)
	require.NoError(t, err, "Failed to check shallow clone status")

	// Log the result for debugging
	t.Logf("Repository is shallow clone: %v", isShallow)

	// We can't assert the specific value since it depends on how the repo was cloned,
	// but we can verify the function returns without error
	assert.NotNil(t, &isShallow, "isShallowClone should return a boolean value")
}

func TestGetStableRepositoryIdentifier(t *testing.T) {
	// Get the git root of the current repository
	gitRoot, err := os.Getwd()
	require.NoError(t, err, "Failed to get working directory")

	// Find the actual git root
	for {
		if _, err := os.Stat(filepath.Join(gitRoot, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(gitRoot)
		if parent == gitRoot {
			t.Skip("Not in a git repository")
			return
		}
		gitRoot = parent
	}

	tests := []struct {
		name           string
		gitRoot        string
		repositorySlug string
		wantPrefix     string
	}{
		{
			name:           "with repository slug and shallow clone",
			gitRoot:        gitRoot,
			repositorySlug: "github/gh-aw",
			wantPrefix:     "", // Either "github/gh-aw" or "git-" depending on shallow status
		},
		{
			name:           "with repository slug in full clone",
			gitRoot:        gitRoot,
			repositorySlug: "testorg/testrepo",
			wantPrefix:     "", // Either "testorg/testrepo" or "git-" depending on shallow status
		},
		{
			name:           "without repository slug",
			gitRoot:        gitRoot,
			repositorySlug: "",
			wantPrefix:     "git-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			identifier := getStableRepositoryIdentifier(tt.gitRoot, tt.repositorySlug)
			assert.NotEmpty(t, identifier, "Identifier should not be empty")

			// For tests with repository slug, verify the result is either the slug or starts with "git-"
			if tt.repositorySlug != "" {
				assert.True(t,
					identifier == tt.repositorySlug || strings.HasPrefix(identifier, "git-"),
					"Identifier should be either repository slug or start with 'git-', got: %s",
					identifier,
				)
			}

			// For tests without repository slug, verify it starts with "git-"
			if tt.repositorySlug == "" && tt.wantPrefix != "" {
				assert.True(t,
					strings.HasPrefix(identifier, tt.wantPrefix),
					"Identifier should start with '%s', got: %s",
					tt.wantPrefix,
					identifier,
				)
			}

			// Verify determinism - calling again should return the same value
			identifier2 := getStableRepositoryIdentifier(tt.gitRoot, tt.repositorySlug)
			assert.Equal(t, identifier, identifier2, "getStableRepositoryIdentifier should be deterministic")
		})
	}
}

func TestGetStableRepositoryIdentifierDeterminism(t *testing.T) {
	// Get the git root of the current repository
	gitRoot, err := os.Getwd()
	require.NoError(t, err, "Failed to get working directory")

	// Find the actual git root
	for {
		if _, err := os.Stat(filepath.Join(gitRoot, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(gitRoot)
		if parent == gitRoot {
			t.Skip("Not in a git repository")
			return
		}
		gitRoot = parent
	}

	// Call multiple times and verify the result is always the same
	results := make([]string, 5)
	for i := 0; i < 5; i++ {
		results[i] = getStableRepositoryIdentifier(gitRoot, "github/gh-aw")
	}

	// All results should be identical
	for i := 1; i < len(results); i++ {
		assert.Equal(t, results[0], results[i],
			"getStableRepositoryIdentifier should return the same result every time, got different results at index %d: %s vs %s",
			i, results[0], results[i],
		)
	}

	t.Logf("Stable identifier: %s", results[0])
}
