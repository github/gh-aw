//go:build !integration

package workflow

import (
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

// TestGitHubToolsModeSeparation verifies that local and remote GitHub tools lists are properly separated
func TestGitHubToolsModeSeparation(t *testing.T) {
	// Verify both lists exist and are not empty
	if len(constants.DefaultGitHubToolsLocal) == 0 {
		t.Error("DefaultGitHubToolsLocal should not be empty")
	}

	if len(constants.DefaultGitHubToolsRemote) == 0 {
		t.Error("DefaultGitHubToolsRemote should not be empty")
	}

	// Verify backward compatibility - DefaultGitHubTools should point to local
	if len(constants.DefaultGitHubTools) == 0 {
		t.Error("DefaultGitHubTools should not be empty (backward compatibility)")
	}

	// Verify DefaultGitHubTools points to the same data as DefaultGitHubToolsLocal
	if len(constants.DefaultGitHubTools) != len(constants.DefaultGitHubToolsLocal) {
		t.Errorf("DefaultGitHubTools should have same length as DefaultGitHubToolsLocal for backward compatibility")
	}

	// Verify they contain expected core tools
	expectedCoreTools := []string{
		"issue_read",
		"list_issues",
		"get_commit",
		"get_file_contents",
		"search_repositories",
	}

	// Check local tools
	localToolsMap := make(map[string]bool)
	for _, tool := range constants.DefaultGitHubToolsLocal {
		localToolsMap[tool] = true
	}

	for _, expectedTool := range expectedCoreTools {
		if !localToolsMap[expectedTool] {
			t.Errorf("Expected core tool '%s' not found in DefaultGitHubToolsLocal", expectedTool)
		}
	}

	// Check remote tools
	remoteToolsMap := make(map[string]bool)
	for _, tool := range constants.DefaultGitHubToolsRemote {
		remoteToolsMap[tool] = true
	}

	for _, expectedTool := range expectedCoreTools {
		if !remoteToolsMap[expectedTool] {
			t.Errorf("Expected core tool '%s' not found in DefaultGitHubToolsRemote", expectedTool)
		}
	}
}

// TestApplyDefaultToolsNoLongerAddsDefaults verifies that applyDefaultTools no longer adds default tools
// The MCP server should use ["*"] to allow all tools instead
func TestApplyDefaultToolsNoLongerAddsDefaults(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name               string
		tools              map[string]any
		expectedHasAllowed bool
	}{
		{
			name: "Local mode (default) - no allowed field added",
			tools: map[string]any{
				"github": map[string]any{},
			},
			expectedHasAllowed: false,
		},
		{
			name: "Explicit local mode - no allowed field added",
			tools: map[string]any{
				"github": map[string]any{
					"mode": "local",
				},
			},
			expectedHasAllowed: false,
		},
		{
			name: "Remote mode - no allowed field added",
			tools: map[string]any{
				"github": map[string]any{
					"mode": "remote",
				},
			},
			expectedHasAllowed: false,
		},
		{
			name: "Explicit allowed tools are preserved",
			tools: map[string]any{
				"github": map[string]any{
					"allowed": []any{"issue_read", "list_issues"},
				},
			},
			expectedHasAllowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compiler.applyDefaultTools(tt.tools, nil, nil, nil)

			// Get the github configuration
			githubConfig, ok := result["github"].(map[string]any)
			if !ok {
				t.Fatal("Expected github configuration to be a map")
			}

			// Check if allowed field exists
			_, hasAllowed := githubConfig["allowed"]
			if hasAllowed != tt.expectedHasAllowed {
				t.Errorf("Expected allowed field presence to be %v, got %v", tt.expectedHasAllowed, hasAllowed)
			}

			// If allowed exists and we expect it, verify the tools are preserved
			if tt.expectedHasAllowed {
				allowed, ok := githubConfig["allowed"].([]any)
				if !ok {
					t.Fatal("Expected allowed to be a slice")
				}
				// Verify the explicitly provided tools are preserved
				if len(allowed) != 2 {
					t.Errorf("Expected 2 tools, got %d", len(allowed))
				}
			}
		})
	}
}
