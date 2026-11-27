//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// sinkVisibilityExpr aliases the production runtime expression emitted for sink-visibility in guard policies.
const sinkVisibilityExpr = sinkVisibilityRuntimeExpr

// TestDeriveSafeOutputsGuardPolicyFromGitHub tests the guard-policy linking logic
// that generates safeoutputs guard-policies from GitHub guard-policies
func TestDeriveSafeOutputsGuardPolicyFromGitHub(t *testing.T) {
	tests := []struct {
		name             string
		githubTool       map[string]any
		expectedPolicies map[string]any
		expectNil        bool
		description      string
	}{
		{
			name: "single repo pattern",
			githubTool: map[string]any{
				"repos":         "github/gh-aw",
				"min-integrity": "approved",
			},
			expectedPolicies: map[string]any{
				"write-sink": map[string]any{
					"accept":          []string{"private:github/gh-aw"},
					"sink-visibility": sinkVisibilityExpr,
				},
			},
			expectNil:   false,
			description: "Single repo pattern should get private: prefix",
		},
		{
			name: "owner wildcard pattern",
			githubTool: map[string]any{
				"repos":         "github/*",
				"min-integrity": "approved",
			},
			expectedPolicies: map[string]any{
				"write-sink": map[string]any{
					"accept":          []string{"private:github"},
					"sink-visibility": sinkVisibilityExpr,
				},
			},
			expectNil:   false,
			description: "Owner wildcard (github/*) should strip wildcard → private:github",
		},
		{
			name: "repo prefix wildcard pattern",
			githubTool: map[string]any{
				"repos":         "github/gh-aw*",
				"min-integrity": "approved",
			},
			expectedPolicies: map[string]any{
				"write-sink": map[string]any{
					"accept":          []string{"private:github/gh-aw*"},
					"sink-visibility": sinkVisibilityExpr,
				},
			},
			expectNil:   false,
			description: "Repo prefix wildcard should keep as-is with private: prefix",
		},
		{
			name: "repos set to all",
			githubTool: map[string]any{
				"repos":         "all",
				"min-integrity": "approved",
			},
			expectedPolicies: map[string]any{
				"write-sink": map[string]any{
					"accept":          []string{"*"},
					"sink-visibility": sinkVisibilityExpr,
				},
			},
			expectNil:   false,
			description: "repos='all' should return accept=['*'] to allow all safe output operations",
		},
		{
			name: "repos set to public",
			githubTool: map[string]any{
				"repos":         "public",
				"min-integrity": "none",
			},
			expectedPolicies: map[string]any{
				"write-sink": map[string]any{
					"accept":          []string{"*"},
					"sink-visibility": sinkVisibilityExpr,
				},
			},
			expectNil:   false,
			description: "repos='public' should return accept=['*'] to allow all safe output operations",
		},
		{
			name: "repos set to github.repository expression",
			githubTool: map[string]any{
				"allowed-repos": "${{ github.repository }}",
				"min-integrity": "approved",
			},
			expectedPolicies: map[string]any{
				"write-sink": map[string]any{
					"accept":          []string{"private:${{ github.repository }}"},
					"sink-visibility": sinkVisibilityExpr,
				},
			},
			expectNil:   false,
			description: "github.repository expression should map to runtime repository scope",
		},
		{
			name: "duplicate allowed repos as []any",
			githubTool: map[string]any{
				"allowed-repos": []any{"github/*", "microsoft/copilot", "github/*", "microsoft/copilot"},
				"min-integrity": "approved",
			},
			expectedPolicies: map[string]any{
				"write-sink": map[string]any{
					"accept":          []string{"private:github", "private:microsoft/copilot"},
					"sink-visibility": sinkVisibilityExpr,
				},
			},
			description: "Duplicate transformed patterns should be removed in first-seen order",
		},
		{
			name: "duplicate allowed repos as []string",
			githubTool: map[string]any{
				"allowed-repos": []string{"github/gh-aw", "github/copilot*", "github/gh-aw"},
				"min-integrity": "approved",
			},
			expectedPolicies: map[string]any{
				"write-sink": map[string]any{
					"accept":          []string{"private:github/gh-aw", "private:github/copilot*"},
					"sink-visibility": sinkVisibilityExpr,
				},
			},
			description: "Duplicate string patterns should be removed in first-seen order",
		},
		{
			name: "multiple repo patterns as []any",
			githubTool: map[string]any{
				"repos": []any{
					"github/gh-aw*",
					"github/copilot*",
				},
				"min-integrity": "approved",
			},
			expectedPolicies: map[string]any{
				"write-sink": map[string]any{
					"accept": []string{
						"private:github/gh-aw*",
						"private:github/copilot*",
					},
					"sink-visibility": sinkVisibilityExpr,
				},
			},
			expectNil:   false,
			description: "Array of prefix patterns should all get private: prefix",
		},
		{
			name: "multiple repo patterns as []string",
			githubTool: map[string]any{
				"repos": []string{
					"github/gh-aw",
					"github/copilot-cli",
				},
				"min-integrity": "merged",
			},
			expectedPolicies: map[string]any{
				"write-sink": map[string]any{
					"accept": []string{
						"private:github/gh-aw",
						"private:github/copilot-cli",
					},
					"sink-visibility": sinkVisibilityExpr,
				},
			},
			expectNil:   false,
			description: "[]string array should all get private: prefix",
		},
		{
			name: "mixed patterns with owner wildcard",
			githubTool: map[string]any{
				"repos": []string{
					"github/*",
					"microsoft/copilot",
				},
				"min-integrity": "approved",
			},
			expectedPolicies: map[string]any{
				"write-sink": map[string]any{
					"accept": []string{
						"private:github",
						"private:microsoft/copilot",
					},
					"sink-visibility": sinkVisibilityExpr,
				},
			},
			expectNil:   false,
			description: "Owner wildcard (github/*) should transform to private:github, specific repo should keep pattern",
		},
		{
			name: "array with all three pattern types",
			githubTool: map[string]any{
				"repos": []string{
					"github/*",           // owner wildcard
					"microsoft/copilot*", // prefix wildcard
					"google/gemini",      // specific repo
				},
				"min-integrity": "approved",
			},
			expectedPolicies: map[string]any{
				"write-sink": map[string]any{
					"accept": []string{
						"private:github",
						"private:microsoft/copilot*",
						"private:google/gemini",
					},
					"sink-visibility": sinkVisibilityExpr,
				},
			},
			expectNil:   false,
			description: "Array with owner wildcard, prefix wildcard, and specific repo should all transform correctly",
		},
		{
			name: "array with multiple owner wildcards",
			githubTool: map[string]any{
				"repos": []any{
					"github/*",
					"microsoft/*",
					"google/*",
				},
				"min-integrity": "approved",
			},
			expectedPolicies: map[string]any{
				"write-sink": map[string]any{
					"accept": []string{
						"private:github",
						"private:microsoft",
						"private:google",
					},
					"sink-visibility": sinkVisibilityExpr,
				},
			},
			expectNil:   false,
			description: "Multiple owner wildcards should all strip the wildcard suffix",
		},
		{
			name: "no repos configured",
			githubTool: map[string]any{
				"min-integrity": "approved",
			},
			expectedPolicies: map[string]any{
				"write-sink": map[string]any{
					"accept":          []string{"*"},
					"sink-visibility": sinkVisibilityExpr,
				},
			},
			expectNil:   false,
			description: "No repos defaults to all, which means accept=[*] for safeoutputs",
		},
		{
			name: "no guard policy at all",
			githubTool: map[string]any{
				"toolsets": []string{"default"},
			},
			expectNil:   true,
			description: "No guard policy means no guard-policy for safeoutputs",
		},
		{
			name:        "nil github tool",
			githubTool:  nil,
			expectNil:   true,
			description: "nil input should return nil",
		},
		{
			name:        "empty github tool map",
			githubTool:  map[string]any{},
			expectNil:   true,
			description: "empty map input should return nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := deriveSafeOutputsGuardPolicyFromGitHub(tt.githubTool)

			if tt.expectNil {
				assert.Nil(t, result, "Expected nil result for: %s", tt.description)
			} else {
				assert.NotNil(t, result, "Expected non-nil result for: %s", tt.description)
				assert.Equal(t, tt.expectedPolicies, result, "Guard policy mismatch for: %s", tt.description)
			}
		})
	}
}
