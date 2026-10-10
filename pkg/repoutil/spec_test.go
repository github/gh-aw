//go:build !integration

package repoutil

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSpec_PublicAPI_SplitRepoSlug validates the documented behavior of
// SplitRepoSlug as described in the repoutil README.md specification.
func TestSpec_PublicAPI_SplitRepoSlug(t *testing.T) {
	tests := []struct {
		name          string
		slug          string
		expectedOwner string
		expectedRepo  string
		expectError   bool
	}{
		{
			name:          "valid slug returns owner and repo",
			slug:          "github/gh-aw",
			expectedOwner: "github",
			expectedRepo:  "gh-aw",
			expectError:   false,
		},
		{
			name:        "missing separator returns error",
			slug:        "github",
			expectError: true,
		},
		{
			name:        "empty owner returns error",
			slug:        "/gh-aw",
			expectError: true,
		},
		{
			name:        "empty repo returns error",
			slug:        "github/",
			expectError: true,
		},
		{
			name:        "too many separators returns error",
			slug:        "github/gh-aw/x",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := SplitRepoSlug(tt.slug)
			if tt.expectError {
				require.Error(t, err, "should return error for slug: %q", tt.slug)
				assert.Empty(t, owner, "owner should be empty when error occurs for slug: %q", tt.slug)
				assert.Empty(t, repo, "repo should be empty when error occurs for slug: %q", tt.slug)
				return
			}
			require.NoError(t, err, "unexpected error for slug: %q", tt.slug)
			assert.Equal(t, tt.expectedOwner, owner, "owner should be %q for slug %q", tt.expectedOwner, tt.slug)
			assert.Equal(t, tt.expectedRepo, repo, "repo should be %q for slug %q", tt.expectedRepo, tt.slug)
		})
	}
}

// TestSpec_PublicAPI_NormalizeRepoForAPI validates the documented behavior of
// NormalizeRepoForAPI as described in the repoutil README.md specification.
func TestSpec_PublicAPI_NormalizeRepoForAPI(t *testing.T) {
	tests := []struct {
		name              string
		input             string
		expectedOwnerRepo string
		expectedHost      string
	}{
		{
			name:              "plain owner repo keeps empty host",
			input:             "github/gh-aw",
			expectedOwnerRepo: "github/gh-aw",
			expectedHost:      "",
		},
		{
			name:              "host owner repo splits into owner repo and host",
			input:             "ghe.example.com/github/gh-aw",
			expectedOwnerRepo: "github/gh-aw",
			expectedHost:      "ghe.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ownerRepo, host := NormalizeRepoForAPI(tt.input)
			assert.Equal(t, tt.expectedOwnerRepo, ownerRepo, "owner/repo mismatch for input %q", tt.input)
			assert.Equal(t, tt.expectedHost, host, "host mismatch for input %q", tt.input)
		})
	}
}

// TestSpec_ThreadSafety_RepositoryHelpers validates the documented guarantee
// that the pure, stateless repository helpers may be called concurrently.
func TestSpec_ThreadSafety_RepositoryHelpers(t *testing.T) {
	const callers = 32
	var waitGroup sync.WaitGroup
	errors := make(chan error, callers)

	for range callers {
		waitGroup.Go(func() {
			owner, repo, err := SplitRepoSlug("github/gh-aw")
			if err != nil {
				errors <- err
				return
			}
			if owner != "github" || repo != "gh-aw" {
				errors <- assert.AnError
				return
			}

			ownerRepo, host := NormalizeRepoForAPI("ghe.example.com/github/gh-aw")
			if ownerRepo != "github/gh-aw" || host != "ghe.example.com" {
				errors <- assert.AnError
			}
		})
	}

	waitGroup.Wait()
	close(errors)
	assert.Empty(t, errors,
		"concurrent calls to the documented pure repository helpers should succeed")
}
