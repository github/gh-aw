//go:build !integration

package workflow

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseGitHubToolPrivateToPublicFlows tests parsing of tools.github.private-to-public-flows
// from frontmatter into GitHubToolConfig.PrivateToPublicFlows.
func TestParseGitHubToolPrivateToPublicFlows(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		wantVal any
		wantNil bool
	}{
		{
			name:    "string allow",
			input:   map[string]any{"private-to-public-flows": "allow"},
			wantVal: "allow",
		},
		{
			name:    "array of server IDs ([]any)",
			input:   map[string]any{"private-to-public-flows": []any{"github", "custom-server"}},
			wantVal: []string{"github", "custom-server"},
		},
		{
			name:    "array of server IDs ([]string)",
			input:   map[string]any{"private-to-public-flows": []string{"srv-a", "srv-b"}},
			wantVal: []string{"srv-a", "srv-b"},
		},
		{
			name:    "field absent",
			input:   map[string]any{},
			wantNil: true,
		},
		{
			name:    "unsupported type ignored",
			input:   map[string]any{"private-to-public-flows": 42},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := parseGitHubTool(tt.input)
			require.NotNil(t, cfg, "parseGitHubTool should not return nil")
			if tt.wantNil {
				assert.Nil(t, cfg.PrivateToPublicFlows)
			} else {
				assert.Equal(t, tt.wantVal, cfg.PrivateToPublicFlows)
			}
		})
	}
}

// TestBuildMCPGatewayConfigPrivateToPublicFlows verifies that buildMCPGatewayConfig correctly
// translates tools.github.private-to-public-flows into ForcePublicRepos / SinkVisibilityExemptServers
// on the returned MCPGatewayRuntimeConfig.
func TestBuildMCPGatewayConfigPrivateToPublicFlows(t *testing.T) {
	falseVal := false

	tests := []struct {
		name                        string
		privateToPublicFlows        any
		wantForcePublicRepos        *bool
		wantSinkVisibilityExemptSvr []string
	}{
		{
			name:                 "allow: sets ForcePublicRepos to false",
			privateToPublicFlows: "allow",
			wantForcePublicRepos: &falseVal,
		},
		{
			name:                        "list: sets SinkVisibilityExemptServers",
			privateToPublicFlows:        []string{"github", "custom-srv"},
			wantSinkVisibilityExemptSvr: []string{"github", "custom-srv"},
		},
		{
			name:                 "nil: no override fields set",
			privateToPublicFlows: nil,
			wantForcePublicRepos: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wd := &WorkflowData{
				ParsedTools: &Tools{
					GitHub: &GitHubToolConfig{
						ReadOnly:             true,
						PrivateToPublicFlows: tt.privateToPublicFlows,
					},
				},
				SandboxConfig: &SandboxConfig{
					MCP: &MCPGatewayRuntimeConfig{},
				},
			}

			cfg := buildMCPGatewayConfig(wd)
			require.NotNil(t, cfg)

			if tt.wantForcePublicRepos == nil {
				assert.Nil(t, cfg.ForcePublicRepos, "ForcePublicRepos should be nil when not set")
			} else {
				require.NotNil(t, cfg.ForcePublicRepos, "ForcePublicRepos should be set")
				assert.Equal(t, *tt.wantForcePublicRepos, *cfg.ForcePublicRepos)
			}

			assert.Equal(t, tt.wantSinkVisibilityExemptSvr, cfg.SinkVisibilityExemptServers)
		})
	}
}

// TestPrivateToPublicFlowsGatewayEmission compiles a minimal workflow with
// tools.github.private-to-public-flows and checks that the rendered MCP gateway
// JSON contains the expected forcePublicRepos / sinkVisibilityExemptServers fields.
func TestPrivateToPublicFlowsGatewayEmission(t *testing.T) {
	makeWorkflow := func(ptpFlows string) string {
		return `---
on:
  workflow_dispatch:
strict: false
permissions:
  contents: read
engine: copilot
tools:
  github:
    private-to-public-flows: ` + ptpFlows + `
---

# Test workflow
`
	}

	makeWorkflowListForm := func() string {
		return `---
on:
  workflow_dispatch:
strict: false
permissions:
  contents: read
engine: copilot
tools:
  github:
    private-to-public-flows:
      - github
      - custom-server
mcp-servers:
  custom-server:
    type: http
    url: "http://localhost:9000/mcp"
---

# Test workflow
`
	}

	tests := []struct {
		name            string
		content         string
		wantContains    []string
		wantNotContains []string
	}{
		{
			name:            "allow form emits forcePublicRepos false",
			content:         makeWorkflow("allow"),
			wantContains:    []string{`"forcePublicRepos": false`},
			wantNotContains: []string{`"sinkVisibilityExemptServers"`},
		},
		{
			name:            "list form emits sinkVisibilityExemptServers",
			content:         makeWorkflowListForm(),
			wantContains:    []string{`"sinkVisibilityExemptServers": ["github", "custom-server"]`},
			wantNotContains: []string{`"forcePublicRepos"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile, err := os.CreateTemp("", "test-ptp-*.md")
			require.NoError(t, err, "failed to create temp file")
			defer os.Remove(tmpFile.Name())

			_, err = tmpFile.WriteString(tt.content)
			require.NoError(t, err)
			tmpFile.Close()

			compiler := NewCompiler()
			compiler.SetSkipValidation(true)

			workflowData, err := compiler.ParseWorkflowFile(tmpFile.Name())
			require.NoError(t, err, "parsing should succeed")
			require.NotNil(t, workflowData)

			yaml, _, _, err := compiler.generateYAML(workflowData, tmpFile.Name())
			require.NoError(t, err, "YAML generation should succeed")

			for _, want := range tt.wantContains {
				assert.Contains(t, yaml, want,
					"expected YAML to contain %q\n\nYAML snippet:\n%s",
					want, extractGatewaySection(yaml))
			}
			for _, notWant := range tt.wantNotContains {
				assert.NotContains(t, yaml, notWant,
					"expected YAML NOT to contain %q\n\nYAML snippet:\n%s",
					notWant, extractGatewaySection(yaml))
			}
		})
	}
}

// TestPrivateToPublicFlowsStrictModeValidation verifies that strict mode rejects
// private-to-public-flows: allow but permits the list form.
func TestPrivateToPublicFlowsStrictModeValidation(t *testing.T) {
	makeWorkflow := func(ptpFlows string) string {
		return `---
on:
  workflow_dispatch:
permissions:
  contents: read
engine: claude
network:
  allowed:
    - github.com
tools:
  github:
    private-to-public-flows: ` + ptpFlows + `
---

# Test workflow
`
	}

	makeWorkflowListForm := func() string {
		return `---
on:
  workflow_dispatch:
permissions:
  contents: read
engine: claude
network:
  allowed:
    - github.com
tools:
  github:
    private-to-public-flows:
      - github
---

# Test workflow
`
	}

	t.Run("allow is rejected in strict mode", func(t *testing.T) {
		tmpFile, err := os.CreateTemp("", "test-ptp-strict-*.md")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())

		_, err = tmpFile.WriteString(makeWorkflow("allow"))
		require.NoError(t, err)
		tmpFile.Close()

		compiler := NewCompiler()
		compiler.SetStrictMode(true)
		compiler.SetSkipValidation(false)

		err = compiler.CompileWorkflow(tmpFile.Name())
		require.Error(t, err, "strict mode should reject private-to-public-flows: allow")
		require.ErrorContains(t, err, "private-to-public-flows")
	})

	t.Run("list form is accepted in strict mode", func(t *testing.T) {
		tmpFile, err := os.CreateTemp("", "test-ptp-strict-list-*.md")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())

		_, err = tmpFile.WriteString(makeWorkflowListForm())
		require.NoError(t, err)
		tmpFile.Close()

		compiler := NewCompiler()
		compiler.SetStrictMode(true)
		compiler.SetSkipValidation(false)

		err = compiler.CompileWorkflow(tmpFile.Name())
		require.NoError(t, err, "strict mode should accept list form of private-to-public-flows")
	})
}

// extractGatewaySection extracts the gateway JSON section from compiled YAML for test diagnostics.
func extractGatewaySection(yaml string) string {
	start := strings.Index(yaml, `"gateway"`)
	if start == -1 {
		return "(no gateway section found)"
	}
	end := min(start+500, len(yaml))
	return yaml[start:end]
}

// TestPrivateToPublicFlowsAllowSuppressesSinkVisibility verifies that
// private-to-public-flows: allow suppresses sink-visibility in the write-sink guard policy.
func TestPrivateToPublicFlowsAllowSuppressesSinkVisibility(t *testing.T) {
	t.Run("allow suppresses sink-visibility in auto-lockdown path", func(t *testing.T) {
		wd := &WorkflowData{
			Tools: map[string]any{
				"github": map[string]any{},
			},
			ParsedTools: &Tools{
				GitHub: &GitHubToolConfig{
					ReadOnly:             false,
					PrivateToPublicFlows: "allow",
				},
			},
		}
		policy := deriveWriteSinkGuardPolicyFromWorkflow(wd)
		require.NotNil(t, policy, "policy should be derived when github tool is present")
		writeSink, ok := policy["write-sink"].(map[string]any)
		require.True(t, ok, "policy should have write-sink")
		assert.NotContains(t, writeSink, "sink-visibility",
			"sink-visibility should be absent when private-to-public-flows: allow is set")
		assert.Contains(t, writeSink, "accept",
			"accept should still be present")
	})

	t.Run("no allow preserves sink-visibility in auto-lockdown path", func(t *testing.T) {
		wd := &WorkflowData{
			Tools: map[string]any{
				"github": map[string]any{},
			},
			ParsedTools: &Tools{
				GitHub: &GitHubToolConfig{
					ReadOnly: false,
				},
			},
		}
		policy := deriveWriteSinkGuardPolicyFromWorkflow(wd)
		require.NotNil(t, policy)
		writeSink, ok := policy["write-sink"].(map[string]any)
		require.True(t, ok)
		assert.Contains(t, writeSink, "sink-visibility",
			"sink-visibility should be present when private-to-public-flows is not set")
	})
}

// TestValidatePrivateToPublicFlowsServerIDs tests that unknown server IDs are rejected.
func TestValidatePrivateToPublicFlowsServerIDs(t *testing.T) {
	t.Run("valid declared servers accepted", func(t *testing.T) {
		wd := &WorkflowData{
			Tools: map[string]any{"github": map[string]any{}, "my-server": map[string]any{}},
			ParsedTools: &Tools{
				GitHub: &GitHubToolConfig{PrivateToPublicFlows: []string{"github", "my-server"}},
			},
		}
		assert.NoError(t, validatePrivateToPublicFlowsServerIDs(wd))
	})

	t.Run("unknown server ID rejected", func(t *testing.T) {
		wd := &WorkflowData{
			Tools: map[string]any{"github": map[string]any{}},
			ParsedTools: &Tools{
				GitHub: &GitHubToolConfig{PrivateToPublicFlows: []string{"github", "undeclared-server"}},
			},
		}
		err := validatePrivateToPublicFlowsServerIDs(wd)
		require.Error(t, err, "undeclared server ID should be rejected")
		require.ErrorContains(t, err, "undeclared-server")
	})

	t.Run("allow string form skipped", func(t *testing.T) {
		wd := &WorkflowData{
			Tools: map[string]any{"github": map[string]any{}},
			ParsedTools: &Tools{
				GitHub: &GitHubToolConfig{PrivateToPublicFlows: "allow"},
			},
		}
		assert.NoError(t, validatePrivateToPublicFlowsServerIDs(wd),
			"string allow form should not trigger server ID validation")
	})

	t.Run("nil parsed tools returns nil", func(t *testing.T) {
		assert.NoError(t, validatePrivateToPublicFlowsServerIDs(&WorkflowData{}))
	})
}

func TestValidatePrivateToPublicFlowsStringValue(t *testing.T) {
	t.Run("allow string accepted", func(t *testing.T) {
		wd := &WorkflowData{
			ParsedTools: &Tools{
				GitHub: &GitHubToolConfig{PrivateToPublicFlows: "allow"},
			},
		}
		assert.NoError(t, validatePrivateToPublicFlowsStringValue(wd))
	})

	t.Run("invalid string rejected", func(t *testing.T) {
		wd := &WorkflowData{
			ParsedTools: &Tools{
				GitHub: &GitHubToolConfig{PrivateToPublicFlows: "Allow"},
			},
		}
		err := validatePrivateToPublicFlowsStringValue(wd)
		require.Error(t, err)
		require.ErrorContains(t, err, "invalid value")
		require.ErrorContains(t, err, "Allow")
	})

	t.Run("list form skipped", func(t *testing.T) {
		wd := &WorkflowData{
			ParsedTools: &Tools{
				GitHub: &GitHubToolConfig{PrivateToPublicFlows: []string{"github"}},
			},
		}
		assert.NoError(t, validatePrivateToPublicFlowsStringValue(wd))
	})
}

// TestIsSafeMCPServerID tests the shell-safety identifier check.
func TestIsSafeMCPServerID(t *testing.T) {
	safe := []string{"github", "my-server", "my_server", "server123", "UPPER-CASE"}
	for _, id := range safe {
		assert.True(t, isSafeMCPServerID(id), "expected %q to be safe", id)
	}

	unsafe := []string{"", "srv with space", "srv$(cmd)", "srv`cmd`", "srv;rm", "srv|pipe", "srv&bg", "srv>out"}
	for _, id := range unsafe {
		assert.False(t, isSafeMCPServerID(id), "expected %q to be unsafe", id)
	}
}
