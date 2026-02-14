package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	testRunDir     string
	testRunDirOnce sync.Once
)

// GetTestRunDir returns the unique directory for this test run.
// It creates a directory in the system temp directory with a unique subdirectory
// based on the current timestamp and process ID.
// This ensures test directories are completely isolated from any git repository.
func GetTestRunDir() string {
	testRunDirOnce.Do(func() {
		// Use system temp directory to avoid git repository issues
		systemTempDir := os.TempDir()

		// Create gh-aw-test-runs directory in system temp
		testRunsDir := filepath.Join(systemTempDir, "gh-aw-test-runs")
		if err := os.MkdirAll(testRunsDir, 0o755); err != nil {
			panic(fmt.Sprintf("failed to create test-runs directory: %v", err))
		}

		// Create unique subdirectory for this test run
		timestamp := time.Now().Format("20060102-150405")
		pid := os.Getpid()
		testRunDir = filepath.Join(testRunsDir, fmt.Sprintf("%s-%d", timestamp, pid))

		if err := os.MkdirAll(testRunDir, 0o755); err != nil {
			panic(fmt.Sprintf("failed to create test run directory: %v", err))
		}
	})

	return testRunDir
}

// TempDir creates a temporary directory for testing within the test run directory.
// It automatically cleans up the directory when the test completes.
// This replaces the use of os.MkdirTemp or t.TempDir() to ensure all test
// artifacts are isolated in a known location outside any git repository.
func TempDir(t *testing.T, pattern string) string {
	t.Helper()

	baseDir := GetTestRunDir()

	// Create a unique subdirectory within the test run directory
	tempDir, err := os.MkdirTemp(baseDir, pattern)
	if err != nil {
		t.Fatalf("failed to create temp directory: %v", err)
	}

	// Register cleanup to remove the directory after test completes
	t.Cleanup(func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Warning: failed to clean up temporary directory %s: %v", tempDir, err)
		}
	})

	return tempDir
}

// StripYAMLCommentHeader removes the comment header from generated YAML files
// and returns only the non-comment YAML content. This is useful for tests that
// need to verify content without matching strings in the comment header.
func StripYAMLCommentHeader(yamlContent string) string {
	lines := strings.Split(yamlContent, "\n")
	for i, line := range lines {
		// Find the first non-comment, non-empty line (start of actual YAML)
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			return strings.Join(lines[i:], "\n")
		}
	}
	return yamlContent
}
