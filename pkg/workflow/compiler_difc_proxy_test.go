//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertProxyCuratedUpstreamEnv(t *testing.T, result string) {
	t.Helper()
	assert.Contains(t, result, "GITHUB_SERVER_URL: ${{ github.server_url }}", "proxy step should forward github.server_url")
	assert.Contains(t, result, "GITHUB_API_URL: ${{ github.api_url }}", "proxy step should forward github.api_url")
	assert.Contains(t, result, "GH_HOST: ${{ env.GH_HOST }}", "proxy step should forward GH_HOST into the curated env")
	assert.Contains(t, result, "GITHUB_HOST: ${{ env.GITHUB_HOST }}", "proxy step should forward GITHUB_HOST into the curated env")
	assert.Contains(t, result, "GITHUB_ENTERPRISE_HOST: ${{ env.GITHUB_ENTERPRISE_HOST }}", "proxy step should forward GITHUB_ENTERPRISE_HOST into the curated env")
	assert.Contains(t, result, "GITHUB_GRAPHQL_URL: ${{ env.GITHUB_GRAPHQL_URL }}", "proxy step should forward GITHUB_GRAPHQL_URL into the curated env")
	assert.Contains(t, result, "GITHUB_COPILOT_BASE_URL: ${{ env.GITHUB_COPILOT_BASE_URL }}", "proxy step should forward or preserve a custom Copilot base URL")

	// Verify each upstream env key appears exactly once — duplicates indicate the
	// curated env was emitted more than once or leaked from an earlier block.
	for _, key := range []string{
		"GITHUB_SERVER_URL",
		"GITHUB_API_URL",
		"GH_HOST",
		"GITHUB_HOST",
		"GITHUB_ENTERPRISE_HOST",
		"GITHUB_GRAPHQL_URL",
		"GITHUB_COPILOT_BASE_URL",
	} {
		count := strings.Count(result, key+":")
		assert.Equal(t, 1, count, "env key %q should appear exactly once in the proxy step YAML, got %d occurrences", key, count)
	}
}

// TestHasDIFCProxyNeeded verifies that DIFC proxy injection is triggered only
// when guard policies are configured AND pre-agent steps have GH_TOKEN.
func TestHasDIFCProxyNeeded(t *testing.T) {
	tests := []struct {
		name     string
		data     *WorkflowData
		expected bool
		desc     string
	}{
		{
			name:     "nil workflow data",
			data:     nil,
			expected: false,
			desc:     "nil data should never need proxy",
		},
		{
			name:     "no github tool",
			data:     &WorkflowData{Tools: map[string]any{}},
			expected: false,
			desc:     "no github tool means no guard policy, proxy not needed",
		},
		{
			name: "github tool disabled",
			data: &WorkflowData{
				Tools: map[string]any{
					"github": false,
				},
			},
			expected: false,
			desc:     "disabled github tool should not trigger proxy",
		},
		{
			name: "github tool without guard policy",
			data: &WorkflowData{
				Tools: map[string]any{
					"github": map[string]any{"toolsets": []string{"default"}},
				},
				CustomSteps: "steps:\n  - name: Fetch data\n    env:\n      GH_TOKEN: ${{ github.token }}\n    run: gh issue list",
			},
			expected: false,
			desc:     "no guard policy (auto-lockdown only) should not trigger proxy",
		},
		{
			name: "guard policy configured but no pre-agent steps with GH_TOKEN",
			data: &WorkflowData{
				Tools: map[string]any{
					"github": map[string]any{
						"min-integrity": "approved",
					},
				},
			},
			expected: false,
			desc:     "guard policy without GH_TOKEN pre-agent steps should not trigger proxy",
		},
		{
			name: "guard policy + custom steps with GH_TOKEN but integrity-proxy disabled",
			data: &WorkflowData{
				Tools: map[string]any{
					"github": map[string]any{
						"min-integrity":   "approved",
						"integrity-proxy": false,
					},
				},
				CustomSteps: "steps:\n  - name: Fetch issues\n    env:\n      GH_TOKEN: ${{ github.token }}\n    run: gh issue list",
			},
			expected: false,
			desc:     "integrity-proxy: false → proxy not triggered even when guard policy and GH_TOKEN present",
		},
		{
			name: "guard policy + custom steps with GH_TOKEN (proxy enabled by default)",
			data: &WorkflowData{
				Tools: map[string]any{
					"github": map[string]any{
						"min-integrity": "approved",
					},
				},
				CustomSteps: "steps:\n  - name: Fetch issues\n    env:\n      GH_TOKEN: ${{ github.token }}\n    run: gh issue list",
			},
			expected: true,
			desc:     "guard policy + custom steps with GH_TOKEN should trigger proxy by default (no feature flag needed)",
		},
		{
			name: "guard policy + custom steps with GH_TOKEN + integrity-proxy explicitly true",
			data: &WorkflowData{
				Tools: map[string]any{
					"github": map[string]any{
						"min-integrity":   "approved",
						"integrity-proxy": true,
					},
				},
				CustomSteps: "steps:\n  - name: Fetch issues\n    env:\n      GH_TOKEN: ${{ github.token }}\n    run: gh issue list",
			},
			expected: true,
			desc:     "integrity-proxy: true explicitly set should trigger proxy",
		},
		{
			name: "guard policy + repo-memory configured",
			data: &WorkflowData{
				Tools: map[string]any{
					"github": map[string]any{
						"min-integrity": "approved",
						"repos":         "all",
					},
				},
				RepoMemoryConfig: &RepoMemoryConfig{
					Memories: []RepoMemoryEntry{{ID: "memory"}},
				},
			},
			expected: false,
			desc:     "guard policy + repo-memory should NOT trigger proxy: repo-memory clones use direct git URLs, not GH_HOST",
		},
		{
			name: "guard policy with allowed-repos + custom steps with GH_TOKEN (default enabled)",
			data: &WorkflowData{
				Tools: map[string]any{
					"github": map[string]any{
						"min-integrity": "merged",
						"allowed-repos": []string{"owner/repo"},
					},
				},
				CustomSteps: "steps:\n  - name: Fetch PRs\n    env:\n      GH_TOKEN: ${{ secrets.MY_TOKEN }}\n    run: gh pr list",
			},
			expected: true,
			desc:     "allowed-repos + min-integrity + GH_TOKEN custom steps should trigger proxy by default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasDIFCProxyNeeded(tt.data)
			assert.Equal(t, tt.expected, got, "hasDIFCProxyNeeded: %s", tt.desc)
		})
	}
}

// TestHasPreAgentStepsWithGHToken verifies detection of pre-agent steps with GH_TOKEN.
func TestHasPreAgentStepsWithGHToken(t *testing.T) {
	tests := []struct {
		name     string
		data     *WorkflowData
		expected bool
	}{
		{
			name:     "nil data",
			data:     nil,
			expected: false,
		},
		{
			name:     "empty data",
			data:     &WorkflowData{},
			expected: false,
		},
		{
			name: "custom steps without GH_TOKEN",
			data: &WorkflowData{
				CustomSteps: "steps:\n  - name: Build\n    run: make build\n",
			},
			expected: false,
		},
		{
			name: "custom steps with GH_TOKEN",
			data: &WorkflowData{
				CustomSteps: "steps:\n  - name: Fetch\n    env:\n      GH_TOKEN: ${{ github.token }}\n    run: gh issue list\n",
			},
			expected: true,
		},
		{
			name: "repo-memory configured",
			data: &WorkflowData{
				RepoMemoryConfig: &RepoMemoryConfig{
					Memories: []RepoMemoryEntry{{ID: "memory"}},
				},
			},
			expected: false,
			// repo-memory clone steps use direct "git clone https://x-access-token:${GH_TOKEN}@..."
			// URLs derived from GITHUB_SERVER_URL, not GH_HOST, so the proxy does not intercept them.
		},
		{
			name: "repo-memory with empty memories (no clone steps generated)",
			data: &WorkflowData{
				RepoMemoryConfig: &RepoMemoryConfig{},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasPreAgentStepsWithGHToken(tt.data)
			assert.Equal(t, tt.expected, got, "test: %s", tt.name)
		})
	}
}

// TestGetDIFCProxyPolicyJSON verifies that the proxy policy JSON contains
// only the static fields (min-integrity and repos) without dynamic expressions.
func TestGetDIFCProxyPolicyJSON(t *testing.T) {
	tests := []struct {
		name             string
		githubTool       map[string]any
		expectedContains []string
		expectedAbsent   []string
		expectEmpty      bool
	}{
		{
			name:        "nil tool",
			githubTool:  nil,
			expectEmpty: true,
		},
		{
			name:        "empty map tool",
			githubTool:  map[string]any{},
			expectEmpty: true,
		},
		{
			name: "min-integrity only",
			githubTool: map[string]any{
				"min-integrity": "approved",
			},
			expectedContains: []string{`"allow-only"`, `"min-integrity":"approved"`, `"repos":"all"`},
			expectedAbsent:   []string{"blocked-users", "approval-labels", "steps.parse-guard-vars", "__GH_AW_GUARD_EXPR"},
		},
		{
			name: "min-integrity and repos",
			githubTool: map[string]any{
				"min-integrity": "merged",
				"repos":         "all",
			},
			expectedContains: []string{`"allow-only"`, `"min-integrity":"merged"`, `"repos":"all"`},
			expectedAbsent:   []string{"blocked-users", "approval-labels"},
		},
		{
			name: "allowed-repos (preferred field name)",
			githubTool: map[string]any{
				"min-integrity": "unapproved",
				"allowed-repos": "owner/*",
			},
			expectedContains: []string{`"min-integrity":"unapproved"`, `"repos":"owner/*"`},
			expectedAbsent:   []string{"blocked-users", "approval-labels"},
		},
		{
			name: "allowed-repos github.repository expression",
			githubTool: map[string]any{
				"min-integrity": "approved",
				"allowed-repos": "${{ github.repository }}",
			},
			expectedContains: []string{`"min-integrity":"approved"`, `"repos":"${{ github.repository }}"`},
			expectedAbsent:   []string{"blocked-users", "approval-labels"},
		},
		{
			name: "tool without guard policy fields",
			githubTool: map[string]any{
				"toolsets": []string{"default"},
			},
			expectEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getDIFCProxyPolicyJSON(tt.githubTool, nil, nil)

			if tt.expectEmpty {
				assert.Empty(t, got, "policy JSON should be empty for: %s", tt.name)
				return
			}

			require.NotEmpty(t, got, "policy JSON should not be empty for: %s", tt.name)

			for _, s := range tt.expectedContains {
				assert.Contains(t, got, s, "policy JSON should contain %q for: %s", s, tt.name)
			}
			for _, s := range tt.expectedAbsent {
				assert.NotContains(t, got, s, "policy JSON should NOT contain %q for: %s", s, tt.name)
			}
		})
	}
}

// TestGenerateStartDIFCProxyStep verifies the YAML generated for the proxy start step.
func TestGenerateStartDIFCProxyStep(t *testing.T) {
	c := &Compiler{}

	t.Run("no proxy when guard policy not configured", func(t *testing.T) {
		var yaml strings.Builder
		data := &WorkflowData{
			Tools: map[string]any{
				"github": map[string]any{"toolsets": []string{"default"}},
			},
			CustomSteps:   "steps:\n  - name: Fetch\n    env:\n      GH_TOKEN: ${{ github.token }}\n    run: gh issue list",
			SandboxConfig: &SandboxConfig{},
		}
		c.generateStartDIFCProxyStep(&yaml, data)
		assert.Empty(t, yaml.String(), "should not generate proxy step without guard policy")
	})

	t.Run("no proxy when no GH_TOKEN pre-agent steps", func(t *testing.T) {
		var yaml strings.Builder
		data := &WorkflowData{
			Tools: map[string]any{
				"github": map[string]any{"min-integrity": "approved"},
			},
			SandboxConfig: &SandboxConfig{},
		}
		c.generateStartDIFCProxyStep(&yaml, data)
		assert.Empty(t, yaml.String(), "should not generate proxy step without pre-agent GH_TOKEN steps")
	})

	t.Run("generates start step with guard policy and custom steps", func(t *testing.T) {
		var yaml strings.Builder
		data := &WorkflowData{
			Tools: map[string]any{
				"github": map[string]any{
					"min-integrity": "approved",
				},
			},
			CustomSteps:   "steps:\n  - name: Fetch\n    env:\n      GH_TOKEN: ${{ github.token }}\n    run: gh issue list",
			SandboxConfig: &SandboxConfig{},
		}
		ensureDefaultMCPGatewayConfig(data)
		c.generateStartDIFCProxyStep(&yaml, data)

		result := yaml.String()
		require.NotEmpty(t, result, "should generate proxy start step")
		assert.Contains(t, result, "Start DIFC Proxy", "step name should be present")
		assert.Contains(t, result, "GH_TOKEN:", "step should include GH_TOKEN env var")
		assertProxyCuratedUpstreamEnv(t, result)
		assert.Contains(t, result, "DIFC_PROXY_POLICY:", "step should include policy as env var")
		assert.Contains(t, result, "DIFC_PROXY_IMAGE:", "step should include image as env var")
		assert.Contains(t, result, "start_difc_proxy.sh", "step should call the proxy script")
		assert.Contains(t, result, `"allow-only"`, "step should include guard policy JSON in env var")
		assert.Contains(t, result, `"min-integrity":"approved"`, "step should include min-integrity in policy env var")
		assert.Contains(t, result, "ghcr.io/github/gh-aw-mcpg", "step should include container image in env var")
		assert.NotContains(t, result, "blocked-users", "proxy policy should not include dynamic blocked-users")
		assert.NotContains(t, result, "approval-labels", "proxy policy should not include dynamic approval-labels")
	})
}

// TestGenerateStopDIFCProxyStep verifies the YAML generated for the proxy stop step.
func TestGenerateStopDIFCProxyStep(t *testing.T) {
	c := &Compiler{}

	t.Run("no stop step when proxy not needed", func(t *testing.T) {
		var yaml strings.Builder
		data := &WorkflowData{
			Tools: map[string]any{
				"github": map[string]any{"toolsets": []string{"default"}},
			},
			SandboxConfig: &SandboxConfig{},
		}
		c.generateStopDIFCProxyStep(&yaml, data)
		assert.Empty(t, yaml.String(), "should not generate stop step when proxy not needed")
	})

	t.Run("generates stop step when proxy is needed", func(t *testing.T) {
		var yaml strings.Builder
		data := &WorkflowData{
			Tools: map[string]any{
				"github": map[string]any{"min-integrity": "approved"},
			},
			CustomSteps:   "steps:\n  - name: Fetch\n    env:\n      GH_TOKEN: ${{ github.token }}\n    run: gh issue list",
			SandboxConfig: &SandboxConfig{},
		}
		c.generateStopDIFCProxyStep(&yaml, data)

		result := yaml.String()
		require.NotEmpty(t, result, "should generate proxy stop step")
		assert.Contains(t, result, "Stop DIFC Proxy", "step name should be present")
		assert.Contains(t, result, "stop_difc_proxy.sh", "step should call the stop script")
	})
}

// TestDIFCProxyLogPaths verifies the artifact paths returned for DIFC proxy logs.
func TestDIFCProxyLogPaths(t *testing.T) {
	t.Run("no log paths when proxy not needed", func(t *testing.T) {
		data := &WorkflowData{
			Tools: map[string]any{
				"github": map[string]any{"toolsets": []string{"default"}},
			},
		}
		paths := difcProxyLogPaths(data)
		assert.Empty(t, paths, "should return no log paths when proxy not needed")
	})

	t.Run("returns proxy-logs path when proxy is needed", func(t *testing.T) {
		data := &WorkflowData{
			Tools: map[string]any{
				"github": map[string]any{"min-integrity": "approved"},
			},
			CustomSteps: "steps:\n  - name: Fetch\n    env:\n      GH_TOKEN: ${{ github.token }}\n    run: gh issue list",
		}
		paths := difcProxyLogPaths(data)
		require.Len(t, paths, 2, "should return include path and exclusion path")
		assert.Contains(t, paths[0], "proxy-logs", "first path should include proxy-logs directory")
		assert.Contains(t, paths[1], "proxy-tls", "second path should exclude proxy-tls directory")
		assert.True(t, strings.HasPrefix(paths[1], "!"), "exclusion path should start with !")
	})
}

// TestDIFCProxyStepOrderInCompiledWorkflow verifies that proxy steps are injected
// at the correct positions in the generated workflow YAML.
func TestDIFCProxyStepOrderInCompiledWorkflow(t *testing.T) {
	workflow := `---
on: issues
engine: copilot
tools:
  github:
    mode: local
    toolsets: [default]
    min-integrity: approved
steps:
  - name: Fetch repo data
    env:
      GH_TOKEN: ${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GITHUB_TOKEN }}
    run: |
      gh issue list -R $GITHUB_REPOSITORY --state open --limit 500 \
        --json number,labels > /tmp/gh-aw/issues.json 2>/dev/null \
        || echo '[]' > /tmp/gh-aw/issues.json
---

# Test Workflow

Test that DIFC proxy is injected by default when min-integrity is set with custom steps using GH_TOKEN.
`
	compiler := NewCompiler()
	data, err := compiler.ParseWorkflowString(workflow, "test-workflow.md")
	require.NoError(t, err, "parsing should succeed")

	result, err := compiler.CompileToYAML(data, "test-workflow.md")
	require.NoError(t, err, "compilation should succeed")

	// Verify proxy start step is present
	assert.Contains(t, result, "Start DIFC Proxy",
		"compiled workflow should contain proxy start step")

	// Verify proxy stop step is present
	assert.Contains(t, result, "Stop DIFC Proxy",
		"compiled workflow should contain proxy stop step")

	// Verify the standalone "Set GH_REPO" step is no longer emitted;
	// GH_REPO is now injected as step-level env on each custom step.
	assert.NotContains(t, result, "Set GH_REPO for proxied steps",
		"compiled workflow should NOT contain standalone Set GH_REPO step")

	// Verify proxy env vars are injected into the custom step as step-level env.
	assert.Contains(t, result, "GH_HOST: ${{ env.GH_HOST || 'github.com' }}",
		"custom step should have GH_HOST in step-level env")
	assert.Contains(t, result, "GH_REPO: ${{ github.repository }}",
		"custom step should have GH_REPO in step-level env")
	assert.Contains(t, result, "GITHUB_API_URL: https://localhost:18443/api/v3",
		"custom step should have GITHUB_API_URL in step-level env")
	assert.Contains(t, result, "GITHUB_GRAPHQL_URL: https://localhost:18443/api/graphql",
		"custom step should have GITHUB_GRAPHQL_URL in step-level env")
	assert.Contains(t, result, "NODE_EXTRA_CA_CERTS: /tmp/gh-aw/proxy-logs/proxy-tls/ca.crt",
		"custom step should have NODE_EXTRA_CA_CERTS in step-level env")

	// Verify step ordering: Start proxy must come before Stop proxy
	startIdx := strings.Index(result, "Start DIFC Proxy")
	stopIdx := strings.Index(result, "Stop DIFC Proxy")
	require.Greater(t, startIdx, -1, "start proxy step should be in output")
	require.Greater(t, stopIdx, -1, "stop proxy step should be in output")
	assert.Less(t, startIdx, stopIdx, "Start DIFC Proxy must come before Stop DIFC Proxy")

	// Verify the custom step comes after proxy start and before proxy stop
	customStepIdx := strings.Index(result, "Fetch repo data")
	require.Greater(t, customStepIdx, -1, "custom step should be in output")
	assert.Less(t, startIdx, customStepIdx, "Start DIFC Proxy must come before custom step")
	assert.Less(t, customStepIdx, stopIdx, "custom step must come before Stop DIFC Proxy")

	// Verify proxy stop is before MCP gateway start
	gatewayIdx := strings.Index(result, "Start MCP Gateway")
	require.Greater(t, gatewayIdx, -1, "gateway start step should be in output")
	assert.Less(t, stopIdx, gatewayIdx, "Stop DIFC Proxy must come before Start MCP Gateway")

	// Verify start_difc_proxy.sh and stop_difc_proxy.sh are referenced
	assert.Contains(t, result, "start_difc_proxy.sh", "should reference start script")
	assert.Contains(t, result, "stop_difc_proxy.sh", "should reference stop script")

	// Verify the policy JSON in the proxy start step does NOT contain dynamic fields.
	// Note: the MCP gateway config may include approval-labels/blocked-users, but the proxy policy must not.
	// The policy is stored in the DIFC_PROXY_POLICY env var line.
	proxyPolicyLine := ""
	for line := range strings.SplitSeq(result, "\n") {
		if strings.Contains(line, "DIFC_PROXY_POLICY") {
			proxyPolicyLine = line
			break
		}
	}
	require.NotEmpty(t, proxyPolicyLine, "should find the DIFC_PROXY_POLICY env var line")
	assert.NotContains(t, proxyPolicyLine, "blocked-users", "proxy policy should not include blocked-users")
	assert.NotContains(t, proxyPolicyLine, "approval-labels", "proxy policy should not include approval-labels")
}

// TestDIFCProxyNotInjectedWithoutGuardPolicy verifies no proxy injection without guard policy.
func TestDIFCProxyNotInjectedWithoutGuardPolicy(t *testing.T) {
	workflow := `---
on: issues
engine: copilot
tools:
  github:
    mode: local
    toolsets: [default]
steps:
  - name: Fetch repo data
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: gh issue list
---

# Test Workflow

Test that DIFC proxy is NOT injected when min-integrity is not set.
`
	compiler := NewCompiler()
	data, err := compiler.ParseWorkflowString(workflow, "test-workflow.md")
	require.NoError(t, err, "parsing should succeed")

	result, err := compiler.CompileToYAML(data, "test-workflow.md")
	require.NoError(t, err, "compilation should succeed")

	assert.NotContains(t, result, "Start DIFC Proxy",
		"compiled workflow should NOT contain proxy start step without guard policy")
	assert.NotContains(t, result, "Stop DIFC Proxy",
		"compiled workflow should NOT contain proxy stop step without guard policy")
}

// TestDIFCProxyNotInjectedWhenIntegrityProxyFalse verifies no proxy injection when
// guard policies are configured but tools.github.integrity-proxy: false is set.
func TestDIFCProxyNotInjectedWhenIntegrityProxyFalse(t *testing.T) {
	workflow := `---
on: issues
engine: copilot
tools:
  github:
    mode: local
    toolsets: [default]
    min-integrity: approved
    integrity-proxy: false
steps:
  - name: Fetch repo data
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: gh issue list
---

# Test Workflow

Test that DIFC proxy is NOT injected when integrity-proxy: false is set.
`
	compiler := NewCompiler()
	data, err := compiler.ParseWorkflowString(workflow, "test-workflow.md")
	require.NoError(t, err, "parsing should succeed")

	result, err := compiler.CompileToYAML(data, "test-workflow.md")
	require.NoError(t, err, "compilation should succeed")

	assert.NotContains(t, result, "Start DIFC Proxy",
		"compiled workflow should NOT contain proxy start step without guard policy")
	assert.NotContains(t, result, "Stop DIFC Proxy",
		"compiled workflow should NOT contain proxy stop step without guard policy")
}

// TestHasDIFCGuardsConfigured verifies the base guard policy check.
func TestHasDIFCGuardsConfigured(t *testing.T) {
	tests := []struct {
		name     string
		data     *WorkflowData
		expected bool
	}{
		{
			name:     "nil data",
			data:     nil,
			expected: false,
		},
		{
			name:     "no github tool",
			data:     &WorkflowData{Tools: map[string]any{}},
			expected: false,
		},
		{
			name: "github tool without guard policy",
			data: &WorkflowData{
				Tools: map[string]any{
					"github": map[string]any{"toolsets": []string{"default"}},
				},
			},
			expected: false,
		},
		{
			name: "github tool with min-integrity (enabled by default)",
			data: &WorkflowData{
				Tools: map[string]any{
					"github": map[string]any{"min-integrity": "approved"},
				},
			},
			expected: true,
		},
		{
			name: "github tool with min-integrity and integrity-proxy: true",
			data: &WorkflowData{
				Tools: map[string]any{
					"github": map[string]any{
						"min-integrity":   "approved",
						"integrity-proxy": true,
					},
				},
			},
			expected: true,
		},
		{
			name: "github tool with min-integrity and integrity-proxy: false",
			data: &WorkflowData{
				Tools: map[string]any{
					"github": map[string]any{
						"min-integrity":   "approved",
						"integrity-proxy": false,
					},
				},
			},
			expected: false,
		},
		{
			name: "github tool with allowed-repos and min-integrity (enabled by default)",
			data: &WorkflowData{
				Tools: map[string]any{
					"github": map[string]any{
						"allowed-repos": "all",
						"min-integrity": "merged",
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasDIFCGuardsConfigured(tt.data)
			assert.Equal(t, tt.expected, got, "hasDIFCGuardsConfigured: %s", tt.name)
		})
	}
}

// TestProxyEnvVars verifies that all expected proxy routing env vars are returned.
func TestProxyEnvVars(t *testing.T) {
	vars := proxyEnvVars()

	require.NotEmpty(t, vars, "proxyEnvVars should return a non-empty map")
	// GH_HOST should use the identity host expression, not the proxy address.
	assert.Equal(t, "${{ env.GH_HOST || 'github.com' }}", vars["GH_HOST"], "GH_HOST should use the identity host from configure_gh_for_ghe.sh, not the proxy address")
	assert.Equal(t, "${{ github.repository }}", vars["GH_REPO"], "GH_REPO should reference github.repository")
	assert.Equal(t, "https://localhost:18443/api/v3", vars["GITHUB_API_URL"], "GITHUB_API_URL should point to proxy")
	assert.Equal(t, "https://localhost:18443/api/graphql", vars["GITHUB_GRAPHQL_URL"], "GITHUB_GRAPHQL_URL should point to proxy")
	assert.Equal(t, "/tmp/gh-aw/proxy-logs/proxy-tls/ca.crt", vars["NODE_EXTRA_CA_CERTS"], "NODE_EXTRA_CA_CERTS should be the proxy CA cert")
	assert.Len(t, vars, 5, "proxyEnvVars should return exactly 5 vars")
}

// TestInjectProxyEnvIntoCustomSteps verifies that proxy env vars are injected
// into each custom step as step-level env, preserving existing env vars.
func TestInjectProxyEnvIntoCustomSteps(t *testing.T) {
	tests := []struct {
		name             string
		customSteps      string
		expectedContains []string
		expectedAbsent   []string
		desc             string
	}{
		{
			name:        "empty string returns empty",
			customSteps: "",
			desc:        "empty input should return empty output",
		},
		{
			name:        "step without env gets proxy env block added",
			customSteps: "steps:\n- name: Step with no env\n  run: echo hello\n",
			expectedContains: []string{
				"GH_HOST: ${{ env.GH_HOST || 'github.com' }}",
				"GH_REPO: ${{ github.repository }}",
				"GITHUB_API_URL: https://localhost:18443/api/v3",
				"GITHUB_GRAPHQL_URL: https://localhost:18443/api/graphql",
				"NODE_EXTRA_CA_CERTS: /tmp/gh-aw/proxy-logs/proxy-tls/ca.crt",
			},
			desc: "step without env should get proxy env block added",
		},
		{
			name:        "step with existing env preserves existing vars",
			customSteps: "steps:\n- name: Step with env\n  env:\n    GH_TOKEN: ${{ github.token }}\n  run: gh issue list\n",
			expectedContains: []string{
				"GH_TOKEN: ${{ github.token }}",
				"GH_HOST: ${{ env.GH_HOST || 'github.com' }}",
				"GH_REPO: ${{ github.repository }}",
				"GITHUB_API_URL: https://localhost:18443/api/v3",
				"GITHUB_GRAPHQL_URL: https://localhost:18443/api/graphql",
				"NODE_EXTRA_CA_CERTS: /tmp/gh-aw/proxy-logs/proxy-tls/ca.crt",
			},
			desc: "existing env var GH_TOKEN should be preserved alongside proxy vars",
		},
		{
			name:        "multiple steps each get proxy env",
			customSteps: "steps:\n- name: Step 1\n  run: echo one\n- name: Step 2\n  env:\n    MY_VAR: value\n  run: echo two\n",
			expectedContains: []string{
				"name: Step 1",
				"name: Step 2",
				"MY_VAR: value",
				"GH_HOST: ${{ env.GH_HOST || 'github.com' }}",
				"GH_REPO: ${{ github.repository }}",
			},
			desc: "all steps should have proxy env injected",
		},
		{
			name:        "uses step gets proxy env",
			customSteps: "steps:\n- name: Checkout\n  uses: actions/checkout@v4\n  with:\n    token: ${{ github.token }}\n",
			expectedContains: []string{
				"uses: actions/checkout@v4",
				"GH_HOST: ${{ env.GH_HOST || 'github.com' }}",
				"GH_REPO: ${{ github.repository }}",
			},
			desc: "uses: steps should also get proxy env injected",
		},
		{
			name:        "multiline run is preserved",
			customSteps: "steps:\n- name: Complex step\n  env:\n    GH_TOKEN: ${{ github.token }}\n  run: |-\n    cmd1\n    cmd2\n    cmd3\n",
			expectedContains: []string{
				"cmd1",
				"cmd2",
				"cmd3",
				"GH_TOKEN: ${{ github.token }}",
				"GH_HOST: ${{ env.GH_HOST || 'github.com' }}",
			},
			desc: "multiline run content should be preserved after injection",
		},
		{
			// Pinned actions carry an inline version comment ("# v4") that is stripped
			// by YAML unmarshaling.  injectProxyEnvIntoCustomSteps must extract these
			// comments before parsing and re-apply them after so that the compiled lock
			// file retains "uses: actions/checkout@sha # v4" and gh-aw-manifest can
			// record "version":"v4" instead of falling back to the bare SHA.
			// The uses value must also remain unquoted (the YAML marshaller quotes
			// strings that contain "#", but GitHub Actions requires bare values).
			name: "uses version comments are preserved and unquoted",
			customSteps: "steps:\n" +
				"- name: Upload artifacts\n" +
				"  uses: actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7\n" +
				"  with:\n" +
				"    name: output\n" +
				"    path: /tmp/output\n",
			expectedContains: []string{
				"uses: actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7",
				"GH_HOST: ${{ env.GH_HOST || 'github.com' }}",
				"GH_REPO: ${{ github.repository }}",
			},
			expectedAbsent: []string{
				// Must not be quoted: 'uses: "actions/upload-artifact@sha # v7"'
				`uses: "actions/upload-artifact@`,
			},
			desc: "uses version comment must survive YAML round-trip and remain unquoted",
		},
		{
			// Step fields should follow constants.PriorityStepFields ordering
			// (name/uses before env) so that lock-file diffs are stable and
			// reviewers see the step identity before the injected env block.
			name: "step fields are ordered with name before env",
			customSteps: "steps:\n" +
				"- name: Run script\n" +
				"  run: echo hello\n",
			expectedContains: []string{
				"name: Run script",
				"GH_HOST: ${{ env.GH_HOST || 'github.com' }}",
			},
			desc: "name field should appear before env in the output",
		},
		{
			// Proxy routing vars must take precedence over user-defined values for the
			// same keys so that traffic is always routed through the proxy. Non-routing
			// vars (e.g. GH_TOKEN) are preserved. This behavior is normative in ADR-26322.
			name: "conflicting proxy routing env vars are overwritten by proxy values",
			customSteps: "steps:\n" +
				"- name: Step with conflicting proxy env\n" +
				"  env:\n" +
				"    GH_TOKEN: ${{ github.token }}\n" +
				"    GH_HOST: example.com\n" +
				"    GITHUB_API_URL: https://example.com/api/v3\n" +
				"  run: gh issue list\n",
			expectedContains: []string{
				"GH_TOKEN: ${{ github.token }}",
				"GH_HOST: ${{ env.GH_HOST || 'github.com' }}",
				"GITHUB_API_URL: https://localhost:18443/api/v3",
				"GH_REPO: ${{ github.repository }}",
				"GITHUB_GRAPHQL_URL: https://localhost:18443/api/graphql",
				"NODE_EXTRA_CA_CERTS: /tmp/gh-aw/proxy-logs/proxy-tls/ca.crt",
			},
			expectedAbsent: []string{
				"GH_HOST: example.com",
				"GITHUB_API_URL: https://example.com/api/v3",
			},
			desc: "proxy routing env vars should take precedence over conflicting custom values",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := injectProxyEnvIntoCustomSteps(tt.customSteps)

			if tt.customSteps == "" {
				assert.Empty(t, result, "empty input should produce empty output: %s", tt.desc)
				return
			}

			require.NotEmpty(t, result, "result should not be empty: %s", tt.desc)

			for _, s := range tt.expectedContains {
				assert.Contains(t, result, s, "result should contain %q: %s", s, tt.desc)
			}
			for _, s := range tt.expectedAbsent {
				assert.NotContains(t, result, s, "result should NOT contain %q: %s", s, tt.desc)
			}

			// Result should still start with "steps:" so addCustomStepsAsIs can process it
			assert.True(t, strings.HasPrefix(result, "steps:"), "result should start with 'steps:': %s", tt.desc)

			// For the ordering test: verify name appears before env in the output.
			if tt.name == "step fields are ordered with name before env" {
				nameIdx := strings.Index(result, "name:")
				envIdx := strings.Index(result, "env:")
				if nameIdx != -1 && envIdx != -1 {
					assert.Less(t, nameIdx, envIdx, "name field should appear before env field: %s", tt.desc)
				}
			}
		})
	}
}

// TestBuildStartCliProxyStepYAML verifies that the CLI proxy step always emits
// CLI_PROXY_POLICY, using a visibility-aware default when no guard policy is
// configured in the frontmatter.
func TestBuildStartCliProxyStepYAML(t *testing.T) {
	c := &Compiler{}

	t.Run("uses the GitHub App token in gh-proxy mode", func(t *testing.T) {
		data := &WorkflowData{
			Tools: map[string]any{
				"github": map[string]any{"mode": "gh-proxy"},
			},
			ParsedTools: &Tools{
				GitHub: &GitHubToolConfig{
					Mode: GitHubMCPModeGHProxy,
					GitHubApp: &GitHubAppConfig{
						AppID:      "${{ vars.APP_ID }}",
						PrivateKey: "${{ secrets.APP_PRIVATE_KEY }}",
					},
				},
			},
		}

		result := c.buildStartCliProxyStepYAML(data)
		assert.Contains(t, result, "GH_TOKEN: ${{ steps.github-mcp-app-token.outputs.token }}")
		assert.NotContains(t, result, "secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN")
		assert.NotContains(t, result, "secrets.GITHUB_TOKEN")
	})

	t.Run("preserves explicit github-token without an App", func(t *testing.T) {
		data := &WorkflowData{
			Tools: map[string]any{
				"github": map[string]any{
					"mode":         "gh-proxy",
					"github-token": "${{ secrets.CUSTOM_GITHUB_TOKEN }}",
				},
			},
			ParsedTools: &Tools{
				GitHub: &GitHubToolConfig{Mode: GitHubMCPModeGHProxy},
			},
		}

		result := c.buildStartCliProxyStepYAML(data)
		assert.Contains(t, result, "GH_TOKEN: ${{ secrets.CUSTOM_GITHUB_TOKEN }}")
		assert.NotContains(t, result, "steps.github-mcp-app-token.outputs.token")
	})

	t.Run("preserves default token fallback without an App", func(t *testing.T) {
		data := &WorkflowData{
			Tools: map[string]any{
				"github": map[string]any{"mode": "gh-proxy"},
			},
			ParsedTools: &Tools{
				GitHub: &GitHubToolConfig{Mode: GitHubMCPModeGHProxy},
			},
		}

		result := c.buildStartCliProxyStepYAML(data)
		assert.Contains(t, result, "GH_TOKEN: ${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}")
		assert.NotContains(t, result, "steps.github-mcp-app-token.outputs.token")
	})

	t.Run("emits visibility-aware default policy when no guard policy is configured", func(t *testing.T) {
		data := &WorkflowData{
			Tools: map[string]any{
				"github": map[string]any{"toolsets": []string{"default"}},
			},
		}

		result := c.buildStartCliProxyStepYAML(data)
		require.NotEmpty(t, result, "should emit CLI proxy step even without guard policy")
		assert.Contains(t, result, "CLI_PROXY_POLICY", "should always emit CLI_PROXY_POLICY")
		assert.Contains(t, result, `"allow-only"`, "default policy should contain allow-only")
		// Default policy references step output for repos (visibility-aware at runtime)
		assert.Contains(t, result, `steps.determine-automatic-lockdown.outputs.repos`, "default policy should reference step output for repos")
		assert.Contains(t, result, `steps.determine-automatic-lockdown.outputs.min_integrity`, "default policy should reference step output for min-integrity")
		// Should NOT contain hardcoded static repos value
		assert.NotContains(t, result, `"repos":"all"`, "default policy should not have hardcoded repos")
	})

	t.Run("emits visibility-aware default policy when github tool is nil", func(t *testing.T) {
		data := &WorkflowData{
			Tools: map[string]any{},
		}

		result := c.buildStartCliProxyStepYAML(data)
		require.NotEmpty(t, result, "should emit CLI proxy step even without github tool")
		assert.Contains(t, result, "CLI_PROXY_POLICY", "should always emit CLI_PROXY_POLICY")
		assert.Contains(t, result, `steps.determine-automatic-lockdown.outputs.repos`, "default policy should reference step output for repos")
		assert.Contains(t, result, `steps.determine-automatic-lockdown.outputs.min_integrity`, "default policy should reference step output for min-integrity")
	})

	t.Run("uses configured guard policy when present", func(t *testing.T) {
		data := &WorkflowData{
			Tools: map[string]any{
				"github": map[string]any{
					"min-integrity": "approved",
					"allowed-repos": "owner/*",
				},
			},
		}

		result := c.buildStartCliProxyStepYAML(data)
		require.NotEmpty(t, result, "should emit CLI proxy step")
		assert.Contains(t, result, "CLI_PROXY_POLICY", "should emit CLI_PROXY_POLICY")
		assert.Contains(t, result, `"min-integrity":"approved"`, "should use configured min-integrity")
		assert.Contains(t, result, `"repos":"owner/*"`, "should use configured repos")
	})

	t.Run("uses runtime repos default when repos is omitted but min-integrity is configured", func(t *testing.T) {
		data := &WorkflowData{
			Tools: map[string]any{
				"github": map[string]any{
					"min-integrity": "approved",
				},
			},
		}

		result := c.buildStartCliProxyStepYAML(data)
		require.NotEmpty(t, result, "should emit CLI proxy step")
		assert.Contains(t, result, `"min-integrity":"approved"`, "should preserve configured min-integrity")
		assert.Contains(t, result, `steps.determine-automatic-lockdown.outputs.repos`, "should use runtime repos output when repos is omitted")
		assert.NotContains(t, result, `"repos":"all"`, "should not hardcode repos=all when repos is omitted")
	})

	t.Run("emits correct step structure", func(t *testing.T) {
		data := &WorkflowData{
			Tools: map[string]any{
				"github": map[string]any{"toolsets": []string{"default"}},
			},
		}

		result := c.buildStartCliProxyStepYAML(data)
		assert.Contains(t, result, "name: Start CLI Proxy", "should have correct step name")
		assert.Contains(t, result, "GH_TOKEN:", "should include GH_TOKEN")
		assertProxyCuratedUpstreamEnv(t, result)
		assert.Contains(t, result, "CLI_PROXY_IMAGE:", "should include CLI_PROXY_IMAGE")
		assert.Contains(t, result, "start_cli_proxy.sh", "should reference the start script")
	})
}

func TestGitHubMCPAppTokenFailsClosedForGhProxy(t *testing.T) {
	app := &GitHubAppConfig{
		AppID:           "${{ vars.APP_ID }}",
		PrivateKey:      "${{ secrets.APP_PRIVATE_KEY }}",
		IgnoreIfMissing: true,
	}
	data := &WorkflowData{
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true},
		},
		Tools: map[string]any{
			"github": map[string]any{"mode": "gh-proxy"},
		},
		ParsedTools: &Tools{
			GitHub: &GitHubToolConfig{
				Mode:      GitHubMCPModeGHProxy,
				GitHubApp: app,
			},
		},
	}

	steps := (&Compiler{}).generateGitHubMCPAppTokenMintingSteps(data)
	require.NotEmpty(t, steps)
	stepYAML := strings.Join(steps, "")
	assert.NotContains(t, stepYAML, "if: ${{")
	assert.Contains(t, stepYAML, "id: github-mcp-app-token")
	assert.True(t, app.IgnoreIfMissing, "proxy-specific failure behavior must not mutate parsed configuration")

	assert.False(t, isGitHubCLIModeEnabled(&WorkflowData{
		Features: map[string]any{"cli-proxy": true},
	}), "the removed features.cli-proxy flag must not enable GitHub CLI mode")

	data.NetworkPermissions.Firewall.Enabled = false
	disabledSteps := (&Compiler{}).generateGitHubMCPAppTokenMintingSteps(data)
	assert.Contains(t, strings.Join(disabledSteps, ""), "if: ${{", "skip fail-closed override when no CLI proxy starts")
}

// TestResolveProxyContainerImage verifies that the helper builds the correct container
// image reference from the gateway config, falling back to the default version when
// no version is specified.
func TestResolveProxyContainerImage(t *testing.T) {
	tests := []struct {
		name     string
		config   *MCPGatewayRuntimeConfig
		expected string
	}{
		{
			name: "uses default version when version is empty",
			config: &MCPGatewayRuntimeConfig{
				Container: constants.DefaultMCPGatewayContainer,
				Version:   "",
			},
			expected: constants.DefaultMCPGatewayContainer + ":" + string(constants.DefaultMCPGatewayVersion),
		},
		{
			name: "uses explicit version when set",
			config: &MCPGatewayRuntimeConfig{
				Container: constants.DefaultMCPGatewayContainer,
				Version:   "v1.2.3",
			},
			expected: constants.DefaultMCPGatewayContainer + ":v1.2.3",
		},
		{
			name: "custom container with default version fallback",
			config: &MCPGatewayRuntimeConfig{
				Container: "ghcr.io/myorg/my-proxy",
				Version:   "",
			},
			expected: "ghcr.io/myorg/my-proxy:" + string(constants.DefaultMCPGatewayVersion),
		},
		{
			name: "custom container with explicit version",
			config: &MCPGatewayRuntimeConfig{
				Container: "ghcr.io/myorg/my-proxy",
				Version:   "latest",
			},
			expected: "ghcr.io/myorg/my-proxy:latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveProxyContainerImage(tt.config)
			assert.Equal(t, tt.expected, got, "resolveProxyContainerImage(%+v)", tt.config)
		})
	}
}

// TestIsCliProxyNeeded_IntegrityReactionsImplicitEnable verifies that the CLI proxy
// is implicitly enabled when the integrity-reactions feature flag is set, even without
// an explicit cli-proxy feature flag.
func TestIsCliProxyNeeded_IntegrityReactionsImplicitEnable(t *testing.T) {
	awfVersion := "0.25.20"

	tests := []struct {
		name     string
		data     *WorkflowData
		expected bool
		desc     string
	}{
		{
			name: "integrity-reactions enables cli proxy implicitly",
			data: &WorkflowData{
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{
						Enabled: true,
						Version: awfVersion,
					},
				},
				Features: map[string]any{"integrity-reactions": true},
			},
			expected: true,
			desc:     "integrity-reactions should implicitly enable the CLI proxy",
		},
		{
			name: "gh-proxy mode and integrity reactions enabled",
			data: &WorkflowData{
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{
						Enabled: true,
						Version: awfVersion,
					},
				},
				Features: map[string]any{"integrity-reactions": true},
				Tools: map[string]any{
					"github": map[string]any{"mode": "gh-proxy"},
				},
			},
			expected: true,
			desc:     "gh-proxy mode and integrity reactions should enable the CLI proxy",
		},
		{
			name: "tools.github.mode local disables cli proxy",
			data: &WorkflowData{
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{
						Enabled: true,
						Version: awfVersion,
					},
				},
				Tools: map[string]any{
					"github": map[string]any{
						"mode": "local",
					},
				},
			},
			expected: false,
			desc:     "explicit tools.github.mode=local should disable cli proxy",
		},
		{
			name: "tools.github.mode gh-proxy enables cli proxy without legacy feature",
			data: &WorkflowData{
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{
						Enabled: true,
						Version: awfVersion,
					},
				},
				Tools: map[string]any{
					"github": map[string]any{
						"mode": "gh-proxy",
					},
				},
			},
			expected: true,
			desc:     "explicit tools.github.mode=gh-proxy should enable cli proxy without legacy feature",
		},
		{
			name: "neither flag set",
			data: &WorkflowData{
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{
						Enabled: true,
						Version: awfVersion,
					},
				},
				Features: map[string]any{},
			},
			expected: false,
			desc:     "no feature flags should not enable the CLI proxy",
		},
		{
			name: "integrity-reactions without firewall",
			data: &WorkflowData{
				Features: map[string]any{"integrity-reactions": true},
			},
			expected: false,
			desc:     "integrity-reactions without firewall should not enable the CLI proxy",
		},
		{
			name: "integrity-reactions with old AWF version",
			data: &WorkflowData{
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{
						Enabled: true,
						Version: "v0.25.16",
					},
				},
				Features: map[string]any{"integrity-reactions": true},
			},
			expected: false,
			desc:     "integrity-reactions with old AWF version should not enable the CLI proxy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isCliProxyNeeded(tt.data)
			assert.Equal(t, tt.expected, got, tt.desc)
		})
	}
}
