//go:build !integration

package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImportCache(t *testing.T) {
	const (
		owner = "testowner"
		repo  = "testrepo"
		path  = "workflows/test.md"
		sha   = "abc123"
	)
	testContent := []byte("# Test Workflow\n\nTest content")

	t.Run("Set creates file and returns path", func(t *testing.T) {
		cache := NewImportCache(t.TempDir())
		cachedPath, err := cache.Set(owner, repo, path, sha, testContent)
		require.NoError(t, err, "Set should succeed for valid inputs")
		require.FileExists(t, cachedPath, "cache file should be created at expected path")
	})

	t.Run("Get returns cached path after Set", func(t *testing.T) {
		cache := NewImportCache(t.TempDir())
		cachedPath, err := cache.Set(owner, repo, path, sha, testContent)
		require.NoError(t, err, "Set should succeed for valid inputs")
		retrievedPath, found := cache.Get(owner, repo, path, sha)
		assert.True(t, found, "cache entry should be found after Set")
		assert.Equal(t, cachedPath, retrievedPath, "retrieved path should match path returned by Set")
	})

	t.Run("Cached file content matches original", func(t *testing.T) {
		cache := NewImportCache(t.TempDir())
		cachedPath, err := cache.Set(owner, repo, path, sha, testContent)
		require.NoError(t, err, "Set should succeed")
		content, err := os.ReadFile(cachedPath)
		require.NoError(t, err, "reading cached file should succeed")
		assert.Equal(t, string(testContent), string(content), "cached content should match original")
	})

	t.Run("New cache instance finds existing entry", func(t *testing.T) {
		tempDir := t.TempDir()
		cache := NewImportCache(tempDir)
		cachedPath, err := cache.Set(owner, repo, path, sha, testContent)
		require.NoError(t, err, "Set should succeed for valid inputs")
		cache2 := NewImportCache(tempDir)
		retrievedPath2, found := cache2.Get(owner, repo, path, sha)
		assert.True(t, found, "cache entry should be found from new cache instance")
		assert.Equal(t, cachedPath, retrievedPath2, "path from new instance should match original")
	})
}

func TestImportCacheReadOnly(t *testing.T) {
	cache := NewImportCache(t.TempDir())
	cache.SetReadOnly(true)
	_, err := cache.Set("owner", "repo", "shared.md", "sha", []byte("new"))
	require.ErrorContains(t, err, "read-only import cache")
	require.NoDirExists(t, cache.GetCacheDir())

	cache.SetReadOnly(false)
	path, err := cache.Set("owner", "repo", "shared.md", "sha", []byte("original"))
	require.NoError(t, err)
	cache.SetReadOnly(true)
	_, err = cache.Set("owner", "repo", "shared.md", "sha", []byte("replacement"))
	require.ErrorContains(t, err, "read-only import cache")
	gotPath, found := cache.Get("owner", "repo", "shared.md", "sha")
	require.True(t, found)
	require.Equal(t, path, gotPath)
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "original", string(content))
}

func TestImportCacheDirectory(t *testing.T) {
	tempDir := t.TempDir()

	cache := NewImportCache(tempDir)

	// Test cache directory path
	expectedDir := filepath.Join(tempDir, ImportCacheDir)
	assert.Equal(t, expectedDir, cache.GetCacheDir(), "GetCacheDir should return expected path")

	// GetCacheDir works for nested base directories that don't exist yet
	t.Run("nested base dir returns correct path", func(t *testing.T) {
		nestedBase := filepath.Join(t.TempDir(), "nested", "dir")
		nestedCache := NewImportCache(nestedBase)
		assert.Equal(t, filepath.Join(nestedBase, ImportCacheDir), nestedCache.GetCacheDir(),
			"GetCacheDir should return base dir joined with ImportCacheDir for nested paths")
	})

	// Create a cache entry to trigger directory creation
	testContent := []byte("test")
	_, err := cache.Set("owner", "repo", "test.md", "sha1", testContent)
	require.NoError(t, err, "Set should succeed for valid inputs")

	// Verify directory was created
	assert.DirExists(t, expectedDir, "cache directory should be created after Set")

	// Verify .gitattributes was auto-generated
	gitAttributesPath := filepath.Join(expectedDir, ".gitattributes")
	require.FileExists(t, gitAttributesPath, ".gitattributes file should be created in cache directory")

	// Verify .gitattributes content
	content, err := os.ReadFile(gitAttributesPath)
	require.NoError(t, err, "reading .gitattributes should succeed")
	contentStr := string(content)
	assert.Contains(t, contentStr, "linguist-generated=true", ".gitattributes should mark files as linguist-generated")
	assert.Contains(t, contentStr, "merge=ours", ".gitattributes should set merge=ours strategy")
}

func TestImportCacheMissingFile(t *testing.T) {
	tempDir := t.TempDir()

	cache := NewImportCache(tempDir)

	// Add entry to cache
	testContent := []byte("test")
	cachedPath, err := cache.Set("owner", "repo", "test.md", "sha1", testContent)
	require.NoError(t, err, "Set should succeed for valid inputs")

	// Delete the cached file
	err = os.Remove(cachedPath)
	require.NoError(t, err, "removing cached file should succeed")

	// Try to get the entry - should return not found since file is missing
	_, found := cache.Get("owner", "repo", "test.md", "sha1")
	assert.False(t, found, "Get should return cache miss when backing file has been deleted")
}

func TestImportCacheEmptyCache(t *testing.T) {
	tempDir := t.TempDir()

	cache := NewImportCache(tempDir)

	// Try to get from empty cache - should return not found
	_, found := cache.Get("owner", "repo", "test.md", "nonexistent-sha")
	assert.False(t, found, "Get should return cache miss for empty cache")
}

func TestSanitizePath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple path",
			input:    "workflows/test.md",
			expected: "workflows_test.md",
		},
		{
			name:     "nested path",
			input:    "a/b/c/file.md",
			expected: "a_b_c_file.md",
		},
		{
			name:     "already flat",
			input:    "file.md",
			expected: "file.md",
		},
		{
			name:     "path with dots cleaned",
			input:    "a/./b/file.md",
			expected: "a_b_file.md",
		},
		{
			name:     "empty string",
			input:    "",
			expected: ".",
		},
		{
			name:     "trailing slash",
			input:    "a/b/",
			expected: "a_b",
		},
		{
			name:     "single dot component",
			input:    "a/./b",
			expected: "a_b",
		},
		{
			name:     "leading slash becomes root-like",
			input:    "/absolute/path",
			expected: "_absolute_path",
		},
		{
			name:     "dot-prefixed path cleaned",
			input:    "./file.md",
			expected: "file.md",
		},
		{
			name:     "multiple consecutive slashes cleaned",
			input:    "a//b/file.md",
			expected: "a_b_file.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizePath(tt.input)
			assert.Equal(t, tt.expected, result, "sanitizePath(%q) should return expected value", tt.input)
		})
	}
}

func TestValidatePathComponents(t *testing.T) {
	tests := []struct {
		name      string
		owner     string
		repo      string
		path      string
		sha       string
		shouldErr bool
		errMsg    string
	}{
		{
			name:      "valid components",
			owner:     "testowner",
			repo:      "testrepo",
			path:      "workflows/test.md",
			sha:       "abc123",
			shouldErr: false,
		},
		{
			name:      "empty owner",
			owner:     "",
			repo:      "testrepo",
			path:      "test.md",
			sha:       "abc123",
			shouldErr: true,
			errMsg:    "empty component",
		},
		{
			name:      "empty sha",
			owner:     "testowner",
			repo:      "testrepo",
			path:      "test.md",
			sha:       "",
			shouldErr: true,
			errMsg:    "empty component",
		},
		{
			name:      "path traversal in owner",
			owner:     "../etc",
			repo:      "testrepo",
			path:      "test.md",
			sha:       "abc123",
			shouldErr: true,
			errMsg:    "..",
		},
		{
			name:      "path traversal in path",
			owner:     "testowner",
			repo:      "testrepo",
			path:      "../../etc/passwd",
			sha:       "abc123",
			shouldErr: true,
			errMsg:    "..",
		},
		{
			name:      "absolute path in sha",
			owner:     "testowner",
			repo:      "testrepo",
			path:      "test.md",
			sha:       "/absolute/path",
			shouldErr: true,
			errMsg:    "absolute path",
		},
		{
			name:      "empty repo",
			owner:     "testowner",
			repo:      "",
			path:      "test.md",
			sha:       "abc123",
			shouldErr: true,
			errMsg:    "empty component",
		},
		{
			name:      "empty path",
			owner:     "testowner",
			repo:      "testrepo",
			path:      "",
			sha:       "abc123",
			shouldErr: true,
			errMsg:    "empty component",
		},
		{
			name:      "path traversal embedded in sha",
			owner:     "testowner",
			repo:      "testrepo",
			path:      "test.md",
			sha:       "abc..def",
			shouldErr: true,
			errMsg:    "..",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePathComponents(tt.owner, tt.repo, tt.path, tt.sha)
			if tt.shouldErr {
				require.Error(t, err, "should return error for: %s", tt.name)
				require.ErrorContains(t, err, tt.errMsg, "error message should mention: %s", tt.errMsg)
			} else {
				require.NoError(t, err, "should not return error for valid components")
			}
		})
	}
}

func TestImportCacheSet_Validation(t *testing.T) {
	tempDir := t.TempDir()

	tests := []struct {
		name      string
		owner     string
		repo      string
		path      string
		sha       string
		content   []byte
		shouldErr bool
		errMsg    string
	}{
		{
			name:      "oversized content rejected",
			owner:     "owner",
			repo:      "repo",
			path:      "test.md",
			sha:       "abc123",
			content:   make([]byte, 10*1024*1024+1), // 10MB + 1 byte
			shouldErr: true,
			errMsg:    "exceeds maximum",
		},
		{
			name:      "path traversal in owner rejected",
			owner:     "../etc",
			repo:      "repo",
			path:      "test.md",
			sha:       "abc123",
			content:   []byte("content"),
			shouldErr: true,
			errMsg:    "invalid path components",
		},
		{
			name:      "path traversal in path rejected",
			owner:     "owner",
			repo:      "repo",
			path:      "../../etc/passwd",
			sha:       "abc123",
			content:   []byte("content"),
			shouldErr: true,
			errMsg:    "invalid path components",
		},
		{
			name:      "empty owner rejected",
			owner:     "",
			repo:      "repo",
			path:      "test.md",
			sha:       "abc123",
			content:   []byte("content"),
			shouldErr: true,
			errMsg:    "invalid path components",
		},
		{
			name:      "valid inputs succeed",
			owner:     "owner",
			repo:      "repo",
			path:      "workflows/test.md",
			sha:       "abc123def456",
			content:   []byte("# valid content"),
			shouldErr: false,
		},
	}

	cache := NewImportCache(tempDir)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := cache.Set(tt.owner, tt.repo, tt.path, tt.sha, tt.content)
			if tt.shouldErr {
				require.Error(t, err, "Set should reject: %s", tt.name)
				require.ErrorContains(t, err, tt.errMsg, "error message should contain %q", tt.errMsg)
			} else {
				assert.NoError(t, err, "Set should succeed for: %s", tt.name)
			}
		})
	}
}

func TestImportCacheSetIdempotent(t *testing.T) {
	tempDir := t.TempDir()
	cache := NewImportCache(tempDir)

	firstContent := []byte("first content")
	secondContent := []byte("second content")

	path1, err := cache.Set("owner", "repo", "test.md", "sha1", firstContent)
	require.NoError(t, err, "first Set should succeed")

	path2, err := cache.Set("owner", "repo", "test.md", "sha1", secondContent)
	require.NoError(t, err, "second Set with same key should succeed (overwrite)")
	assert.Equal(t, path1, path2, "both Set calls should return the same cache path")

	content, err := os.ReadFile(path2)
	require.NoError(t, err, "reading overwritten file should succeed")
	assert.Equal(t, string(secondContent), string(content), "file should contain second (latest) content")
}

func TestImportCacheGetValidatesComponents(t *testing.T) {
	tempDir := t.TempDir()
	cache := NewImportCache(tempDir)

	traversalTarget := filepath.Join(tempDir, ".github", "aw", "etc", "repo", "sha", "test.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(traversalTarget), 0700))
	require.NoError(t, os.WriteFile(traversalTarget, []byte("decoy"), 0600))

	// Should return not-found (not panic or error), even with suspicious inputs.
	_, found := cache.Get("../etc", "repo", "test.md", "sha")
	assert.False(t, found, "Get with path traversal input should return not-found, not panic")
	require.FileExists(t, traversalTarget)
}
