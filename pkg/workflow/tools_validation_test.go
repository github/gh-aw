//go:build !integration

package workflow

import (
	"fmt"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/semverutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateBashToolConfig(t *testing.T) {
	tests := []struct {
		name        string
		toolsMap    map[string]any
		shouldError bool
		errorMsg    string
	}{
		{
			name:        "nil tools config is valid",
			toolsMap:    nil,
			shouldError: false,
		},
		{
			name:        "no bash tool is valid",
			toolsMap:    map[string]any{"github": nil},
			shouldError: false,
		},
		{
			name:        "bash: true is valid",
			toolsMap:    map[string]any{"bash": true},
			shouldError: false,
		},
		{
			name:        "bash: false is valid",
			toolsMap:    map[string]any{"bash": false},
			shouldError: false,
		},
		{
			name:        "bash with array is valid",
			toolsMap:    map[string]any{"bash": []any{"echo", "ls"}},
			shouldError: false,
		},
		{
			name:        "bash with wildcard is valid",
			toolsMap:    map[string]any{"bash": []any{"*"}},
			shouldError: false,
		},
		{
			name:        "anonymous bash (nil) is invalid",
			toolsMap:    map[string]any{"bash": nil},
			shouldError: true,
			errorMsg:    "anonymous syntax 'bash:' is not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools := NewTools(tt.toolsMap)
			err := validateBashToolConfig(tools, "test-workflow")

			if tt.shouldError {
				require.Error(t, err, "Expected error for %s", tt.name)
				if tt.errorMsg != "" {
					require.ErrorContains(t, err, tt.errorMsg, "Error message should contain expected text")
				}
			} else {
				assert.NoError(t, err, "Expected no error for %s", tt.name)
			}
		})
	}
}

func TestParseBashToolWithBoolean(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected *BashToolConfig
	}{
		{
			name:     "bash: true enables all commands",
			input:    true,
			expected: &BashToolConfig{AllowedCommands: nil},
		},
		{
			name:     "bash: false explicitly disables",
			input:    false,
			expected: &BashToolConfig{AllowedCommands: []string{}},
		},
		{
			name:     "bash: nil is invalid",
			input:    nil,
			expected: nil,
		},
		{
			name:  "bash with array",
			input: []any{"echo", "ls"},
			expected: &BashToolConfig{
				AllowedCommands: []string{"echo", "ls"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseBashTool(tt.input)

			if tt.expected == nil {
				assert.Nil(t, result, "Expected nil result")
			} else {
				require.NotNil(t, result, "Expected non-nil result")
				if tt.expected.AllowedCommands == nil {
					assert.Nil(t, result.AllowedCommands, "Expected nil AllowedCommands (all allowed)")
				} else {
					assert.Equal(t, tt.expected.AllowedCommands, result.AllowedCommands, "AllowedCommands should match")
				}
			}
		})
	}
}

func TestNewToolsWithInvalidBash(t *testing.T) {
	t.Run("detects invalid bash configuration", func(t *testing.T) {
		toolsMap := map[string]any{
			"bash": nil, // Anonymous syntax
		}

		tools := NewTools(toolsMap)

		// The parser should set Bash to nil for invalid config
		assert.Nil(t, tools.Bash, "Bash should be nil for invalid config")

		// Validation should catch this
		err := validateBashToolConfig(tools, "test-workflow")
		require.Error(t, err, "Expected validation error")
		require.ErrorContains(t, err, "anonymous syntax", "Error should mention anonymous syntax")
	})

	t.Run("accepts valid bash configurations", func(t *testing.T) {
		validConfigs := []map[string]any{
			{"bash": true},
			{"bash": false},
			{"bash": []any{"echo"}},
			{"bash": []any{"*"}},
		}

		for _, toolsMap := range validConfigs {
			tools := NewTools(toolsMap)
			assert.NotNil(t, tools.Bash, "Bash should not be nil for valid config")

			err := validateBashToolConfig(tools, "test-workflow")
			assert.NoError(t, err, "Expected no validation error for valid config")
		}
	})
}

// Note: TestValidateGitToolForSafeOutputs was removed because the validation function
// was removed. Git commands are automatically injected by the compiler when safe-outputs
// needs them (see compiler_safe_outputs.go), so validation was misleading and unnecessary.

func TestValidateGitHubToolConfig(t *testing.T) {
	tests := []struct {
		name        string
		toolsMap    map[string]any
		shouldError bool
		errorMsg    string
	}{
		{
			name:        "nil tools config is valid",
			toolsMap:    nil,
			shouldError: false,
		},
		{
			name:        "no github tool is valid",
			toolsMap:    map[string]any{"bash": true},
			shouldError: false,
		},
		{
			name: "github tool with github-app only is valid",
			toolsMap: map[string]any{
				"github": map[string]any{
					"github-app": map[string]any{
						"app-id":      "123456",
						"private-key": "${{ secrets.APP_PRIVATE_KEY }}",
					},
				},
			},
			shouldError: false,
		},
		{
			name: "github tool with github-token only is valid",
			toolsMap: map[string]any{
				"github": map[string]any{
					"github-token": "${{ secrets.MY_TOKEN }}",
				},
			},
			shouldError: false,
		},
		{
			name: "github tool with both github-app and github-token is invalid",
			toolsMap: map[string]any{
				"github": map[string]any{
					"github-app": map[string]any{
						"app-id":      "123456",
						"private-key": "${{ secrets.APP_PRIVATE_KEY }}",
					},
					"github-token": "${{ secrets.MY_TOKEN }}",
				},
			},
			shouldError: true,
			errorMsg:    "'tools.github.github-app' and 'tools.github.github-token' cannot both be set",
		},
		{
			name: "github tool with neither app nor github-token is valid",
			toolsMap: map[string]any{
				"github": map[string]any{
					"toolsets": []any{"default"},
				},
			},
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools := NewTools(tt.toolsMap)
			err := validateGitHubToolConfig(tools, "test-workflow")

			if tt.shouldError {
				require.Error(t, err, "Expected error for %s", tt.name)
				if tt.errorMsg != "" {
					require.ErrorContains(t, err, tt.errorMsg, "Error message should contain expected text")
				}
			} else {
				assert.NoError(t, err, "Expected no error for %s", tt.name)
			}
		})
	}
}

func TestValidateGitHubGuardPolicy(t *testing.T) {
	tests := []struct {
		name        string
		toolsMap    map[string]any
		shouldError bool
		errorMsg    string
	}{
		{
			name:        "nil tools is valid",
			toolsMap:    nil,
			shouldError: false,
		},
		{
			name:        "no github tool is valid",
			toolsMap:    map[string]any{"bash": true},
			shouldError: false,
		},
		{
			name:        "github tool without guard policy fields is valid",
			toolsMap:    map[string]any{"github": map[string]any{"mode": "remote"}},
			shouldError: false,
		},
		{
			name:        "unsupported GitHub MCP roles field is rejected",
			toolsMap:    map[string]any{"github": map[string]any{"roles": []any{"write"}}},
			shouldError: true,
			errorMsg:    "'tools.github.roles' is not supported by the local compiler; it is not enforced. Remove this unsupported field.",
		},
		{
			name:        "unsupported GitHub MCP private-repos field is rejected",
			toolsMap:    map[string]any{"github": map[string]any{"private-repos": false}},
			shouldError: true,
			errorMsg:    "'tools.github.private-repos' is not supported by the local compiler; it is not enforced. Remove this unsupported field.",
		},
		{
			name: "unsupported GitHub MCP roles and private-repos fields are both reported",
			toolsMap: map[string]any{"github": map[string]any{
				"roles":         []any{"write"},
				"private-repos": false,
			}},
			shouldError: true,
			errorMsg:    "'tools.github.roles', 'tools.github.private-repos' are not supported by the local compiler; these settings are not enforced. Remove these unsupported fields.",
		},
		{
			name: "valid guard policy with allowed-repos=all",
			toolsMap: map[string]any{
				"github": map[string]any{
					"allowed-repos": "all",
					"min-integrity": "unapproved",
				},
			},
			shouldError: false,
		},
		{
			name: "valid guard policy with allowed-repos=public",
			toolsMap: map[string]any{
				"github": map[string]any{
					"allowed-repos": "public",
					"min-integrity": "approved",
				},
			},
			shouldError: false,
		},
		{
			name: "valid guard policy with allowed-repos array ([]any)",
			toolsMap: map[string]any{
				"github": map[string]any{
					"allowed-repos": []any{"owner/repo", "owner/*"},
					"min-integrity": "merged",
				},
			},
			shouldError: false,
		},
		{
			name: "valid guard policy with min-integrity=none",
			toolsMap: map[string]any{
				"github": map[string]any{
					"allowed-repos": "all",
					"min-integrity": "none",
				},
			},
			shouldError: false,
		},
		{
			name: "missing allowed-repos field defaults to all",
			toolsMap: map[string]any{
				"github": map[string]any{
					"min-integrity": "unapproved",
				},
			},
			shouldError: false,
		},
		{
			name: "missing min-integrity field",
			toolsMap: map[string]any{
				"github": map[string]any{
					"allowed-repos": "all",
				},
			},
			shouldError: true,
			errorMsg:    "'github.min-integrity' is required",
		},
		{
			name: "invalid min-integrity value",
			toolsMap: map[string]any{
				"github": map[string]any{
					"allowed-repos": "all",
					"min-integrity": "superuser",
				},
			},
			shouldError: true,
			errorMsg:    "'github.min-integrity' must be one of",
		},
		{
			name: "invalid allowed-repos string value",
			toolsMap: map[string]any{
				"github": map[string]any{
					"allowed-repos": "private",
					"min-integrity": "unapproved",
				},
			},
			shouldError: true,
			errorMsg:    "must be in format",
		},
		{
			name: "allowed-repos github.repository expression is valid",
			toolsMap: map[string]any{
				"github": map[string]any{
					"allowed-repos": "${{ github.repository }}",
					"min-integrity": "approved",
				},
			},
			shouldError: false,
		},
		{
			name: "empty allowed-repos array",
			toolsMap: map[string]any{
				"github": map[string]any{
					"allowed-repos": []any{},
					"min-integrity": "unapproved",
				},
			},
			shouldError: true,
			errorMsg:    "'github.allowed-repos' array cannot be empty",
		},
		{
			name: "allowed-repos array with uppercase pattern",
			toolsMap: map[string]any{
				"github": map[string]any{
					"allowed-repos": []any{"Owner/repo"},
					"min-integrity": "unapproved",
				},
			},
			shouldError: true,
			errorMsg:    "must be lowercase",
		},
		{
			name: "allowed-repos array with invalid pattern format",
			toolsMap: map[string]any{
				"github": map[string]any{
					"allowed-repos": []any{"just-a-name"},
					"min-integrity": "unapproved",
				},
			},
			shouldError: true,
			errorMsg:    "must be in format",
		},
		{
			name: "valid guard policy with blocked-users",
			toolsMap: map[string]any{
				"github": map[string]any{
					"allowed-repos": "all",
					"min-integrity": "unapproved",
					"blocked-users": []string{"spam-bot", "compromised-user"},
				},
			},
			shouldError: false,
		},
		{
			name: "valid guard policy with approval-labels",
			toolsMap: map[string]any{
				"github": map[string]any{
					"allowed-repos":   "all",
					"min-integrity":   "approved",
					"approval-labels": []string{"human-reviewed", "safe-for-agent"},
				},
			},
			shouldError: false,
		},
		{
			name: "valid guard policy with both blocked-users and approval-labels",
			toolsMap: map[string]any{
				"github": map[string]any{
					"allowed-repos":   []any{"myorg/*"},
					"min-integrity":   "approved",
					"blocked-users":   []string{"spam-bot"},
					"approval-labels": []string{"human-reviewed"},
				},
			},
			shouldError: false,
		},
		{
			name: "blocked-users without min-integrity fails",
			toolsMap: map[string]any{
				"github": map[string]any{
					"blocked-users": []string{"spam-bot"},
				},
			},
			shouldError: true,
			errorMsg:    "'github.min-integrity' to be set",
		},
		{
			name: "approval-labels without min-integrity fails",
			toolsMap: map[string]any{
				"github": map[string]any{
					"approval-labels": []string{"human-reviewed"},
				},
			},
			shouldError: true,
			errorMsg:    "'github.min-integrity' to be set",
		},
		{
			name: "blocked-users with empty string entry fails",
			toolsMap: map[string]any{
				"github": map[string]any{
					"allowed-repos": "all",
					"min-integrity": "unapproved",
					"blocked-users": []string{"valid-user", ""},
				},
			},
			shouldError: true,
			errorMsg:    "'github.blocked-users' entries must not be empty strings",
		},
		{
			name: "approval-labels with empty string entry fails",
			toolsMap: map[string]any{
				"github": map[string]any{
					"allowed-repos":   "all",
					"min-integrity":   "approved",
					"approval-labels": []string{""},
				},
			},
			shouldError: true,
			errorMsg:    "'github.approval-labels' entries must not be empty strings",
		},
		{
			name: "blocked-users with allowed-repos but without min-integrity fails",
			toolsMap: map[string]any{
				"github": map[string]any{
					"allowed-repos": "all",
					"blocked-users": []string{"spam-bot"},
				},
			},
			shouldError: true,
			errorMsg:    "'github.min-integrity' to be set",
		},
		{
			name: "allowed-repos non-all without min-integrity fails",
			toolsMap: map[string]any{
				"github": map[string]any{
					"allowed-repos": "public",
				},
			},
			shouldError: true,
			errorMsg:    "'github.min-integrity' is required",
		},
		{
			name: "blocked-users as GitHub Actions expression is valid",
			toolsMap: map[string]any{
				"github": map[string]any{
					"allowed-repos": "all",
					"min-integrity": "unapproved",
					"blocked-users": "${{ vars.BLOCKED_USERS }}",
				},
			},
			shouldError: false,
		},
		{
			name: "blocked-users as comma-separated static string is valid",
			toolsMap: map[string]any{
				"github": map[string]any{
					"allowed-repos": "all",
					"min-integrity": "unapproved",
					"blocked-users": "spam-bot, compromised-user",
				},
			},
			shouldError: false,
		},
		{
			name: "blocked-users as newline-separated static string is valid",
			toolsMap: map[string]any{
				"github": map[string]any{
					"allowed-repos": "all",
					"min-integrity": "unapproved",
					"blocked-users": "spam-bot\ncompromised-user",
				},
			},
			shouldError: false,
		},
		{
			name: "blocked-users expression without min-integrity fails",
			toolsMap: map[string]any{
				"github": map[string]any{
					"blocked-users": "${{ vars.BLOCKED_USERS }}",
				},
			},
			shouldError: true,
			errorMsg:    "'github.min-integrity' to be set",
		},
		{
			name: "approval-labels as GitHub Actions expression is valid",
			toolsMap: map[string]any{
				"github": map[string]any{
					"allowed-repos":   "all",
					"min-integrity":   "approved",
					"approval-labels": "${{ vars.APPROVAL_LABELS }}",
				},
			},
			shouldError: false,
		},
		{
			name: "valid guard policy with trusted-users",
			toolsMap: map[string]any{
				"github": map[string]any{
					"allowed-repos": "all",
					"min-integrity": "approved",
					"trusted-users": []any{"contractor-1", "partner-dev"},
				},
			},
			shouldError: false,
		},
		{
			name: "trusted-users without min-integrity fails",
			toolsMap: map[string]any{
				"github": map[string]any{
					"trusted-users": []any{"contractor-1"},
				},
			},
			shouldError: true,
			errorMsg:    "'github.min-integrity' to be set",
		},
		{
			name: "trusted-users with empty string entry fails",
			toolsMap: map[string]any{
				"github": map[string]any{
					"allowed-repos": "all",
					"min-integrity": "approved",
					"trusted-users": []any{""},
				},
			},
			shouldError: true,
			errorMsg:    "'github.trusted-users' entries must not be empty strings",
		},
		{
			name: "trusted-users as GitHub Actions expression is valid",
			toolsMap: map[string]any{
				"github": map[string]any{
					"allowed-repos": "all",
					"min-integrity": "approved",
					"trusted-users": "${{ vars.TRUSTED_USERS }}",
				},
			},
			shouldError: false,
		},
		{
			name: "trusted-users expression without min-integrity fails",
			toolsMap: map[string]any{
				"github": map[string]any{
					"trusted-users": "${{ vars.TRUSTED_USERS }}",
				},
			},
			shouldError: true,
			errorMsg:    "'github.min-integrity' to be set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools := NewTools(tt.toolsMap)
			err := validateGitHubGuardPolicy(tools, "test-workflow")

			if tt.shouldError {
				require.Error(t, err, "Expected error for %s", tt.name)
				if tt.errorMsg != "" {
					require.ErrorContains(t, err, tt.errorMsg, "Error message should contain expected text")
				}
			} else {
				assert.NoError(t, err, "Expected no error for %s", tt.name)
			}
		})
	}
}

func TestValidateGitHubGuardPolicyLockdownWarning(t *testing.T) {
	tests := []struct {
		name        string
		toolsMap    map[string]any
		shouldError bool
		errorMsg    string
	}{
		{
			name: "allowed-repos and min-integrity",
			toolsMap: map[string]any{
				"github": map[string]any{
					"lockdown":      true,
					"allowed-repos": "all",
					"min-integrity": "approved",
				},
			},
			shouldError: false,
		},
		{
			name: "deprecated repos alias and min-integrity",
			toolsMap: map[string]any{
				"github": map[string]any{
					"lockdown":      true,
					"repos":         "all",
					"min-integrity": "approved",
				},
			},
			shouldError: false,
		},
		{
			name: "allowed-repos without min-integrity still fails under lockdown",
			toolsMap: map[string]any{
				"github": map[string]any{
					"lockdown":      true,
					"allowed-repos": "all",
				},
			},
			shouldError: true,
			errorMsg:    "'github.min-integrity' is required",
		},
		{
			name: "blocked-users with min-integrity",
			toolsMap: map[string]any{
				"github": map[string]any{
					"lockdown":      true,
					"min-integrity": "approved",
					"blocked-users": []string{"spam-bot"},
				},
			},
			shouldError: false,
		},
		{
			name: "trusted-users with min-integrity",
			toolsMap: map[string]any{
				"github": map[string]any{
					"lockdown":      true,
					"min-integrity": "approved",
					"trusted-users": []any{"trusted-user"},
				},
			},
			shouldError: false,
		},
		{
			name: "approval-labels with min-integrity",
			toolsMap: map[string]any{
				"github": map[string]any{
					"lockdown":        true,
					"min-integrity":   "approved",
					"approval-labels": []string{"human-reviewed"},
				},
			},
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools := NewTools(tt.toolsMap)
			err := validateGitHubGuardPolicy(tools, "test-workflow")
			if tt.shouldError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					require.ErrorContains(t, err, tt.errorMsg)
				}
				return
			}
			require.NoError(t, err)

			compiler := NewCompiler()
			stderrOutput := captureStderr(func() {
				emitGitHubLockdownGuardPolicyWarning(compiler, tools, "test-workflow.md")
			})

			assert.Contains(t, stderrOutput, "tools.github.lockdown: true")
			assert.Contains(t, stderrOutput, "guard policy fields")
			assert.Contains(t, stderrOutput, "will be ignored")
			assert.Equal(t, 1, compiler.GetWarningCount())
		})
	}
}

func TestUnsupportedGitHubAccessControlFieldsErrorExample(t *testing.T) {
	err := unsupportedGitHubAccessControlFieldsError([]string{"roles"})
	require.ErrorContains(t, err,
		"Supported repository-scope example (not a substitute for role or visibility filtering):")
	require.ErrorContains(t, err, "allowed-repos: [\"owner/repo\"]")
}

func TestValidateGitHubGuardPolicyLockdownWarningWithoutCompiler(t *testing.T) {
	tools := NewTools(map[string]any{
		"github": map[string]any{
			"lockdown":      true,
			"allowed-repos": "all",
			"min-integrity": "approved",
		},
	})

	require.NoError(t, validateGitHubGuardPolicy(tools, "test-workflow"))

	stderrOutput := captureStderr(func() {
		emitGitHubLockdownGuardPolicyWarning(nil, tools, "test-workflow.md")
	})

	assert.Empty(t, strings.TrimSpace(stderrOutput))
}

func TestValidateReposScope(t *testing.T) {
	tests := []struct {
		name        string
		repos       GitHubReposScope
		shouldError bool
		errorMsg    string
	}{
		{
			name:        "valid []string repos array",
			repos:       GitHubReposScope{"owner/repo", "owner/*"},
			shouldError: false,
		},
		{
			name:        "valid []string repos array with github.repository expression",
			repos:       GitHubReposScope{"${{ github.repository }}", "owner/repo"},
			shouldError: false,
		},
		{
			name:        "empty []string repos array",
			repos:       GitHubReposScope{},
			shouldError: true,
			errorMsg:    "array cannot be empty",
		},
		{
			name:        "[]string with invalid pattern",
			repos:       GitHubReposScope{"Owner/Repo"},
			shouldError: true,
			errorMsg:    "must be lowercase",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateReposScope(tt.repos, "test-workflow")

			if tt.shouldError {
				require.Error(t, err, "Expected error for %s", tt.name)
				if tt.errorMsg != "" {
					require.ErrorContains(t, err, tt.errorMsg, "Error message should contain expected text")
				}
			} else {
				assert.NoError(t, err, "Expected no error for %s", tt.name)
			}
		})
	}
}

// TestMCPGSupportsIntegrityReactions verifies the version gate for integrity-reactions.
func TestMCPGSupportsIntegrityReactions(t *testing.T) {
	// Compute expected result for default-version cases dynamically so the test
	// doesn't break every time DefaultMCPGatewayVersion is bumped.
	defaultVersion := string(constants.DefaultMCPGatewayVersion)
	minVersion := string(constants.MCPGIntegrityReactionsMinVersion)
	defaultSupported := semverutil.Compare(defaultVersion, minVersion) >= 0

	tests := []struct {
		name          string
		gatewayConfig *MCPGatewayRuntimeConfig
		want          bool
	}{
		{
			name:          fmt.Sprintf("nil gateway config uses default (%s)", defaultVersion),
			gatewayConfig: nil,
			want:          defaultSupported,
		},
		{
			name:          fmt.Sprintf("empty version uses default (%s)", defaultVersion),
			gatewayConfig: &MCPGatewayRuntimeConfig{Container: "ghcr.io/test/mcpg"},
			want:          defaultSupported,
		},
		{
			name: "version exactly at minimum (v0.2.18)",
			gatewayConfig: &MCPGatewayRuntimeConfig{
				Container: "ghcr.io/test/mcpg",
				Version:   "v0.2.18",
			},
			want: true,
		},
		{
			name: "version above minimum (v0.2.19)",
			gatewayConfig: &MCPGatewayRuntimeConfig{
				Container: "ghcr.io/test/mcpg",
				Version:   "v0.2.19",
			},
			want: true,
		},
		{
			name: "version below minimum (v0.2.17)",
			gatewayConfig: &MCPGatewayRuntimeConfig{
				Container: "ghcr.io/test/mcpg",
				Version:   "v0.2.17",
			},
			want: false,
		},
		{
			name: "version much higher (v1.0.0)",
			gatewayConfig: &MCPGatewayRuntimeConfig{
				Container: "ghcr.io/test/mcpg",
				Version:   "v1.0.0",
			},
			want: true,
		},
		{
			name: "latest always supported",
			gatewayConfig: &MCPGatewayRuntimeConfig{
				Container: "ghcr.io/test/mcpg",
				Version:   "latest",
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mcpgSupportsIntegrityReactions(tt.gatewayConfig)
			assert.Equal(t, tt.want, got, "mcpgSupportsIntegrityReactions result")
		})
	}
}

// TestValidateIntegrityReactions verifies validation of integrity-reactions fields.
func TestValidateIntegrityReactions(t *testing.T) {
	// Gateway config with MCPG >= v0.2.18 (supports integrity reactions)
	newGatewayConfig := &MCPGatewayRuntimeConfig{
		Container: "ghcr.io/test/mcpg",
		Version:   "v0.2.18",
	}
	// Gateway config with MCPG < v0.2.18 (does not support integrity reactions)
	oldGatewayConfig := &MCPGatewayRuntimeConfig{
		Container: "ghcr.io/test/mcpg",
		Version:   "v0.2.17",
	}

	makeDataWithFeature := func(enabled bool) *WorkflowData {
		features := map[string]any{}
		if enabled {
			features["integrity-reactions"] = true
		}
		return &WorkflowData{Features: features}
	}

	tests := []struct {
		name          string
		tools         *Tools
		data          *WorkflowData
		gatewayConfig *MCPGatewayRuntimeConfig
		shouldError   bool
		errorContains string
	}{
		{
			name:          "nil tools is valid",
			tools:         nil,
			data:          makeDataWithFeature(true),
			gatewayConfig: newGatewayConfig,
			shouldError:   false,
		},
		{
			name:          "no github tool is valid",
			tools:         &Tools{},
			data:          makeDataWithFeature(true),
			gatewayConfig: newGatewayConfig,
			shouldError:   false,
		},
		{
			name: "no reaction fields is valid (feature disabled)",
			tools: &Tools{
				GitHub: &GitHubToolConfig{
					MinIntegrity: GitHubIntegrityApproved,
				},
			},
			data:          makeDataWithFeature(false),
			gatewayConfig: newGatewayConfig,
			shouldError:   false,
		},
		{
			name: "valid endorsement and disapproval reactions",
			tools: &Tools{
				GitHub: &GitHubToolConfig{
					MinIntegrity:         GitHubIntegrityApproved,
					EndorsementReactions: []string{"THUMBS_UP", "HEART"},
					DisapprovalReactions: []string{"THUMBS_DOWN", "CONFUSED"},
				},
			},
			data:          makeDataWithFeature(true),
			gatewayConfig: newGatewayConfig,
			shouldError:   false,
		},
		{
			name: "valid with all optional fields",
			tools: &Tools{
				GitHub: &GitHubToolConfig{
					MinIntegrity:         GitHubIntegrityApproved,
					EndorsementReactions: []string{"THUMBS_UP"},
					DisapprovalReactions: []string{"THUMBS_DOWN"},
					DisapprovalIntegrity: "none",
					EndorserMinIntegrity: "approved",
				},
			},
			data:          makeDataWithFeature(true),
			gatewayConfig: newGatewayConfig,
			shouldError:   false,
		},
		{
			name: "reaction fields without feature flag",
			tools: &Tools{
				GitHub: &GitHubToolConfig{
					MinIntegrity:         GitHubIntegrityApproved,
					EndorsementReactions: []string{"THUMBS_UP"},
				},
			},
			data:          makeDataWithFeature(false),
			gatewayConfig: newGatewayConfig,
			shouldError:   true,
			errorContains: "integrity-reactions",
		},
		{
			name: "reaction fields with old MCPG version",
			tools: &Tools{
				GitHub: &GitHubToolConfig{
					MinIntegrity:         GitHubIntegrityApproved,
					EndorsementReactions: []string{"THUMBS_UP"},
				},
			},
			data:          makeDataWithFeature(true),
			gatewayConfig: oldGatewayConfig,
			shouldError:   true,
			errorContains: "v0.2.18",
		},
		{
			name: "reaction fields without min-integrity",
			tools: &Tools{
				GitHub: &GitHubToolConfig{
					EndorsementReactions: []string{"THUMBS_UP"},
				},
			},
			data:          makeDataWithFeature(true),
			gatewayConfig: newGatewayConfig,
			shouldError:   true,
			errorContains: "min-integrity",
		},
		{
			name: "invalid endorsement reaction value",
			tools: &Tools{
				GitHub: &GitHubToolConfig{
					MinIntegrity:         GitHubIntegrityApproved,
					EndorsementReactions: []string{"INVALID_REACTION"},
				},
			},
			data:          makeDataWithFeature(true),
			gatewayConfig: newGatewayConfig,
			shouldError:   true,
			errorContains: "INVALID_REACTION",
		},
		{
			name: "invalid disapproval reaction value",
			tools: &Tools{
				GitHub: &GitHubToolConfig{
					MinIntegrity:         GitHubIntegrityApproved,
					DisapprovalReactions: []string{"WAVE"},
				},
			},
			data:          makeDataWithFeature(true),
			gatewayConfig: newGatewayConfig,
			shouldError:   true,
			errorContains: "WAVE",
		},
		{
			name: "invalid disapproval-integrity value",
			tools: &Tools{
				GitHub: &GitHubToolConfig{
					MinIntegrity:         GitHubIntegrityApproved,
					DisapprovalIntegrity: "invalid-level",
				},
			},
			data:          makeDataWithFeature(true),
			gatewayConfig: newGatewayConfig,
			shouldError:   true,
			errorContains: "invalid-level",
		},
		{
			name: "invalid endorser-min-integrity value",
			tools: &Tools{
				GitHub: &GitHubToolConfig{
					MinIntegrity:         GitHubIntegrityApproved,
					EndorserMinIntegrity: "none",
				},
			},
			data:          makeDataWithFeature(true),
			gatewayConfig: newGatewayConfig,
			shouldError:   true,
			errorContains: "none",
		},
		{
			name: "only disapproval-integrity (no reaction arrays) with min-integrity is valid",
			tools: &Tools{
				GitHub: &GitHubToolConfig{
					MinIntegrity:         GitHubIntegrityApproved,
					DisapprovalIntegrity: "none",
				},
			},
			data:          makeDataWithFeature(true),
			gatewayConfig: newGatewayConfig,
			shouldError:   false,
		},
		{
			name: "feature flag enabled with min-integrity but no explicit reactions — valid (defaults used)",
			tools: &Tools{
				GitHub: &GitHubToolConfig{
					MinIntegrity: GitHubIntegrityApproved,
				},
			},
			data:          makeDataWithFeature(true),
			gatewayConfig: newGatewayConfig,
			shouldError:   false,
		},
		{
			name: "feature flag enabled without min-integrity — error even without explicit reactions",
			tools: &Tools{
				GitHub: &GitHubToolConfig{},
			},
			data:          makeDataWithFeature(true),
			gatewayConfig: newGatewayConfig,
			shouldError:   true,
			errorContains: "min-integrity",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateIntegrityReactions(tt.tools, "test-workflow", tt.data, tt.gatewayConfig)

			if tt.shouldError {
				require.Error(t, err, "Expected error for: %s", tt.name)
				if tt.errorContains != "" {
					require.ErrorContains(t, err, tt.errorContains, "Error should mention: %s", tt.errorContains)
				}
			} else {
				assert.NoError(t, err, "Expected no error for: %s", tt.name)
			}
		})
	}
}

// TestGetDIFCProxyPolicyJSONWithReactions verifies that reaction fields are injected
// into the DIFC proxy policy when the integrity-reactions feature flag is enabled and
// the MCPG version supports it.
func TestGetDIFCProxyPolicyJSONWithReactions(t *testing.T) {
	newGatewayConfig := &MCPGatewayRuntimeConfig{
		Container: "ghcr.io/test/mcpg",
		Version:   "v0.2.18",
	}
	oldGatewayConfig := &MCPGatewayRuntimeConfig{
		Container: "ghcr.io/test/mcpg",
		Version:   "v0.2.17",
	}

	makeDataWithFeature := func(enabled bool) *WorkflowData {
		features := map[string]any{}
		if enabled {
			features["integrity-reactions"] = true
		}
		return &WorkflowData{Features: features}
	}

	tests := []struct {
		name             string
		githubTool       map[string]any
		data             *WorkflowData
		gatewayConfig    *MCPGatewayRuntimeConfig
		expectedContains []string
		expectedAbsent   []string
	}{
		{
			name: "reactions injected when feature enabled and MCPG supports it",
			githubTool: map[string]any{
				"min-integrity":         "approved",
				"endorsement-reactions": []any{"THUMBS_UP", "HEART"},
				"disapproval-reactions": []any{"THUMBS_DOWN"},
			},
			data:             makeDataWithFeature(true),
			gatewayConfig:    newGatewayConfig,
			expectedContains: []string{`"endorsement-reactions"`, `"disapproval-reactions"`, "THUMBS_UP", "HEART", "THUMBS_DOWN"},
		},
		{
			name: "reactions not injected when feature disabled",
			githubTool: map[string]any{
				"min-integrity":         "approved",
				"endorsement-reactions": []any{"THUMBS_UP"},
			},
			data:           makeDataWithFeature(false),
			gatewayConfig:  newGatewayConfig,
			expectedAbsent: []string{"endorsement-reactions"},
		},
		{
			name: "reactions not injected when MCPG version too old",
			githubTool: map[string]any{
				"min-integrity":         "approved",
				"endorsement-reactions": []any{"THUMBS_UP"},
			},
			data:           makeDataWithFeature(true),
			gatewayConfig:  oldGatewayConfig,
			expectedAbsent: []string{"endorsement-reactions"},
		},
		{
			name: "optional reaction fields injected when present",
			githubTool: map[string]any{
				"min-integrity":          "approved",
				"endorsement-reactions":  []any{"THUMBS_UP"},
				"disapproval-integrity":  "none",
				"endorser-min-integrity": "approved",
			},
			data:             makeDataWithFeature(true),
			gatewayConfig:    newGatewayConfig,
			expectedContains: []string{`"disapproval-integrity"`, `"endorser-min-integrity"`},
		},
		{
			name: "defaults injected when feature enabled but no explicit reactions",
			githubTool: map[string]any{
				"min-integrity": "approved",
			},
			data:          makeDataWithFeature(true),
			gatewayConfig: newGatewayConfig,
			expectedContains: []string{
				`"endorsement-reactions"`, "THUMBS_UP", "HEART",
				`"disapproval-reactions"`, "THUMBS_DOWN", "CONFUSED",
			},
		},
		{
			name: "explicit reactions override defaults",
			githubTool: map[string]any{
				"min-integrity":         "approved",
				"endorsement-reactions": []any{"ROCKET"},
				"disapproval-reactions": []any{"EYES"},
			},
			data:             makeDataWithFeature(true),
			gatewayConfig:    newGatewayConfig,
			expectedContains: []string{"ROCKET", "EYES"},
			expectedAbsent:   []string{"HEART", "CONFUSED"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getDIFCProxyPolicyJSON(tt.githubTool, tt.data, tt.gatewayConfig)
			require.NotEmpty(t, got, "policy JSON should not be empty")

			for _, s := range tt.expectedContains {
				assert.Contains(t, got, s, "policy JSON should contain %q", s)
			}
			for _, s := range tt.expectedAbsent {
				assert.NotContains(t, got, s, "policy JSON should NOT contain %q", s)
			}
		})
	}
}

func TestValidateCLIProxyBashCompatibility(t *testing.T) {
	tests := []struct {
		name        string
		toolsMap    map[string]any
		shouldError bool
		errorField  string
	}{
		{
			name:        "cli-proxy enabled without bash setting is valid",
			toolsMap:    map[string]any{"cli-proxy": true},
			shouldError: false,
		},
		{
			name:        "cli-proxy enabled with bash allowlist is valid",
			toolsMap:    map[string]any{"bash": []any{"cat"}, "cli-proxy": true},
			shouldError: false,
		},
		{
			name:        "cli-proxy disabled with bash: false is valid",
			toolsMap:    map[string]any{"bash": false, "cli-proxy": false},
			shouldError: false,
		},
		{
			name:        "bash false without cli-proxy key is valid outside strict mode",
			toolsMap:    map[string]any{"bash": false},
			shouldError: false,
		},
		{
			name:        "cli-proxy enabled with bash: false is rejected",
			toolsMap:    map[string]any{"bash": false, "cli-proxy": true},
			shouldError: true,
		},
		{
			name:        "cli-proxy enabled with empty bash allowlist is rejected",
			toolsMap:    map[string]any{"bash": []any{}, "cli-proxy": true},
			shouldError: true,
		},
		{
			name:        "github gh-proxy with bash false is rejected",
			toolsMap:    map[string]any{"bash": false, "cli-proxy": false, "github": map[string]any{"mode": "gh-proxy"}},
			shouldError: true,
			errorField:  "tools.github.mode",
		},
		{
			name:        "github cli alias with bash false is rejected",
			toolsMap:    map[string]any{"bash": false, "cli-proxy": false, "github": map[string]any{"mode": "cli"}},
			shouldError: true,
			errorField:  "tools.github.mode",
		},
		{
			name:        "github local with bash false is valid",
			toolsMap:    map[string]any{"bash": false, "cli-proxy": false, "github": map[string]any{"mode": "local"}},
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCLIProxyBashCompatibility(tt.toolsMap, "test-workflow")
			if tt.shouldError {
				require.Error(t, err)
				errorField := tt.errorField
				if errorField == "" {
					errorField = "tools.cli-proxy"
				}
				assert.Contains(t, err.Error(), errorField)
				return
			}
			assert.NoError(t, err)
		})
	}
}
