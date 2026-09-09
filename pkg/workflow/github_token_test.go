//go:build !integration

package workflow

import (
	"testing"
)

func TestGetEffectiveGitHubToken(t *testing.T) {
	tests := []struct {
		name        string
		customToken string
		expected    string
	}{
		{
			name:        "custom token has highest precedence",
			customToken: "${{ secrets.CUSTOM_TOKEN }}",
			expected:    "${{ secrets.CUSTOM_TOKEN }}",
		},
		{
			name:        "default fallback includes GH_AW_GITHUB_MCP_SERVER_TOKEN (for MCP and tools)",
			customToken: "",
			expected:    "${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getEffectiveGitHubToken(tt.customToken)
			if result != tt.expected {
				t.Errorf("getEffectiveGitHubToken() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestGetEffectiveSafeOutputGitHubToken(t *testing.T) {
	tests := []struct {
		name        string
		customToken string
		expected    string
	}{
		{
			name:        "custom token has highest precedence",
			customToken: "${{ secrets.CUSTOM_TOKEN }}",
			expected:    "${{ secrets.CUSTOM_TOKEN }}",
		},
		{
			name:        "default fallback includes GH_AW_GITHUB_TOKEN (safe outputs chain)",
			customToken: "",
			expected:    "${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getEffectiveSafeOutputGitHubToken(tt.customToken)
			if result != tt.expected {
				t.Errorf("getEffectiveSafeOutputGitHubToken() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestGetEffectiveCopilotGitHubToken(t *testing.T) {
	tests := []struct {
		name        string
		customToken string
		expected    string
	}{
		{
			name:        "custom token has highest precedence",
			customToken: "${{ secrets.CUSTOM_COPILOT_TOKEN }}",
			expected:    "${{ secrets.CUSTOM_COPILOT_TOKEN }}",
		},
		{
			name:        "default fallback for Copilot",
			customToken: "",
			expected:    "${{ secrets.COPILOT_GITHUB_TOKEN }}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getEffectiveCopilotRequestsToken(tt.customToken)
			if result != tt.expected {
				t.Errorf("getEffectiveCopilotRequestsToken() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestGetEffectiveAgentGitHubToken(t *testing.T) {
	tests := []struct {
		name        string
		customToken string
		expected    string
	}{
		{
			name:        "custom token has highest precedence",
			customToken: "${{ secrets.CUSTOM_AGENT_TOKEN }}",
			expected:    "${{ secrets.CUSTOM_AGENT_TOKEN }}",
		},
		{
			name:        "default fallback chain for agent operations",
			customToken: "",
			expected:    "${{ secrets.GH_AW_AGENT_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getEffectiveCopilotCodingAgentGitHubToken(tt.customToken)
			if result != tt.expected {
				t.Errorf("getEffectiveCopilotCodingAgentGitHubToken() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestGetEffectiveCITriggerGitHubToken(t *testing.T) {
	tests := []struct {
		name        string
		customToken string
		expected    string
	}{
		{
			name:        "custom token has highest precedence",
			customToken: "${{ secrets.CUSTOM_CI_TOKEN }}",
			expected:    "${{ secrets.CUSTOM_CI_TOKEN }}",
		},
		{
			name:        "default fallback for CI trigger operations",
			customToken: "",
			expected:    "${{ secrets.GH_AW_CI_TRIGGER_TOKEN }}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getEffectiveCITriggerGitHubToken(tt.customToken)
			if result != tt.expected {
				t.Errorf("getEffectiveCITriggerGitHubToken() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestCombineTokenExpressions(t *testing.T) {
	tests := []struct {
		name     string
		primary  string
		fallback string
		expected string
	}{
		{
			name:     "combines wrapped expressions",
			primary:  "${{ steps.safe-outputs-app-token.outputs.token }}",
			fallback: "${{ secrets.GITHUB_TOKEN }}",
			expected: "${{ steps.safe-outputs-app-token.outputs.token || secrets.GITHUB_TOKEN }}",
		},
		{
			name:     "trims whitespace and unwrapped expressions",
			primary:  " steps.safe-outputs-app-token.outputs.token ",
			fallback: " secrets.GITHUB_TOKEN ",
			expected: "${{ steps.safe-outputs-app-token.outputs.token || secrets.GITHUB_TOKEN }}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := combineTokenExpressions(tt.primary, tt.fallback)
			if result != tt.expected {
				t.Errorf("combineTokenExpressions() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestResolvePRCheckoutToken(t *testing.T) {
	t.Run("uses checkout safe-output app token when target-repo matches", func(t *testing.T) {
		safeOutputs := &SafeOutputsConfig{
			CreatePullRequests: &CreatePullRequestsConfig{
				TargetRepoSlug: "owner/target",
			},
		}
		checkoutMgr := NewCheckoutManager([]*CheckoutConfig{
			{
				Repository: "owner/target",
				SafeOutputGitHubApp: &GitHubAppConfig{
					AppID:      "${{ vars.APP_ID }}",
					PrivateKey: "${{ secrets.APP_KEY }}",
				},
			},
		})

		token, isCustom := resolvePRCheckoutToken(safeOutputs, checkoutMgr)
		if token != "${{ steps.checkout-safe-output-app-token-0.outputs.token }}" {
			t.Fatalf("expected checkout safe-output app token, got %q", token)
		}
		if !isCustom {
			t.Fatalf("expected isCustom=true")
		}
	})

	t.Run("falls back to previous precedence when no checkout safe-output app exists", func(t *testing.T) {
		safeOutputs := &SafeOutputsConfig{
			CreatePullRequests: &CreatePullRequestsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					GitHubToken: "${{ secrets.PR_PAT }}",
				},
			},
			GitHubToken: "${{ secrets.SAFE_OUTPUTS_PAT }}",
		}

		token, isCustom := resolvePRCheckoutToken(safeOutputs, NewCheckoutManager(nil))
		if token != "${{ secrets.PR_PAT }}" {
			t.Fatalf("expected per-config PAT, got %q", token)
		}
		if !isCustom {
			t.Fatalf("expected isCustom=true")
		}
	})

	t.Run("per-config PAT takes precedence over checkout safe-output app token", func(t *testing.T) {
		safeOutputs := &SafeOutputsConfig{
			CreatePullRequests: &CreatePullRequestsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					GitHubToken: "${{ secrets.PR_PAT }}",
				},
				TargetRepoSlug: "owner/target",
			},
		}
		checkoutMgr := NewCheckoutManager([]*CheckoutConfig{
			{
				Repository: "owner/target",
				SafeOutputGitHubApp: &GitHubAppConfig{
					AppID:      "${{ vars.APP_ID }}",
					PrivateKey: "${{ secrets.APP_KEY }}",
				},
			},
		})

		token, isCustom := resolvePRCheckoutToken(safeOutputs, checkoutMgr)
		if token != "${{ secrets.PR_PAT }}" {
			t.Fatalf("expected per-config PAT token, got %q", token)
		}
		if !isCustom {
			t.Fatalf("expected isCustom=true")
		}
	})
}
