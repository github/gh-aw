//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestCollectRequiredPermissions(t *testing.T) {
	tests := []struct {
		name     string
		toolsets []string
		readOnly bool
		expected map[PermissionScope]PermissionLevel
	}{
		{
			name:     "Context toolset requires no permissions",
			toolsets: []string{"context"},
			readOnly: false,
			expected: map[PermissionScope]PermissionLevel{},
		},
		{
			name:     "Repos toolset in read-write mode",
			toolsets: []string{"repos"},
			readOnly: false,
			expected: map[PermissionScope]PermissionLevel{
				PermissionContents: PermissionWrite,
			},
		},
		{
			name:     "Repos toolset in read-only mode",
			toolsets: []string{"repos"},
			readOnly: true,
			expected: map[PermissionScope]PermissionLevel{
				PermissionContents: PermissionRead,
			},
		},
		{
			name:     "Issues toolset in read-write mode",
			toolsets: []string{"issues"},
			readOnly: false,
			expected: map[PermissionScope]PermissionLevel{
				PermissionIssues: PermissionWrite,
			},
		},
		{
			name:     "Multiple toolsets",
			toolsets: []string{"repos", "issues", "pull_requests"},
			readOnly: false,
			expected: map[PermissionScope]PermissionLevel{
				PermissionContents:     PermissionWrite,
				PermissionIssues:       PermissionWrite,
				PermissionPullRequests: PermissionWrite,
			},
		},
		{
			name:     "Default toolsets in read-write mode",
			toolsets: DefaultGitHubToolsets,
			readOnly: false,
			expected: map[PermissionScope]PermissionLevel{
				PermissionContents:     PermissionWrite,
				PermissionIssues:       PermissionWrite,
				PermissionPullRequests: PermissionWrite,
			},
		},
		{
			name:     "Actions toolset (read-only)",
			toolsets: []string{"actions"},
			readOnly: false,
			expected: map[PermissionScope]PermissionLevel{
				PermissionActions: PermissionRead,
			},
		},
		{
			name:     "Code security toolset",
			toolsets: []string{"code_security"},
			readOnly: false,
			expected: map[PermissionScope]PermissionLevel{
				PermissionSecurityEvents: PermissionWrite,
			},
		},
		{
			name:     "Discussions toolset",
			toolsets: []string{"discussions"},
			readOnly: false,
			expected: map[PermissionScope]PermissionLevel{
				PermissionDiscussions: PermissionWrite,
			},
		},
		{
			name:     "Projects toolset (requires PAT - no permissions)",
			toolsets: []string{"projects"},
			readOnly: false,
			expected: map[PermissionScope]PermissionLevel{
				// No permissions required - projects require PAT, not GITHUB_TOKEN
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collectRequiredPermissions(tt.toolsets, tt.readOnly)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d permissions, got %d: %v", len(tt.expected), len(result), result)
				return
			}

			for scope, expectedLevel := range tt.expected {
				actualLevel, found := result[scope]
				if !found {
					t.Errorf("Expected permission %s not found in result", scope)
					continue
				}
				if actualLevel != expectedLevel {
					t.Errorf("Permission %s: expected level %s, got %s", scope, expectedLevel, actualLevel)
				}
			}
		})
	}
}

func TestValidatePermissions_MissingPermissions(t *testing.T) {
	tests := []struct {
		name               string
		permissions        *Permissions
		githubToolConfig   *GitHubToolConfig
		expectMissing      map[PermissionScope]PermissionLevel
		expectMissingCount int
		expectHasIssues    bool
	}{
		{
			name:               "No GitHub tool configured",
			permissions:        NewPermissions(),
			githubToolConfig:   nil,
			expectMissing:      map[PermissionScope]PermissionLevel{},
			expectMissingCount: 0,
			expectHasIssues:    false,
		},
		{
			name:        "Default toolsets with no permissions",
			permissions: NewPermissions(),
			githubToolConfig: &GitHubToolConfig{
				Toolset: GitHubToolsets{"default"},
			},
			expectMissingCount: 3, // contents, issues, pull-requests
			expectHasIssues:    true,
		},
		{
			name: "Default toolsets with all required permissions",
			permissions: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
				PermissionContents:     PermissionWrite,
				PermissionIssues:       PermissionWrite,
				PermissionPullRequests: PermissionWrite,
			}),
			githubToolConfig: &GitHubToolConfig{
				Toolset:  GitHubToolsets{"default"},
				ReadOnly: false,
			},
			expectMissingCount: 0,
			expectHasIssues:    false,
		},
		{
			name: "Default toolsets with only read permissions (missing write)",
			permissions: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
				PermissionContents:     PermissionRead,
				PermissionIssues:       PermissionRead,
				PermissionPullRequests: PermissionRead,
			}),
			githubToolConfig: &GitHubToolConfig{
				Toolset:  GitHubToolsets{"default"},
				ReadOnly: false, // Need write permissions
			},
			expectMissingCount: 3, // All need write
			expectHasIssues:    true,
		},
		{
			name: "Read-only mode with read permissions",
			permissions: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
				PermissionContents:     PermissionRead,
				PermissionIssues:       PermissionRead,
				PermissionPullRequests: PermissionRead,
			}),
			githubToolConfig: &GitHubToolConfig{
				Toolset:  GitHubToolsets{"default"},
				ReadOnly: true,
			},
			expectMissingCount: 0,
			expectHasIssues:    false,
		},
		{
			name: "Specific toolsets with partial permissions",
			permissions: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
				PermissionContents: PermissionWrite,
			}),
			githubToolConfig: &GitHubToolConfig{
				Toolset:  GitHubToolsets{"repos", "issues"},
				ReadOnly: false,
			},
			expectMissingCount: 1, // Missing issues: write
			expectHasIssues:    true,
		},
		{
			name: "Actions toolset with read permission",
			permissions: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
				PermissionActions: PermissionRead,
			}),
			githubToolConfig: &GitHubToolConfig{
				Toolset: GitHubToolsets{"actions"},
			},
			expectMissingCount: 0,
			expectHasIssues:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidatePermissions(tt.permissions, tt.githubToolConfig)

			if len(result.MissingPermissions) != tt.expectMissingCount {
				t.Errorf("Expected %d missing permissions, got %d: %v",
					tt.expectMissingCount, len(result.MissingPermissions), result.MissingPermissions)
			}

			if result.HasValidationIssues != tt.expectHasIssues {
				t.Errorf("Expected HasValidationIssues=%v, got %v", tt.expectHasIssues, result.HasValidationIssues)
			}

			if tt.expectMissing != nil {
				for scope, expectedLevel := range tt.expectMissing {
					actualLevel, found := result.MissingPermissions[scope]
					if !found {
						t.Errorf("Expected missing permission %s not found", scope)
						continue
					}
					if actualLevel != expectedLevel {
						t.Errorf("Missing permission %s: expected level %s, got %s", scope, expectedLevel, actualLevel)
					}
				}
			}
		})
	}
}

func TestFormatValidationMessage(t *testing.T) {
	tests := []struct {
		name              string
		result            *PermissionsValidationResult
		strict            bool
		expectContains    []string
		expectNotContains []string
	}{
		{
			name: "No validation issues",
			result: &PermissionsValidationResult{
				HasValidationIssues: false,
			},
			strict:         false,
			expectContains: []string{},
		},
		{
			name: "Missing permissions message",
			result: &PermissionsValidationResult{
				HasValidationIssues: true,
				MissingPermissions: map[PermissionScope]PermissionLevel{
					PermissionContents: PermissionWrite,
					PermissionIssues:   PermissionWrite,
				},
				MissingToolsetDetails: map[string][]PermissionScope{
					"repos":  {PermissionContents},
					"issues": {PermissionIssues},
				},
			},
			strict: false,
			expectContains: []string{
				"Missing required permissions for github toolsets:",
				"contents: write (required by repos)",
				"issues: write (required by issues)",
				"Option 1: Add missing permissions to your workflow frontmatter:",
				"Option 2: Reduce the required toolsets in your workflow:",
			},
			expectNotContains: []string{
				"ERROR:",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message := FormatValidationMessage(tt.result, tt.strict)

			if !tt.result.HasValidationIssues {
				if message != "" {
					t.Errorf("Expected empty message for no issues, got: %s", message)
				}
				return
			}

			for _, expected := range tt.expectContains {
				if !strings.Contains(message, expected) {
					t.Errorf("Expected message to contain %q, got:\n%s", expected, message)
				}
			}

			for _, notExpected := range tt.expectNotContains {
				if strings.Contains(message, notExpected) {
					t.Errorf("Expected message NOT to contain %q, got:\n%s", notExpected, message)
				}
			}
		})
	}
}

func TestToolsetPermissionsMapping(t *testing.T) {
	// Verify that all toolsets are properly defined
	expectedToolsets := []string{
		"context", "repos", "issues", "pull_requests", "actions",
		"code_security", "dependabot", "discussions", "experiments",
		"gists", "labels", "notifications", "orgs", "projects",
		"secret_protection", "security_advisories", "stargazers",
		"users", "search",
	}

	for _, toolset := range expectedToolsets {
		if _, exists := toolsetPermissionsMap[toolset]; !exists {
			t.Errorf("Toolset %q not defined in toolsetPermissionsMap", toolset)
		}
	}

	// Verify that default toolsets are valid
	for _, toolset := range DefaultGitHubToolsets {
		if _, exists := toolsetPermissionsMap[toolset]; !exists {
			t.Errorf("Default toolset %q not defined in toolsetPermissionsMap", toolset)
		}
	}
}

func TestValidatePermissions_ComplexScenarios(t *testing.T) {
	tests := []struct {
		name             string
		permissions      *Permissions
		githubToolConfig *GitHubToolConfig
		expectMsg        []string
	}{
		{
			name:        "Shorthand read-all with default toolsets",
			permissions: NewPermissionsReadAll(),
			githubToolConfig: &GitHubToolConfig{
				Toolset:  GitHubToolsets{"default"},
				ReadOnly: false,
			},
			expectMsg: []string{
				"Missing required permissions for github toolsets:",
				"contents: write",
				"issues: write",
				"pull-requests: write",
			},
		},
		{
			name:        "All: read with discussions toolset",
			permissions: NewPermissionsAllRead(),
			githubToolConfig: &GitHubToolConfig{
				Toolset:  GitHubToolsets{"discussions"},
				ReadOnly: false,
			},
			expectMsg: []string{
				"Missing required permissions for github toolsets:",
				"discussions: write",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidatePermissions(tt.permissions, tt.githubToolConfig)
			message := FormatValidationMessage(result, false)

			for _, expected := range tt.expectMsg {
				if !strings.Contains(message, expected) {
					t.Errorf("Expected message to contain %q, got:\n%s", expected, message)
				}
			}
		})
	}
}
