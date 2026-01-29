//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGetRepositoryRelativePath tests that paths are correctly converted to repository-relative paths
func TestGetRepositoryRelativePath(t *testing.T) {
	// Get the actual repository root
	repoRoot, err := findGitRoot()
	if err != nil {
		t.Skipf("Skipping test: not in a git repository: %v", err)
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "workflow in .github/workflows",
			input:    filepath.Join(repoRoot, ".github", "workflows", "test.md"),
			expected: ".github/workflows/test.md",
		},
		{
			name:     "existing workflow file",
			input:    filepath.Join(repoRoot, ".github", "workflows", "audit-workflows.md"),
			expected: ".github/workflows/audit-workflows.md",
		},
		{
			name:     "existing root file",
			input:    filepath.Join(repoRoot, "README.md"),
			expected: "README.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := getRepositoryRelativePath(tt.input)
			if err != nil {
				t.Errorf("getRepositoryRelativePath(%q) error = %v", tt.input, err)
				return
			}
			if result != tt.expected {
				t.Errorf("getRepositoryRelativePath(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestGetRepositoryRelativePathConsistency verifies that the same logical path
// produces the same relative path regardless of how it's constructed
func TestGetRepositoryRelativePathConsistency(t *testing.T) {
	// Get the actual repository root
	repoRoot, err := findGitRoot()
	if err != nil {
		t.Skipf("Skipping test: not in a git repository: %v", err)
	}

	// Test different ways of constructing the same path
	path1 := filepath.Join(repoRoot, ".github", "workflows", "test.md")
	path2 := filepath.Join(repoRoot, ".github/workflows/test.md")

	// Change to repo root directory temporarily
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)

	if err := os.Chdir(repoRoot); err != nil {
		t.Fatal(err)
	}

	// Relative path that needs to be made absolute
	path3 := ".github/workflows/test.md"

	result1, err := getRepositoryRelativePath(path1)
	if err != nil {
		t.Fatalf("getRepositoryRelativePath(%q) error = %v", path1, err)
	}

	result2, err := getRepositoryRelativePath(path2)
	if err != nil {
		t.Fatalf("getRepositoryRelativePath(%q) error = %v", path2, err)
	}

	result3, err := getRepositoryRelativePath(path3)
	if err != nil {
		t.Fatalf("getRepositoryRelativePath(%q) error = %v", path3, err)
	}

	if result1 != result2 {
		t.Errorf("Different constructions produced different results: %q vs %q", result1, result2)
	}

	if result1 != result3 {
		t.Errorf("Absolute and relative paths produced different results: %q vs %q", result1, result3)
	}

	// Verify forward slashes are used (cross-platform consistency)
	expected := ".github/workflows/test.md"
	if result1 != expected {
		t.Errorf("Expected %q, got %q", expected, result1)
	}
}

// TestGetRepositoryRelativePathCrossPlatform verifies that the path separator
// is normalized to forward slashes for cross-platform stability
func TestGetRepositoryRelativePathCrossPlatform(t *testing.T) {
	// Get the actual repository root
	repoRoot, err := findGitRoot()
	if err != nil {
		t.Skipf("Skipping test: not in a git repository: %v", err)
	}

	// Test with a path that would have backslashes on Windows
	testPath := filepath.Join(repoRoot, ".github", "workflows", "daily-test.md")

	result, err := getRepositoryRelativePath(testPath)
	if err != nil {
		t.Fatalf("getRepositoryRelativePath(%q) error = %v", testPath, err)
	}

	// Verify result uses forward slashes, not backslashes
	// This ensures the same hash on Windows, Linux, and macOS
	expected := ".github/workflows/daily-test.md"
	if result != expected {
		t.Errorf("Expected normalized path %q, got %q", expected, result)
	}

	// Verify no backslashes in the result
	for i, ch := range result {
		if ch == '\\' {
			t.Errorf("Found backslash at position %d in result %q", i, result)
		}
	}
}
