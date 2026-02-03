//go:build integration

package workflow

import (
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

// TestConstantsIntegration verifies that constants can be accessed from the workflow package
func TestConstantsIntegration(t *testing.T) {
	// Test that DefaultGitHubTools constant is accessible and not empty
	if len(constants.DefaultGitHubTools) == 0 {
		t.Error("DefaultGitHubTools constant should not be empty")
	}

	// Test that it contains expected tools
	expectedTools := []string{
		"issue_read",
		"list_issues",
		"search_repositories",
		"get_commit",
		"get_file_contents",
	}

	toolsMap := make(map[string]bool)
	for _, tool := range constants.DefaultGitHubTools {
		toolsMap[tool] = true
	}

	for _, expectedTool := range expectedTools {
		if !toolsMap[expectedTool] {
			t.Errorf("Expected tool '%s' not found in DefaultGitHubTools", expectedTool)
		}
	}
}

// TestClaudeCanAccessGitHubTools demonstrates that Claude engine can access the GitHub tools constant
func TestClaudeCanAccessGitHubTools(t *testing.T) {
	engine := NewClaudeEngine()
	if engine == nil {
		t.Fatal("Failed to create Claude engine")
	}

	// Demonstrate that Claude can access the constant
	gitHubTools := constants.DefaultGitHubTools
	if len(gitHubTools) == 0 {
		t.Error("Claude engine should be able to access DefaultGitHubTools constant")
	}

	// Verify specific tools that would be useful for Claude
	toolsMap := make(map[string]bool)
	for _, tool := range gitHubTools {
		toolsMap[tool] = true
	}

	claudeRelevantTools := []string{
		"issue_read",
		"pull_request_read",
		"search_code",
		"list_commits",
	}

	for _, tool := range claudeRelevantTools {
		if !toolsMap[tool] {
			t.Errorf("Claude-relevant tool '%s' not found in DefaultGitHubTools", tool)
		}
	}
}
