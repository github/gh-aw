//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestValidateGitHubToolsAgainstToolsets(t *testing.T) {
	tests := []struct {
		name            string
		allowedTools    []string
		enabledToolsets []string
		expectError     bool
		errorContains   []string
	}{
		{
			name:            "No allowed tools - validation passes",
			allowedTools:    []string{},
			enabledToolsets: []string{"repos"},
			expectError:     false,
		},
		{
			name:            "Nil allowed tools - validation passes",
			allowedTools:    nil,
			enabledToolsets: []string{"repos"},
			expectError:     false,
		},
		{
			name:            "All tools have corresponding toolsets enabled",
			allowedTools:    []string{"get_repository", "list_commits", "get_file_contents"},
			enabledToolsets: []string{"repos"},
			expectError:     false,
		},
		{
			name:            "Mix of toolsets all enabled",
			allowedTools:    []string{"get_repository", "list_issues", "pull_request_read"},
			enabledToolsets: []string{"repos", "issues", "pull_requests"},
			expectError:     false,
		},
		{
			name:            "Default toolset includes required toolset",
			allowedTools:    []string{"get_repository", "list_issues"},
			enabledToolsets: []string{"default"}, // Default expands to include repos and issues
			expectError:     false,
		},
		{
			name:            "All toolset enables everything",
			allowedTools:    []string{"get_repository", "list_issues", "list_workflows", "create_gist"},
			enabledToolsets: []string{"all"},
			expectError:     false,
		},
		{
			name:            "Missing single toolset",
			allowedTools:    []string{"get_repository", "list_issues"},
			enabledToolsets: []string{"repos"}, // issues toolset missing
			expectError:     true,
			errorContains:   []string{"issues", "list_issues"},
		},
		{
			name:            "Missing multiple toolsets",
			allowedTools:    []string{"get_repository", "list_issues", "list_workflows"},
			enabledToolsets: []string{"repos"}, // issues and actions missing
			expectError:     true,
			errorContains:   []string{"issues", "actions", "list_issues", "list_workflows"},
		},
		{
			name:            "Missing toolset for pull request tools",
			allowedTools:    []string{"search_pull_requests", "pull_request_read", "list_pull_requests"},
			enabledToolsets: []string{"repos", "issues"}, // pull_requests missing
			expectError:     true,
			errorContains:   []string{"pull_requests", "search_pull_requests"},
		},
		{
			name:            "Unknown tool is ignored",
			allowedTools:    []string{"get_repository", "unknown_tool_xyz"},
			enabledToolsets: []string{"repos"},
			expectError:     true,
			errorContains:   []string{"Unknown GitHub tool", "unknown_tool_xyz"},
		},
		{
			name:            "Mix of known and unknown tools",
			allowedTools:    []string{"get_repository", "unknown_tool", "list_issues"},
			enabledToolsets: []string{"repos"}, // issues missing
			expectError:     true,
			errorContains:   []string{"Unknown GitHub tool", "unknown_tool"},
		},
		{
			name:            "Actions toolset tools",
			allowedTools:    []string{"list_workflows", "get_workflow_run", "download_workflow_run_artifact"},
			enabledToolsets: []string{"actions"},
			expectError:     false,
		},
		{
			name:            "Actions toolset missing",
			allowedTools:    []string{"list_workflows", "get_workflow_run"},
			enabledToolsets: []string{"repos"},
			expectError:     true,
			errorContains:   []string{"actions", "list_workflows", "get_workflow_run"},
		},
		{
			name:            "Discussions and gists toolsets",
			allowedTools:    []string{"create_discussion", "create_gist"},
			enabledToolsets: []string{"discussions", "gists"},
			expectError:     false,
		},
		{
			name:            "Security-related toolsets",
			allowedTools:    []string{"list_code_scanning_alerts", "list_secret_scanning_alerts"},
			enabledToolsets: []string{"code_security", "secret_protection"},
			expectError:     false,
		},
		{
			name:            "Users and context toolsets",
			allowedTools:    []string{"get_user", "get_me", "get_teams"},
			enabledToolsets: []string{"users", "context"},
			expectError:     false,
		},
		{
			name:            "Search toolset",
			allowedTools:    []string{"search_repositories", "search_users"},
			enabledToolsets: []string{"search"},
			expectError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Expand special toolsets (default, all) for testing
			expandedToolsets := expandToolsetsForTesting(tt.enabledToolsets)

			err := ValidateGitHubToolsAgainstToolsets(tt.allowedTools, expandedToolsets)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
					return
				}

				errMsg := err.Error()
				for _, expectedSubstr := range tt.errorContains {
					if !strings.Contains(errMsg, expectedSubstr) {
						t.Errorf("Expected error to contain %q, but it didn't.\nError: %s", expectedSubstr, errMsg)
					}
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

func TestGitHubToolsetValidationError_Error(t *testing.T) {
	tests := []struct {
		name            string
		missingToolsets map[string][]string
		expectedParts   []string
	}{
		{
			name: "Single missing toolset with single tool",
			missingToolsets: map[string][]string{
				"actions": {"list_workflows"},
			},
			expectedParts: []string{
				"ERROR",
				"actions",
				"list_workflows",
				"Suggested fix",
			},
		},
		{
			name: "Single missing toolset with multiple tools",
			missingToolsets: map[string][]string{
				"pull_requests": {"search_pull_requests", "pull_request_read", "list_pull_requests"},
			},
			expectedParts: []string{
				"ERROR",
				"pull_requests",
				"search_pull_requests",
				"pull_request_read",
				"list_pull_requests",
			},
		},
		{
			name: "Multiple missing toolsets",
			missingToolsets: map[string][]string{
				"issues":  {"list_issues", "create_issue"},
				"actions": {"list_workflows"},
			},
			expectedParts: []string{
				"ERROR",
				"actions",
				"issues",
				"list_workflows",
				"list_issues",
				"create_issue",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewGitHubToolsetValidationError(tt.missingToolsets)
			errMsg := err.Error()

			for _, expectedPart := range tt.expectedParts {
				if !strings.Contains(errMsg, expectedPart) {
					t.Errorf("Expected error message to contain %q, but it didn't.\nError: %s", expectedPart, errMsg)
				}
			}

			// Verify it's a multi-line error with helpful formatting
			if !strings.Contains(errMsg, "\n") {
				t.Error("Expected multi-line error message")
			}
		})
	}
}

func TestGitHubToolToToolsetMap_Completeness(t *testing.T) {
	// Verify that the map contains entries for all expected tool categories
	expectedToolsets := []string{
		"context", "repos", "issues", "pull_requests", "actions",
		"code_security", "discussions", "gists", "labels",
		"notifications", "orgs", "users", "search", "secret_protection",
	}

	foundToolsets := make(map[string]bool)
	for _, toolset := range GitHubToolToToolsetMap {
		foundToolsets[toolset] = true
	}

	for _, expectedToolset := range expectedToolsets {
		if !foundToolsets[expectedToolset] {
			t.Errorf("Expected to find tools for toolset %q in GitHubToolToToolsetMap", expectedToolset)
		}
	}
}

func TestGitHubToolToToolsetMap_ConsistencyWithDocumentation(t *testing.T) {
	// Sample of tools that should be in the map based on documentation
	expectedMappings := map[string]string{
		"get_me":                      "users",
		"get_repository":              "repos",
		"get_file_contents":           "repos",
		"list_issues":                 "issues",
		"create_issue":                "issues",
		"pull_request_read":           "pull_requests",
		"search_pull_requests":        "pull_requests",
		"list_workflows":              "actions",
		"get_workflow_run":            "actions",
		"list_code_scanning_alerts":   "code_security",
		"create_discussion":           "discussions",
		"create_gist":                 "gists",
		"get_label":                   "labels",
		"list_notifications":          "notifications",
		"get_organization":            "orgs",
		"get_user":                    "users",
		"search_repositories":         "search",
		"list_secret_scanning_alerts": "secret_protection",
	}

	for tool, expectedToolset := range expectedMappings {
		actualToolset, exists := GitHubToolToToolsetMap[tool]
		if !exists {
			t.Errorf("Expected tool %q to be in GitHubToolToToolsetMap", tool)
			continue
		}
		if actualToolset != expectedToolset {
			t.Errorf("Tool %q: expected toolset %q, got %q", tool, expectedToolset, actualToolset)
		}
	}
}

// expandToolsetsForTesting expands "default" and "all" toolsets for testing purposes
func expandToolsetsForTesting(toolsets []string) []string {
	var expanded []string
	seenToolsets := make(map[string]bool)

	for _, toolset := range toolsets {
		switch toolset {
		case "default":
			// Add default toolsets
			for _, dt := range DefaultGitHubToolsets {
				if !seenToolsets[dt] {
					expanded = append(expanded, dt)
					seenToolsets[dt] = true
				}
			}
		case "all":
			// Add all toolsets from the permissions map
			for t := range toolsetPermissionsMap {
				if !seenToolsets[t] {
					expanded = append(expanded, t)
					seenToolsets[t] = true
				}
			}
		default:
			// Add individual toolset
			if !seenToolsets[toolset] {
				expanded = append(expanded, toolset)
				seenToolsets[toolset] = true
			}
		}
	}

	return expanded
}

// TestValidateGitHubToolsDidYouMean tests the "did you mean" suggestion feature for GitHub tools
func TestValidateGitHubToolsDidYouMean(t *testing.T) {
	tests := []struct {
		name                 string
		invalidTool          string
		expectedSuggestion   string
		shouldHaveSuggestion bool
	}{
		{
			name:                 "typo issue_raed suggests issue_read",
			invalidTool:          "issue_raed",
			expectedSuggestion:   "issue_read",
			shouldHaveSuggestion: true,
		},
		{
			name:                 "typo crate_issue suggests create_issue",
			invalidTool:          "crate_issue",
			expectedSuggestion:   "create_issue",
			shouldHaveSuggestion: true,
		},
		{
			name:                 "typo get_repositry suggests get_repository",
			invalidTool:          "get_repositry",
			expectedSuggestion:   "get_repository",
			shouldHaveSuggestion: true,
		},
		{
			name:                 "typo list_workflos suggests list_workflows",
			invalidTool:          "list_workflos",
			expectedSuggestion:   "list_workflows",
			shouldHaveSuggestion: true,
		},
		{
			name:                 "typo serch_code suggests search_code",
			invalidTool:          "serch_code",
			expectedSuggestion:   "search_code",
			shouldHaveSuggestion: true,
		},
		{
			name:                 "completely wrong tool gets no suggestion",
			invalidTool:          "xyz_abc_123",
			expectedSuggestion:   "",
			shouldHaveSuggestion: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test with the invalid tool
			allowedTools := []string{"get_repository", tt.invalidTool}
			enabledToolsets := []string{"repos"}

			err := ValidateGitHubToolsAgainstToolsets(allowedTools, enabledToolsets)

			if err == nil {
				t.Fatal("Expected validation to fail for unknown tool")
			}

			errorMsg := err.Error()

			// Should mention the unknown tool
			if !strings.Contains(errorMsg, "Unknown GitHub tool") {
				t.Errorf("Expected 'Unknown GitHub tool' in error message, got: %s", errorMsg)
			}

			if !strings.Contains(errorMsg, tt.invalidTool) {
				t.Errorf("Expected invalid tool '%s' in error message, got: %s",
					tt.invalidTool, errorMsg)
			}

			if tt.shouldHaveSuggestion {
				// Should have "Did you mean:" suggestion
				if !strings.Contains(errorMsg, "Did you mean:") {
					t.Errorf("Expected 'Did you mean:' in error message, got: %s", errorMsg)
				}

				if !strings.Contains(errorMsg, tt.expectedSuggestion) {
					t.Errorf("Expected suggestion '%s' in error message, got: %s",
						tt.expectedSuggestion, errorMsg)
				}
			} else {
				// Should NOT have "Did you mean:" suggestion
				if strings.Contains(errorMsg, "Did you mean:") {
					t.Errorf("Should not suggest anything for '%s', but got: %s",
						tt.invalidTool, errorMsg)
				}
			}

			// All errors should list some valid tools
			if !strings.Contains(errorMsg, "Valid GitHub tools") {
				t.Errorf("Error should list valid GitHub tools, got: %s", errorMsg)
			}
		})
	}
}

// TestValidateGitHubToolsMultipleUnknown tests error message when multiple unknown tools are used
func TestValidateGitHubToolsMultipleUnknown(t *testing.T) {
	allowedTools := []string{"get_repository", "issue_raed", "crate_issue", "unknown_xyz"}
	enabledToolsets := []string{"repos", "issues"}

	err := ValidateGitHubToolsAgainstToolsets(allowedTools, enabledToolsets)

	if err == nil {
		t.Fatal("Expected validation to fail for unknown tools")
	}

	errorMsg := err.Error()

	// Should mention all unknown tools
	if !strings.Contains(errorMsg, "issue_raed") {
		t.Errorf("Expected 'issue_raed' in error message, got: %s", errorMsg)
	}
	if !strings.Contains(errorMsg, "crate_issue") {
		t.Errorf("Expected 'crate_issue' in error message, got: %s", errorMsg)
	}
	if !strings.Contains(errorMsg, "unknown_xyz") {
		t.Errorf("Expected 'unknown_xyz' in error message, got: %s", errorMsg)
	}

	// Should have suggestions section
	if !strings.Contains(errorMsg, "Did you mean:") {
		t.Errorf("Expected 'Did you mean:' in error message, got: %s", errorMsg)
	}
}
