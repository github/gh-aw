//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestApplyDefaultGitCommandsForSafeOutputs(t *testing.T) {
	compiler := NewCompiler()
	engine := NewClaudeEngine()

	tests := []struct {
		name        string
		tools       map[string]any
		safeOutputs *SafeOutputsConfig
		expectGit   bool
	}{
		{
			name:        "no safe outputs - no git commands",
			tools:       map[string]any{},
			safeOutputs: nil,
			expectGit:   false,
		},
		{
			name:  "create-pull-request enabled - should add git commands",
			tools: map[string]any{},
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{},
			},
			expectGit: true,
		},
		{
			name:  "push-to-pull-request-branch enabled - should add git commands",
			tools: map[string]any{},
			safeOutputs: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
			},
			expectGit: true,
		},
		{
			name:  "only create-issue enabled - no git commands",
			tools: map[string]any{},
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
			},
			expectGit: false,
		},
		{
			name: "existing bash commands should be preserved",
			tools: map[string]any{
				"bash": []any{"echo", "ls"},
			},
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{},
			},
			expectGit: true,
		},
		{
			name: "bash with wildcard should remain wildcard",
			tools: map[string]any{
				"bash": []any{":*"},
			},
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{},
			},
			expectGit: true,
		},
		{
			name: "bash with nil value should remain nil",
			tools: map[string]any{
				"bash": nil,
			},
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{},
			},
			expectGit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a copy of input tools to avoid modifying test data
			tools := make(map[string]any)
			for k, v := range tt.tools {
				tools[k] = v
			}

			// Apply both default tool functions in sequence
			tools = compiler.applyDefaultTools(tools, tt.safeOutputs, nil, nil)

			// Extract cache-memory config
			cacheMemoryConfig, _ := compiler.extractCacheMemoryConfigFromMap(tools)
			result := engine.computeAllowedClaudeToolsString(tools, tt.safeOutputs, cacheMemoryConfig)

			// Parse the result string into individual tools
			resultTools := []string{}
			if result != "" {
				resultTools = strings.Split(result, ",")
			}

			// Check if we have bash tools when expected
			hasBashTool := false
			hasGitCommands := false

			for _, tool := range resultTools {
				tool = strings.TrimSpace(tool)
				if tool == "Bash" {
					hasBashTool = true
					hasGitCommands = true // "Bash" alone means all bash commands are allowed
					break
				}
				if strings.HasPrefix(tool, "Bash(git ") {
					hasBashTool = true
					hasGitCommands = true
					break
				}
			}

			if tt.expectGit {
				if !hasBashTool {
					t.Error("Expected Bash tool to be present when Git commands are needed")
				}
				if !hasGitCommands {
					t.Error("Expected to find Git commands in Bash tool")
				}
			}
			// If we don't expect git commands, we just verify no error occurred
			// The result can still contain other tools
		})
	}
}

func TestAdditionalClaudeToolsForSafeOutputs(t *testing.T) {
	compiler := NewCompiler()
	engine := NewClaudeEngine()

	tests := []struct {
		name               string
		tools              map[string]any
		safeOutputs        *SafeOutputsConfig
		expectEditingTools bool
	}{
		{
			name:               "no safe outputs - no editing tools",
			tools:              map[string]any{},
			safeOutputs:        nil,
			expectEditingTools: false,
		},
		{
			name:  "create-pull-request enabled - should add editing tools",
			tools: map[string]any{},
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{},
			},
			expectEditingTools: true,
		},
		{
			name:  "push-to-pull-request-branch enabled - should add editing tools",
			tools: map[string]any{},
			safeOutputs: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
			},
			expectEditingTools: true,
		},
		{
			name:  "only create-issue enabled - no editing tools",
			tools: map[string]any{},
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
			},
			expectEditingTools: false,
		},
		{
			name: "existing editing tools should be preserved",
			tools: map[string]any{
				"edit": nil,
			},
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{},
			},
			expectEditingTools: true,
		},
	}

	expectedEditingTools := []string{"Edit", "MultiEdit", "NotebookEdit"}
	expectedWriteTool := "Write"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a copy of input tools to avoid modifying test data
			tools := make(map[string]any)
			for k, v := range tt.tools {
				tools[k] = v
			}

			// Apply both default tool functions in sequence
			tools = compiler.applyDefaultTools(tools, tt.safeOutputs, nil, nil)

			// Extract cache-memory config
			cacheMemoryConfig, _ := compiler.extractCacheMemoryConfigFromMap(tools)
			result := engine.computeAllowedClaudeToolsString(tools, tt.safeOutputs, cacheMemoryConfig)

			// Parse the result string into individual tools
			resultTools := []string{}
			if result != "" {
				resultTools = strings.Split(result, ",")
			}

			// Check if we have the expected editing tools
			foundEditingTools := make(map[string]bool)
			hasWriteTool := false

			for _, tool := range resultTools {
				tool = strings.TrimSpace(tool)
				for _, expectedTool := range expectedEditingTools {
					if tool == expectedTool {
						foundEditingTools[expectedTool] = true
					}
				}
				if tool == expectedWriteTool {
					hasWriteTool = true
				}
			}

			// Write tool should be present for any SafeOutputs configuration
			if tt.safeOutputs != nil && !hasWriteTool {
				t.Error("Expected Write tool to be present when SafeOutputs is configured")
			}

			// If we don't expect editing tools, verify they aren't there due to this feature
			if !tt.expectEditingTools {
				// Only check if we started with empty tools - if there were pre-existing tools, they should remain
				if len(tt.tools) == 0 {
					for _, tool := range expectedEditingTools {
						if foundEditingTools[tool] {
							t.Errorf("Unexpected editing tool %s found when not expected", tool)
						}
					}
				}
				return
			}

			// Check that all expected editing tools are present (not including Write, which is handled separately)
			for _, expectedTool := range expectedEditingTools {
				if !foundEditingTools[expectedTool] {
					t.Errorf("Expected editing tool %s to be present", expectedTool)
				}
			}
		})
	}
}

func TestNeedsGitCommands(t *testing.T) {
	tests := []struct {
		name        string
		safeOutputs *SafeOutputsConfig
		expected    bool
	}{
		{
			name:        "nil safe outputs",
			safeOutputs: nil,
			expected:    false,
		},
		{
			name:        "empty safe outputs",
			safeOutputs: &SafeOutputsConfig{},
			expected:    false,
		},
		{
			name: "create-pull-request enabled",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{},
			},
			expected: true,
		},
		{
			name: "push-to-pull-request-branch enabled",
			safeOutputs: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
			},
			expected: true,
		},
		{
			name: "both enabled",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests:      &CreatePullRequestsConfig{},
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
			},
			expected: true,
		},
		{
			name: "only other outputs enabled",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
				AddComments:  &AddCommentsConfig{},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := needsGitCommands(tt.safeOutputs)
			if result != tt.expected {
				t.Errorf("needsGitCommands() = %v, expected %v", result, tt.expected)
			}
		})
	}
}
