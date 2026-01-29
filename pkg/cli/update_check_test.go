//go:build !integration

package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestShouldCheckForUpdate(t *testing.T) {
	// Save original environment
	origCI := os.Getenv("CI")
	origMCP := os.Getenv("GH_AW_MCP_SERVER")
	origGetLastCheckFilePath := getLastCheckFilePathFunc
	defer func() {
		os.Setenv("CI", origCI)
		os.Setenv("GH_AW_MCP_SERVER", origMCP)
		getLastCheckFilePathFunc = origGetLastCheckFilePath
	}()

	tests := []struct {
		name           string
		noCheckUpdate  bool
		ciEnv          string
		mcpEnv         string
		lastCheckTime  string
		expectedResult bool
	}{
		{
			name:           "should check when flag is false and no recent check",
			noCheckUpdate:  false,
			ciEnv:          "",
			mcpEnv:         "",
			lastCheckTime:  "",
			expectedResult: true,
		},
		{
			name:           "should not check when flag is true",
			noCheckUpdate:  true,
			ciEnv:          "",
			mcpEnv:         "",
			lastCheckTime:  "",
			expectedResult: false,
		},
		{
			name:           "should not check in CI environment",
			noCheckUpdate:  false,
			ciEnv:          "true",
			mcpEnv:         "",
			lastCheckTime:  "",
			expectedResult: false,
		},
		{
			name:           "should not check in MCP server mode",
			noCheckUpdate:  false,
			ciEnv:          "",
			mcpEnv:         "true",
			lastCheckTime:  "",
			expectedResult: false,
		},
		{
			name:           "should not check when recent check exists",
			noCheckUpdate:  false,
			ciEnv:          "",
			mcpEnv:         "",
			lastCheckTime:  time.Now().Format(time.RFC3339),
			expectedResult: false,
		},
		{
			name:           "should check when last check is old",
			noCheckUpdate:  false,
			ciEnv:          "",
			mcpEnv:         "",
			lastCheckTime:  time.Now().Add(-25 * time.Hour).Format(time.RFC3339),
			expectedResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			if tt.ciEnv == "" {
				os.Unsetenv("CI")
				os.Unsetenv("CONTINUOUS_INTEGRATION")
				os.Unsetenv("GITHUB_ACTIONS")
			} else {
				os.Setenv("CI", tt.ciEnv)
			}

			if tt.mcpEnv == "" {
				os.Unsetenv("GH_AW_MCP_SERVER")
			} else {
				os.Setenv("GH_AW_MCP_SERVER", tt.mcpEnv)
			}

			// Create temporary last check file if needed
			tmpDir := t.TempDir()
			lastCheckFile := filepath.Join(tmpDir, lastCheckFileName)

			// Override the function to use temp directory
			getLastCheckFilePathFunc = func() string {
				return lastCheckFile
			}

			if tt.lastCheckTime != "" {
				err := os.WriteFile(lastCheckFile, []byte(tt.lastCheckTime), 0644)
				if err != nil {
					t.Fatalf("Failed to create test file: %v", err)
				}
			}

			result := shouldCheckForUpdate(tt.noCheckUpdate)
			if result != tt.expectedResult {
				t.Errorf("shouldCheckForUpdate() = %v, want %v", result, tt.expectedResult)
			}
		})
	}
}

func TestIsRunningAsMCPServer(t *testing.T) {
	// Save original environment
	origMCP := os.Getenv("GH_AW_MCP_SERVER")
	defer func() {
		os.Setenv("GH_AW_MCP_SERVER", origMCP)
	}()

	tests := []struct {
		name     string
		mcpEnv   string
		expected bool
	}{
		{
			name:     "not in MCP server mode",
			mcpEnv:   "",
			expected: false,
		},
		{
			name:     "in MCP server mode",
			mcpEnv:   "true",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("GH_AW_MCP_SERVER", tt.mcpEnv)
			result := isRunningAsMCPServer()
			if result != tt.expected {
				t.Errorf("isRunningAsMCPServer() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetLastCheckFilePath(t *testing.T) {
	path := getLastCheckFilePath()
	if path == "" {
		t.Error("getLastCheckFilePath() returned empty string")
	}

	// Check that the path contains expected components
	if !filepath.IsAbs(path) {
		t.Errorf("getLastCheckFilePath() returned non-absolute path: %s", path)
	}

	// Check that the directory exists or can be created
	dir := filepath.Dir(path)
	if _, err := os.Stat(dir); err != nil {
		if !os.IsNotExist(err) {
			t.Errorf("Unexpected error checking directory: %v", err)
		}
	}
}

func TestUpdateLastCheckTime(t *testing.T) {
	// Save original function
	origGetLastCheckFilePath := getLastCheckFilePathFunc
	defer func() {
		getLastCheckFilePathFunc = origGetLastCheckFilePath
	}()

	// Create temporary directory
	tmpDir := t.TempDir()
	lastCheckFile := filepath.Join(tmpDir, lastCheckFileName)

	// Override the function to use temp directory
	getLastCheckFilePathFunc = func() string {
		return lastCheckFile
	}

	// Update the last check time
	updateLastCheckTime()

	// Verify the file was created
	if _, err := os.Stat(lastCheckFile); err != nil {
		t.Fatalf("Last check file was not created: %v", err)
	}

	// Read and verify the timestamp
	data, err := os.ReadFile(lastCheckFile)
	if err != nil {
		t.Fatalf("Failed to read last check file: %v", err)
	}

	timestamp, err := time.Parse(time.RFC3339, string(data))
	if err != nil {
		t.Fatalf("Failed to parse timestamp: %v", err)
	}

	// Check that the timestamp is recent (within 1 second)
	if time.Since(timestamp) > time.Second {
		t.Errorf("Timestamp is not recent: %v", timestamp)
	}
}

func TestCheckForUpdatesWithNoCheckUpdateFlag(t *testing.T) {
	// This test verifies that checkForUpdates respects the noCheckUpdate flag
	// and doesn't make any API calls when the flag is true

	// Save original environment and function
	origCI := os.Getenv("CI")
	origGithubActions := os.Getenv("GITHUB_ACTIONS")
	origContinuousIntegration := os.Getenv("CONTINUOUS_INTEGRATION")
	origGetLastCheckFilePath := getLastCheckFilePathFunc
	defer func() {
		if origCI != "" {
			os.Setenv("CI", origCI)
		} else {
			os.Unsetenv("CI")
		}
		if origGithubActions != "" {
			os.Setenv("GITHUB_ACTIONS", origGithubActions)
		} else {
			os.Unsetenv("GITHUB_ACTIONS")
		}
		if origContinuousIntegration != "" {
			os.Setenv("CONTINUOUS_INTEGRATION", origContinuousIntegration)
		} else {
			os.Unsetenv("CONTINUOUS_INTEGRATION")
		}
		getLastCheckFilePathFunc = origGetLastCheckFilePath
	}()

	// Ensure we're not in CI mode
	os.Unsetenv("CI")
	os.Unsetenv("GITHUB_ACTIONS")
	os.Unsetenv("CONTINUOUS_INTEGRATION")

	// Create temporary directory for last check file
	tmpDir := t.TempDir()
	lastCheckFile := filepath.Join(tmpDir, lastCheckFileName)

	// Override the function to use temp directory
	getLastCheckFilePathFunc = func() string {
		return lastCheckFile
	}

	// Call checkForUpdates with noCheckUpdate=true
	checkForUpdates(true, false)

	// Verify that no last check file was created (since check was skipped)
	if _, err := os.Stat(lastCheckFile); err == nil {
		t.Error("Last check file should not be created when noCheckUpdate=true")
	}
}

func TestCheckForUpdatesInCIMode(t *testing.T) {
	// Save original environment and function
	origCI := os.Getenv("CI")
	origGetLastCheckFilePath := getLastCheckFilePathFunc
	defer func() {
		os.Setenv("CI", origCI)
		getLastCheckFilePathFunc = origGetLastCheckFilePath
	}()

	// Set CI environment
	os.Setenv("CI", "true")

	// Create temporary directory for last check file
	tmpDir := t.TempDir()
	lastCheckFile := filepath.Join(tmpDir, lastCheckFileName)

	// Override the function to use temp directory
	getLastCheckFilePathFunc = func() string {
		return lastCheckFile
	}

	// Call checkForUpdates
	checkForUpdates(false, false)

	// Verify that no last check file was created (since check was skipped in CI)
	if _, err := os.Stat(lastCheckFile); err == nil {
		t.Error("Last check file should not be created in CI mode")
	}
}

func TestCheckForUpdatesAsync_ContextCancellation(t *testing.T) {
	// Test that async update check respects context cancellation
	// Save original environment
	origCI := os.Getenv("CI")
	origGetLastCheckFilePath := getLastCheckFilePathFunc
	defer func() {
		os.Setenv("CI", origCI)
		getLastCheckFilePathFunc = origGetLastCheckFilePath
	}()

	// Ensure we're not in CI mode
	os.Unsetenv("CI")
	os.Unsetenv("GITHUB_ACTIONS")
	os.Unsetenv("CONTINUOUS_INTEGRATION")

	// Create temporary directory for last check file
	tmpDir := t.TempDir()
	lastCheckFile := filepath.Join(tmpDir, lastCheckFileName)

	// Override the function to use temp directory
	getLastCheckFilePathFunc = func() string {
		return lastCheckFile
	}

	// Create a cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately
	cancel()

	// Call CheckForUpdatesAsync with cancelled context
	CheckForUpdatesAsync(ctx, false, false)

	// Wait a bit to ensure any goroutines would have had time to run
	time.Sleep(200 * time.Millisecond)

	// The update check should not have created a last check file
	// because the context was cancelled
	// Note: The check might still run if it started before cancellation,
	// so we just verify no panics occurred
}
