//go:build !integration

package fileutil

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateAbsolutePath(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		shouldError bool
		errorMsg    string
	}{
		{
			name:        "valid absolute Unix path",
			path:        "/home/user/file.txt",
			shouldError: false,
		},
		{
			name:        "valid absolute path with cleaned components",
			path:        "/home/user/../user/file.txt",
			shouldError: false,
		},
		{
			name:        "empty path",
			path:        "",
			shouldError: true,
			errorMsg:    "path cannot be empty",
		},
		{
			name:        "relative path",
			path:        "relative/path.txt",
			shouldError: true,
			errorMsg:    "path must be absolute",
		},
		{
			name:        "relative path with dot",
			path:        "./file.txt",
			shouldError: true,
			errorMsg:    "path must be absolute",
		},
		{
			name:        "relative path with double dot",
			path:        "../file.txt",
			shouldError: true,
			errorMsg:    "path must be absolute",
		},
		{
			name:        "path traversal attempt",
			path:        "../../../etc/passwd",
			shouldError: true,
			errorMsg:    "path must be absolute",
		},
		{
			name:        "single dot",
			path:        ".",
			shouldError: true,
			errorMsg:    "path must be absolute",
		},
		{
			name:        "double dot",
			path:        "..",
			shouldError: true,
			errorMsg:    "path must be absolute",
		},
	}

	// Add Windows-specific tests only on Windows
	if runtime.GOOS == "windows" {
		tests = append(tests, []struct {
			name        string
			path        string
			shouldError bool
			errorMsg    string
		}{
			{
				name:        "valid absolute Windows path",
				path:        "C:\\Users\\user\\file.txt",
				shouldError: false,
			},
			{
				name:        "valid absolute Windows UNC path",
				path:        "\\\\server\\share\\file.txt",
				shouldError: false,
			},
		}...)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ValidateAbsolutePath(tt.path)

			if tt.shouldError {
				require.Error(t, err, "Expected error for path: %s", tt.path)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg, "Error message should contain expected text")
				}
				assert.Empty(t, result, "Result should be empty on error")
			} else {
				require.NoError(t, err, "Should not error for valid absolute path: %s", tt.path)
				assert.NotEmpty(t, result, "Result should not be empty")
				assert.True(t, filepath.IsAbs(result), "Result should be an absolute path: %s", result)
				// Verify path is cleaned (no .. components)
				assert.NotContains(t, result, "..", "Cleaned path should not contain .. components")
			}
		})
	}
}

func TestValidateAbsolutePath_Cleaning(t *testing.T) {
	// Test that paths are properly cleaned
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "path with redundant separators",
			path:     "/home//user///file.txt",
			expected: "/home/user/file.txt",
		},
		{
			name:     "path with trailing separator",
			path:     "/home/user/",
			expected: "/home/user",
		},
		{
			name:     "path with . components",
			path:     "/home/./user/./file.txt",
			expected: "/home/user/file.txt",
		},
		{
			name:     "path with .. components",
			path:     "/home/user/../user/file.txt",
			expected: "/home/user/file.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Only run on Unix systems for consistent path separators
			if runtime.GOOS != "windows" {
				result, err := ValidateAbsolutePath(tt.path)
				require.NoError(t, err, "Should not error for valid absolute path")
				assert.Equal(t, tt.expected, result, "Path should be cleaned correctly")
			}
		})
	}
}

func TestValidateAbsolutePath_SecurityScenarios(t *testing.T) {
	// Test common path traversal attack patterns
	traversalPatterns := []string{
		"../../etc/passwd",
		"../../../etc/passwd",
		"../../../../etc/passwd",
		"..\\..\\windows\\system32\\config\\sam",
		"./../../../etc/passwd",
		"./../../etc/passwd",
	}

	for _, pattern := range traversalPatterns {
		t.Run("blocks_"+strings.ReplaceAll(pattern, "/", "_"), func(t *testing.T) {
			result, err := ValidateAbsolutePath(pattern)
			require.Error(t, err, "Should reject path traversal pattern: %s", pattern)
			assert.Contains(t, err.Error(), "path must be absolute", "Error should mention absolute path requirement")
			assert.Empty(t, result, "Result should be empty for invalid path")
		})
	}
}
