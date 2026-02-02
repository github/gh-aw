//go:build !integration

package workflow

import (
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestExtractBaseRepo(t *testing.T) {
	tests := []struct {
		name     string
		repo     string
		expected string
	}{
		{
			name:     "simple repo",
			repo:     "actions/checkout",
			expected: "actions/checkout",
		},
		{
			name:     "repo with subpath",
			repo:     "github/codeql-action/upload-sarif",
			expected: "github/codeql-action",
		},
		{
			name:     "repo with multiple subpaths",
			repo:     "owner/repo/sub/path",
			expected: "owner/repo",
		},
		{
			name:     "single part repo",
			repo:     "myrepo",
			expected: "myrepo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractBaseRepo(tt.repo)
			if result != tt.expected {
				t.Errorf("extractBaseRepo(%q) = %q, want %q", tt.repo, result, tt.expected)
			}
		})
	}
}

func TestActionResolverCache(t *testing.T) {
	// Create a cache and resolver
	tmpDir := testutil.TempDir(t, "test-*")
	cache := NewActionCache(tmpDir)
	resolver := NewActionResolver(cache)

	// Manually add an entry to the cache
	cache.Set("actions/checkout", "v5", "test-sha-123")

	// Resolve should return cached value without making API call
	sha, err := resolver.ResolveSHA("actions/checkout", "v5")
	if err != nil {
		t.Errorf("Expected no error for cached entry, got: %v", err)
	}
	if sha != "test-sha-123" {
		t.Errorf("Expected SHA 'test-sha-123', got '%s'", sha)
	}
}

// Note: Testing the actual GitHub API resolution requires network access
// and is tested in integration tests or with network-dependent test tags
