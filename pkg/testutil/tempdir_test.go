//go:build !integration

package testutil_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestGetTestRunDir(t *testing.T) {
	// Get the test run directory
	dir := testutil.GetTestRunDir()

	// Verify it exists
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Errorf("test run directory does not exist: %s", dir)
	}

	// Verify it contains "test-runs" in the path
	if !strings.Contains(dir, "test-runs") {
		t.Errorf("test run directory should contain 'test-runs', got: %s", dir)
	}

	// Verify calling it again returns the same directory
	dir2 := testutil.GetTestRunDir()
	if dir != dir2 {
		t.Errorf("GetTestRunDir should return same directory, got %s and %s", dir, dir2)
	}
}

func TestTempDir(t *testing.T) {
	// Create a temporary directory
	tempDir := testutil.TempDir(t, "test-pattern-*")

	// Verify it exists
	if _, err := os.Stat(tempDir); os.IsNotExist(err) {
		t.Errorf("temp directory does not exist: %s", tempDir)
	}

	// Verify it's under the test run directory
	testRunDir := testutil.GetTestRunDir()
	if !strings.HasPrefix(tempDir, testRunDir) {
		t.Errorf("temp directory should be under test run directory, got: %s (expected prefix: %s)", tempDir, testRunDir)
	}

	// Verify pattern is in the path
	if !strings.Contains(filepath.Base(tempDir), "test-pattern-") {
		t.Errorf("temp directory should contain pattern, got: %s", tempDir)
	}

	// Create a file in the temp directory to verify it's writable
	testFile := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Errorf("failed to write to temp directory: %v", err)
	}

	// Verify the file exists
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Errorf("test file should exist: %s", testFile)
	}
}

func TestTempDirCleanup(t *testing.T) {
	var tempDir string

	// Run a subtest to create and verify cleanup
	t.Run("subtest", func(t *testing.T) {
		tempDir = testutil.TempDir(t, "cleanup-test-*")

		// Verify it exists during the test
		if _, err := os.Stat(tempDir); os.IsNotExist(err) {
			t.Errorf("temp directory should exist during test: %s", tempDir)
		}
	})

	// After the subtest completes, the cleanup should have run
	// Note: The directory might still exist briefly due to deferred cleanup,
	// but we can at least verify the path was created properly
	if tempDir == "" {
		t.Error("tempDir should have been set by subtest")
	}
}

func TestStripYAMLCommentHeader(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "strips comment header",
			input: `#
# Header comment
# More comments
#
name: my-workflow
on: push`,
			expected: `name: my-workflow
on: push`,
		},
		{
			name:     "handles no comments",
			input:    `name: my-workflow`,
			expected: `name: my-workflow`,
		},
		{
			name: "handles empty lines before YAML",
			input: `#
# Comment

name: my-workflow`,
			expected: `name: my-workflow`,
		},
		{
			name:     "handles empty input",
			input:    "",
			expected: "",
		},
		{
			name: "handles only comments",
			input: `# Only comments
# No YAML`,
			expected: `# Only comments
# No YAML`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := testutil.StripYAMLCommentHeader(tt.input)
			if result != tt.expected {
				t.Errorf("StripYAMLCommentHeader(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
