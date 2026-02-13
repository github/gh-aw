//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGitHubToolsetInferenceEngine_AllToolsets tests inference for ALL toolsets
// defined in github_toolsets_permissions.json. This ensures the inference engine
// correctly handles every toolset configuration.
func TestGitHubToolsetInferenceEngine_AllToolsets(t *testing.T) {
	engine := NewGitHubToolsetInferenceEngine()
	allToolsets := engine.GetAllToolsets()

	require.NotEmpty(t, allToolsets, "Should have toolsets from JSON")

	// For each toolset, test that it can be inferred when all its permissions are granted
	for _, toolset := range allToolsets {
		t.Run("toolset_"+toolset, func(t *testing.T) {
			perms := engine.GetToolsetPermissions(toolset)
			require.NotNil(t, perms, "Toolset %s should have permissions defined", toolset)

			// Grant all required read permissions
			permissions := NewPermissions()
			for _, scope := range perms.ReadPermissions {
				permissions.Set(scope, PermissionRead)
			}

			// Test read-only mode
			compatibleReadOnly := engine.InferFromToolsets(permissions, []string{toolset}, true)
			assert.Contains(t, compatibleReadOnly, toolset,
				"Toolset %s should be compatible in read-only mode when all read permissions granted", toolset)

			// If toolset has write permissions, test write mode
			if len(perms.WritePermissions) > 0 {
				// Should NOT be compatible in write mode with only read permissions
				compatibleWriteWithReadOnly := engine.InferFromToolsets(permissions, []string{toolset}, false)
				if len(perms.ReadPermissions) > 0 || len(perms.WritePermissions) > 0 {
					// Toolsets with permission requirements should not be compatible without write permissions
					assert.NotContains(t, compatibleWriteWithReadOnly, toolset,
						"Toolset %s should NOT be compatible in write mode without write permissions", toolset)
				}

				// Grant write permissions
				for _, scope := range perms.WritePermissions {
					permissions.Set(scope, PermissionWrite)
				}

				// Now should be compatible in write mode
				compatibleWrite := engine.InferFromToolsets(permissions, []string{toolset}, false)
				assert.Contains(t, compatibleWrite, toolset,
					"Toolset %s should be compatible in write mode when all write permissions granted", toolset)
			}
		})
	}
}

// TestGitHubToolsetInferenceEngine_DefaultToolsets specifically tests inference
// for the default toolsets used in workflows
func TestGitHubToolsetInferenceEngine_DefaultToolsets(t *testing.T) {
	engine := NewGitHubToolsetInferenceEngine()

	tests := []struct {
		name          string
		permissions   map[PermissionScope]PermissionLevel
		readOnly      bool
		expectedTools []string
		description   string
	}{
		{
			name: "all default permissions - read only",
			permissions: map[PermissionScope]PermissionLevel{
				PermissionContents:     PermissionRead,
				PermissionIssues:       PermissionRead,
				PermissionPullRequests: PermissionRead,
			},
			readOnly:      true,
			expectedTools: []string{"context", "repos", "issues", "pull_requests"},
			description:   "All default toolsets should be compatible when all required read permissions are granted",
		},
		{
			name: "missing pull-requests permission",
			permissions: map[PermissionScope]PermissionLevel{
				PermissionContents: PermissionRead,
				PermissionIssues:   PermissionRead,
				PermissionActions:  PermissionRead,
			},
			readOnly:      true,
			expectedTools: []string{"context", "repos", "issues"},
			description:   "pull_requests toolset should be excluded when pull-requests permission is missing",
		},
		{
			name: "only contents permission",
			permissions: map[PermissionScope]PermissionLevel{
				PermissionContents: PermissionRead,
			},
			readOnly:      true,
			expectedTools: []string{"context", "repos"},
			description:   "Only context and repos toolsets should be compatible with contents permission",
		},
		{
			name: "only issues permission",
			permissions: map[PermissionScope]PermissionLevel{
				PermissionIssues: PermissionRead,
			},
			readOnly:      true,
			expectedTools: []string{"context", "issues"},
			description:   "Only context and issues toolsets should be compatible with issues permission",
		},
		{
			name:          "no permissions",
			permissions:   map[PermissionScope]PermissionLevel{},
			readOnly:      true,
			expectedTools: []string{"context"},
			description:   "Only context toolset should be compatible when no permissions are granted",
		},
		{
			name: "write mode requires write permissions",
			permissions: map[PermissionScope]PermissionLevel{
				PermissionContents: PermissionRead,
				PermissionIssues:   PermissionRead,
			},
			readOnly:      false,
			expectedTools: []string{"context"},
			description:   "Only context toolset is compatible in write mode when only read permissions granted",
		},
		{
			name: "write mode with write permissions",
			permissions: map[PermissionScope]PermissionLevel{
				PermissionContents: PermissionWrite,
				PermissionIssues:   PermissionWrite,
			},
			readOnly:      false,
			expectedTools: []string{"context", "repos", "issues"},
			description:   "repos and issues toolsets compatible in write mode with write permissions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create Permissions from map
			perms := NewPermissions()
			for scope, level := range tt.permissions {
				perms.Set(scope, level)
			}

			result := engine.InferFromDefaults(perms, tt.readOnly)

			assert.Equal(t, tt.expectedTools, result, tt.description)
		})
	}
}

// TestGitHubToolsetInferenceEngine_NilPermissions tests edge case of nil permissions
func TestGitHubToolsetInferenceEngine_NilPermissions(t *testing.T) {
	engine := NewGitHubToolsetInferenceEngine()
	result := engine.InferFromDefaults(nil, true)
	assert.Empty(t, result, "Should return empty slice for nil permissions")
}

// TestGitHubToolsetInferenceEngine_NonDefaultToolsets tests inference from
// a custom list of toolsets (not just defaults)
func TestGitHubToolsetInferenceEngine_NonDefaultToolsets(t *testing.T) {
	engine := NewGitHubToolsetInferenceEngine()

	tests := []struct {
		name          string
		toolsets      []string
		permissions   map[PermissionScope]PermissionLevel
		readOnly      bool
		expectedTools []string
	}{
		{
			name:     "actions toolset with actions permission",
			toolsets: []string{"actions"},
			permissions: map[PermissionScope]PermissionLevel{
				PermissionActions: PermissionRead,
			},
			readOnly:      true,
			expectedTools: []string{"actions"},
		},
		{
			name:     "actions toolset without permission",
			toolsets: []string{"actions"},
			permissions: map[PermissionScope]PermissionLevel{
				PermissionContents: PermissionRead,
			},
			readOnly:      true,
			expectedTools: nil, // actions not compatible, expect nil slice
		},
		{
			name:     "multiple security toolsets",
			toolsets: []string{"code_security", "dependabot", "secret_protection"},
			permissions: map[PermissionScope]PermissionLevel{
				PermissionSecurityEvents: PermissionRead,
			},
			readOnly:      true,
			expectedTools: []string{"code_security", "dependabot", "secret_protection"},
		},
		{
			name:     "discussions toolset",
			toolsets: []string{"discussions"},
			permissions: map[PermissionScope]PermissionLevel{
				PermissionDiscussions: PermissionRead,
			},
			readOnly:      true,
			expectedTools: []string{"discussions"},
		},
		{
			name:        "toolsets with no permission requirements",
			toolsets:    []string{"context", "gists", "notifications", "search", "stargazers"},
			permissions: map[PermissionScope]PermissionLevel{},
			readOnly:    true,
			// All these toolsets require no permissions
			expectedTools: []string{"context", "gists", "notifications", "search", "stargazers"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			perms := NewPermissions()
			for scope, level := range tt.permissions {
				perms.Set(scope, level)
			}

			result := engine.InferFromToolsets(perms, tt.toolsets, tt.readOnly)
			assert.Equal(t, tt.expectedTools, result)
		})
	}
}

// TestGitHubToolsetInferenceEngine_PermissionLevels tests that the inference
// engine correctly distinguishes between read, write, and none permission levels
func TestGitHubToolsetInferenceEngine_PermissionLevels(t *testing.T) {
	engine := NewGitHubToolsetInferenceEngine()

	tests := []struct {
		name        string
		toolset     string
		permissions map[PermissionScope]PermissionLevel
		readOnly    bool
		shouldMatch bool
	}{
		{
			name:    "repos with read permission in read-only mode",
			toolset: "repos",
			permissions: map[PermissionScope]PermissionLevel{
				PermissionContents: PermissionRead,
			},
			readOnly:    true,
			shouldMatch: true,
		},
		{
			name:    "repos with read permission in write mode",
			toolset: "repos",
			permissions: map[PermissionScope]PermissionLevel{
				PermissionContents: PermissionRead,
			},
			readOnly:    false,
			shouldMatch: false, // needs write permission
		},
		{
			name:    "repos with write permission in write mode",
			toolset: "repos",
			permissions: map[PermissionScope]PermissionLevel{
				PermissionContents: PermissionWrite,
			},
			readOnly:    false,
			shouldMatch: true,
		},
		{
			name:    "repos with none permission",
			toolset: "repos",
			permissions: map[PermissionScope]PermissionLevel{
				PermissionContents: PermissionNone,
			},
			readOnly:    true,
			shouldMatch: false,
		},
		{
			name:    "issues with write in read-only mode",
			toolset: "issues",
			permissions: map[PermissionScope]PermissionLevel{
				PermissionIssues: PermissionWrite,
			},
			readOnly:    true,
			shouldMatch: true, // write satisfies read requirement
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			perms := NewPermissions()
			for scope, level := range tt.permissions {
				perms.Set(scope, level)
			}

			result := engine.InferFromToolsets(perms, []string{tt.toolset}, tt.readOnly)

			if tt.shouldMatch {
				assert.Contains(t, result, tt.toolset, "Should contain %s", tt.toolset)
			} else {
				assert.NotContains(t, result, tt.toolset, "Should NOT contain %s", tt.toolset)
			}
		})
	}
}

// TestGitHubToolsetInferenceEngine_GetToolsetPermissions tests the ability
// to query individual toolset permissions
func TestGitHubToolsetInferenceEngine_GetToolsetPermissions(t *testing.T) {
	engine := NewGitHubToolsetInferenceEngine()

	// Test known toolsets
	reposPerms := engine.GetToolsetPermissions("repos")
	require.NotNil(t, reposPerms)
	assert.Contains(t, reposPerms.ReadPermissions, PermissionContents)
	assert.Contains(t, reposPerms.WritePermissions, PermissionContents)

	issuesPerms := engine.GetToolsetPermissions("issues")
	require.NotNil(t, issuesPerms)
	assert.Contains(t, issuesPerms.ReadPermissions, PermissionIssues)
	assert.Contains(t, issuesPerms.WritePermissions, PermissionIssues)

	contextPerms := engine.GetToolsetPermissions("context")
	require.NotNil(t, contextPerms)
	assert.Empty(t, contextPerms.ReadPermissions, "context should have no read permissions")
	assert.Empty(t, contextPerms.WritePermissions, "context should have no write permissions")

	// Test unknown toolset
	unknownPerms := engine.GetToolsetPermissions("nonexistent-toolset")
	assert.Nil(t, unknownPerms, "Should return nil for unknown toolset")
}

// TestGitHubToolsetInferenceEngine_ToolsetTools tests that toolset tool lists
// are correctly loaded from JSON
func TestGitHubToolsetInferenceEngine_ToolsetTools(t *testing.T) {
	engine := NewGitHubToolsetInferenceEngine()

	tests := []struct {
		toolset      string
		expectedTool string // at least one tool we expect
	}{
		{"repos", "get_file_contents"},
		{"issues", "list_issues"},
		{"pull_requests", "list_pull_requests"},
		{"actions", "get_job_logs"},
		{"code_security", "list_code_scanning_alerts"},
		{"discussions", "list_discussions"},
		{"search", "search_code"},
	}

	for _, tt := range tests {
		t.Run(tt.toolset, func(t *testing.T) {
			perms := engine.GetToolsetPermissions(tt.toolset)
			require.NotNil(t, perms, "Toolset %s should exist", tt.toolset)
			assert.NotEmpty(t, perms.Tools, "Toolset %s should have tools defined", tt.toolset)
			assert.Contains(t, perms.Tools, tt.expectedTool,
				"Toolset %s should include tool %s", tt.toolset, tt.expectedTool)
		})
	}
}

// TestInferCompatibleToolsets_BackwardCompatibility tests that the legacy
// convenience function still works correctly
func TestInferCompatibleToolsets_BackwardCompatibility(t *testing.T) {
	perms := NewPermissions()
	perms.Set(PermissionContents, PermissionRead)
	perms.Set(PermissionIssues, PermissionRead)

	result := InferCompatibleToolsets(perms, true)

	assert.Contains(t, result, "context")
	assert.Contains(t, result, "repos")
	assert.Contains(t, result, "issues")
	assert.NotContains(t, result, "pull_requests")
}

// TestGitHubToolsetInferenceEngine_PartialPermissions tests complex scenarios
// where some toolsets are compatible and others are not
func TestGitHubToolsetInferenceEngine_PartialPermissions(t *testing.T) {
	engine := NewGitHubToolsetInferenceEngine()

	// Grant mixed permissions
	perms := NewPermissions()
	perms.Set(PermissionContents, PermissionRead)
	perms.Set(PermissionActions, PermissionRead)
	perms.Set(PermissionSecurityEvents, PermissionRead)

	// Check what we can infer from all toolsets
	allToolsets := engine.GetAllToolsets()
	compatible := engine.InferFromToolsets(perms, allToolsets, true)

	// Should include toolsets that require these permissions
	assert.Contains(t, compatible, "context")
	assert.Contains(t, compatible, "repos")
	assert.Contains(t, compatible, "actions")
	assert.Contains(t, compatible, "code_security")
	assert.Contains(t, compatible, "dependabot")
	assert.Contains(t, compatible, "secret_protection")

	// Should NOT include toolsets that require other permissions
	assert.NotContains(t, compatible, "issues")
	assert.NotContains(t, compatible, "pull_requests")
	assert.NotContains(t, compatible, "discussions")
}
