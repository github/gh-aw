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
			result := resolveGitHubToken(tt.customToken)
			if result != tt.expected {
				t.Errorf("resolveGitHubToken() = %q, want %q", result, tt.expected)
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
			result := resolveSafeOutputGitHubToken(tt.customToken)
			if result != tt.expected {
				t.Errorf("resolveSafeOutputGitHubToken() = %q, want %q", result, tt.expected)
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

func TestGetEffectiveAgentGitHubTokenSource(t *testing.T) {
	if got := getEffectiveCopilotCodingAgentGitHubTokenSource(""); got != "${{ secrets.GH_AW_AGENT_TOKEN != '' && 'GH_AW_AGENT_TOKEN' || secrets.GH_AW_GITHUB_TOKEN != '' && 'GH_AW_GITHUB_TOKEN' || secrets.GITHUB_TOKEN != '' && 'GITHUB_TOKEN' || 'unavailable' }}" {
		t.Fatalf("Unexpected default assignment credential source expression: %s", got)
	}
	for _, customToken := range []string{"${{ secrets.PRIVATE_TOKEN_NAME }}", "${{ inputs.token }}", "test-sensitive-value"} {
		if got := getEffectiveCopilotCodingAgentGitHubTokenSource(customToken); got != `"configured"` {
			t.Fatal("Custom assignment credential source must use only the configured category")
		}
	}
}

func TestAgentGitHubTokenAndSourceSharePrecedence(t *testing.T) {
	originalSecrets := copilotCodingAgentGitHubTokenSecrets
	t.Cleanup(func() {
		copilotCodingAgentGitHubTokenSecrets = originalSecrets
	})
	copilotCodingAgentGitHubTokenSecrets = []string{"SECOND_AGENT_TOKEN", "FIRST_AGENT_TOKEN"}

	if got := getEffectiveCopilotCodingAgentGitHubToken(""); got != "${{ secrets.SECOND_AGENT_TOKEN || secrets.FIRST_AGENT_TOKEN }}" {
		t.Fatalf("Assignment token must follow the shared secret order, got %q", got)
	}
	if got := getEffectiveCopilotCodingAgentGitHubTokenSource(""); got != "${{ secrets.SECOND_AGENT_TOKEN != '' && 'SECOND_AGENT_TOKEN' || secrets.FIRST_AGENT_TOKEN != '' && 'FIRST_AGENT_TOKEN' || 'unavailable' }}" {
		t.Fatalf("Assignment credential source must follow the shared secret order, got %q", got)
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
	t.Run("uses global GitHub App token for create-pull-request checkout", func(t *testing.T) {
		safeOutputs := &SafeOutputsConfig{
			GitHubApp: &GitHubAppConfig{
				AppID:      "${{ vars.APP_ID }}",
				PrivateKey: "${{ secrets.APP_KEY }}",
			},
			CreatePullRequests: &CreatePullRequestsConfig{},
		}

		token, isCustom := resolvePRCheckoutToken(safeOutputs, NewCheckoutManager(nil))
		if token != "${{ steps.safe-outputs-app-token.outputs.token }}" {
			t.Fatalf("expected global app token, got %q", token)
		}
		if !isCustom {
			t.Fatalf("expected isCustom=true")
		}
	})

	t.Run("ignore-if-missing global GitHub App falls back to safe-output token", func(t *testing.T) {
		safeOutputs := &SafeOutputsConfig{
			GitHubApp: &GitHubAppConfig{
				AppID:           "${{ vars.APP_ID }}",
				PrivateKey:      "${{ secrets.APP_KEY }}",
				IgnoreIfMissing: true,
			},
			PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
		}

		token, isCustom := resolvePRCheckoutToken(safeOutputs, NewCheckoutManager(nil))
		if token != "${{ steps.safe-outputs-app-token.outputs.token || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}" {
			t.Fatalf("expected app token with safe-output fallback, got %q", token)
		}
		if !isCustom {
			t.Fatalf("expected isCustom=true")
		}
	})

	t.Run("optional global App preserves custom and default token fallbacks", func(t *testing.T) {
		for _, ignoreMissing := range []bool{false, true} {
			safeOutputs := &SafeOutputsConfig{
				GitHubToken: "${{ secrets.SO_TOKEN }}",
				GitHubApp: &GitHubAppConfig{
					AppID:           "${{ vars.APP_ID }}",
					PrivateKey:      "${{ secrets.APP_KEY }}",
					IgnoreIfMissing: ignoreMissing,
				},
			}
			if ignoreMissing {
				safeOutputs.CreatePullRequests = &CreatePullRequestsConfig{}
			}
			token, isCustom := resolvePRCheckoutToken(safeOutputs, NewCheckoutManager(nil))
			expected := "${{ steps.safe-outputs-app-token.outputs.token || secrets.SO_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}"
			if token != expected {
				t.Fatalf("ignore-if-missing=%v: expected complete fallback chain, got %q", ignoreMissing, token)
			}
			if !isCustom {
				t.Fatalf("expected isCustom=true")
			}
		}
	})

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
