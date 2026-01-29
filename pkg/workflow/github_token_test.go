//go:build !integration

package workflow

import (
	"testing"
)

func TestGetEffectiveGitHubToken(t *testing.T) {
	tests := []struct {
		name          string
		customToken   string
		toplevelToken string
		expected      string
	}{
		{
			name:          "custom token has highest precedence",
			customToken:   "${{ secrets.CUSTOM_TOKEN }}",
			toplevelToken: "${{ secrets.TOPLEVEL_TOKEN }}",
			expected:      "${{ secrets.CUSTOM_TOKEN }}",
		},
		{
			name:          "toplevel token used when no custom token",
			customToken:   "",
			toplevelToken: "${{ secrets.TOPLEVEL_TOKEN }}",
			expected:      "${{ secrets.TOPLEVEL_TOKEN }}",
		},
		{
			name:          "default fallback includes GH_AW_GITHUB_MCP_SERVER_TOKEN (for MCP and tools)",
			customToken:   "",
			toplevelToken: "",
			expected:      "${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getEffectiveGitHubToken(tt.customToken, tt.toplevelToken)
			if result != tt.expected {
				t.Errorf("getEffectiveGitHubToken() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestGetEffectiveSafeOutputGitHubToken(t *testing.T) {
	tests := []struct {
		name          string
		customToken   string
		toplevelToken string
		expected      string
	}{
		{
			name:          "custom token has highest precedence",
			customToken:   "${{ secrets.CUSTOM_TOKEN }}",
			toplevelToken: "${{ secrets.TOPLEVEL_TOKEN }}",
			expected:      "${{ secrets.CUSTOM_TOKEN }}",
		},
		{
			name:          "toplevel token used when no custom token",
			customToken:   "",
			toplevelToken: "${{ secrets.TOPLEVEL_TOKEN }}",
			expected:      "${{ secrets.TOPLEVEL_TOKEN }}",
		},
		{
			name:          "default fallback includes GH_AW_GITHUB_TOKEN (safe outputs chain)",
			customToken:   "",
			toplevelToken: "",
			expected:      "${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getEffectiveSafeOutputGitHubToken(tt.customToken, tt.toplevelToken)
			if result != tt.expected {
				t.Errorf("getEffectiveSafeOutputGitHubToken() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestGetEffectiveCopilotGitHubToken(t *testing.T) {
	tests := []struct {
		name          string
		customToken   string
		toplevelToken string
		expected      string
	}{
		{
			name:          "custom token has highest precedence",
			customToken:   "${{ secrets.CUSTOM_COPILOT_TOKEN }}",
			toplevelToken: "${{ secrets.TOPLEVEL_TOKEN }}",
			expected:      "${{ secrets.CUSTOM_COPILOT_TOKEN }}",
		},
		{
			name:          "toplevel token used when no custom token",
			customToken:   "",
			toplevelToken: "${{ secrets.TOPLEVEL_TOKEN }}",
			expected:      "${{ secrets.TOPLEVEL_TOKEN }}",
		},
		{
			name:          "default fallback for Copilot includes multiple tokens",
			customToken:   "",
			toplevelToken: "",
			expected:      "${{ secrets.COPILOT_GITHUB_TOKEN || secrets.GH_AW_GITHUB_TOKEN }}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getEffectiveCopilotGitHubToken(tt.customToken, tt.toplevelToken)
			if result != tt.expected {
				t.Errorf("getEffectiveCopilotGitHubToken() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestGetEffectiveAgentGitHubToken(t *testing.T) {
	tests := []struct {
		name          string
		customToken   string
		toplevelToken string
		expected      string
	}{
		{
			name:          "custom token has highest precedence",
			customToken:   "${{ secrets.CUSTOM_AGENT_TOKEN }}",
			toplevelToken: "${{ secrets.TOP_LEVEL_TOKEN }}",
			expected:      "${{ secrets.CUSTOM_AGENT_TOKEN }}",
		},
		{
			name:          "toplevel token when custom is empty",
			customToken:   "",
			toplevelToken: "${{ secrets.TOP_LEVEL_TOKEN }}",
			expected:      "${{ secrets.TOP_LEVEL_TOKEN }}",
		},
		{
			name:          "default fallback chain for agent operations",
			customToken:   "",
			toplevelToken: "",
			expected:      "${{ secrets.GH_AW_AGENT_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getEffectiveAgentGitHubToken(tt.customToken, tt.toplevelToken)
			if result != tt.expected {
				t.Errorf("getEffectiveAgentGitHubToken() = %q, want %q", result, tt.expected)
			}
		})
	}
}
