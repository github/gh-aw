//go:build !integration

package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveAWFContainerAgentTimeoutMinutes(t *testing.T) {
	defaultFallback := int(constants.DefaultAgenticWorkflowTimeout / time.Minute)

	t.Run("uses numeric timeout-minutes when provided", func(t *testing.T) {
		t.Setenv(compilerenv.DefaultTimeoutMinutes, "")
		got := resolveAWFContainerAgentTimeoutMinutes(&WorkflowData{TimeoutMinutes: "timeout-minutes: 30"})
		assert.Equal(t, 30, got)
	})

	t.Run("falls back to default when timeout-minutes is omitted", func(t *testing.T) {
		t.Setenv(compilerenv.DefaultTimeoutMinutes, "")
		tests := []struct {
			name string
			data *WorkflowData
		}{
			{name: "nil workflow data", data: nil},
			{name: "empty timeout", data: &WorkflowData{}},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := resolveAWFContainerAgentTimeoutMinutes(tt.data)
				assert.Equal(t, defaultFallback, got)
			})
		}
	})

	t.Run("omits agentTimeout when timeout-minutes is an expression", func(t *testing.T) {
		t.Setenv(compilerenv.DefaultTimeoutMinutes, "45")
		// An expression cannot be evaluated at compile time, so no fixed inner
		// timeout may override the expression-backed step timeout.
		got := resolveAWFContainerAgentTimeoutMinutes(&WorkflowData{TimeoutMinutes: "timeout-minutes: ${{ inputs.timeout-minutes }}"})
		assert.Zero(t, got)
	})

	t.Run("uses GH_AW_DEFAULT_TIMEOUT_MINUTES override when timeout-minutes is omitted", func(t *testing.T) {
		t.Setenv(compilerenv.DefaultTimeoutMinutes, "45")
		got := resolveAWFContainerAgentTimeoutMinutes(&WorkflowData{})
		assert.Equal(t, 45, got)
	})
}

// TestBuildAWFConfigJSON verifies that BuildAWFConfigJSON produces a valid JSON config
// that contains the expected network, apiProxy, and container fields.
func TestBuildAWFConfigJSON(t *testing.T) {
	t.Run("basic config with allowed domains and API proxy enabled", func(t *testing.T) {
		// Clear any ambient env override so the assertion below tests the built-in default.
		t.Setenv(compilerenv.DefaultMaxTurnCacheMisses, "")
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com,api.github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err, "BuildAWFConfigJSON should not return an error")

		// Must be valid JSON
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(jsonStr), &parsed), "result must be valid JSON")

		// Schema reference
		assert.Contains(t, jsonStr, "$schema", "should include $schema reference")

		// Network section with allowDomains
		assert.Contains(t, jsonStr, `"allowDomains"`, "should include allowDomains")
		assert.Contains(t, jsonStr, "github.com", "should include github.com in allowDomains")
		assert.Contains(t, jsonStr, "api.github.com", "should include api.github.com in allowDomains")

		// apiProxy section with enabled: true
		assert.Contains(t, jsonStr, `"apiProxy"`, "should include apiProxy section")
		assert.Contains(t, jsonStr, `"enabled":true`, "apiProxy should be enabled")
		assert.Contains(t, jsonStr, fmt.Sprintf(`"maxRuns":%d`, constants.DefaultMaxRuns), "apiProxy should emit default maxRuns")
		assert.Contains(t, jsonStr, fmt.Sprintf(`"maxCacheMisses":%d`, constants.DefaultMaxTurnCacheMisses), "apiProxy should emit default maxCacheMisses")
		assert.NotContains(t, jsonStr, `"maxEffectiveTokens"`, "apiProxy should omit maxEffectiveTokens when unset")

		// container.imageTag
		assert.Contains(t, jsonStr, `"imageTag"`, "should include imageTag")

		// logging section
		assert.Contains(t, jsonStr, `"logging"`, "should include logging section")
		assert.Contains(t, jsonStr, `"proxyLogsDir":"/tmp/gh-aw/sandbox/firewall/logs"`, "should include proxyLogsDir")
		assert.Contains(t, jsonStr, `"auditDir":"/tmp/gh-aw/sandbox/firewall/audit"`, "should include auditDir")
	})

	t.Run("platform config is omitted when sandbox agent is disabled", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Type:     SandboxTypeAWF,
						Platform: "ghes",
						Disabled: true,
					},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.NotContains(t, jsonStr, `"platform":`, "platform should be absent when sandbox agent is disabled")
	})

	t.Run("enables sbx egress verification for docker-sbx", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName: "copilot",
			WorkflowData: &WorkflowData{
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{Runtime: AgentRuntimeDockerSbx},
				},
			},
		}
		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"verifySbxEgress":true`)
	})

	t.Run("blocked domains are included in the network section", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Blocked:  []string{"ads.example.com"},
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err, "BuildAWFConfigJSON should not return an error")

		assert.Contains(t, jsonStr, `"blockDomains"`, "should include blockDomains")
		assert.Contains(t, jsonStr, "ads.example.com", "should include the blocked domain")
	})

	t.Run("filesystem allowWrite is included for the cloud-hypervisor runtime", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName: "copilot",
			WorkflowData: &WorkflowData{
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Version: string(constants.AWFCloudHypervisorFilesystemAllowWriteMinVersion)},
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Runtime: AgentRuntimeCloudHypervisor,
						Config: &SandboxRuntimeConfig{
							Filesystem: &SRTFilesystemConfig{
								AllowWrite: []string{"/tmp/gh-aw/agent"},
							},
						},
					},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"filesystem":{"allowWrite":["/tmp/gh-aw/agent"]}`)
		require.NoError(t, validateAWFConfigJSON(jsonStr))
	})

	t.Run("filesystem allowWrite is omitted for an older firewall", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName: "copilot",
			WorkflowData: &WorkflowData{
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Version: "v0.28.4"},
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Runtime: AgentRuntimeCloudHypervisor,
						Config: &SandboxRuntimeConfig{
							Filesystem: &SRTFilesystemConfig{
								AllowWrite: []string{"/tmp/gh-aw/agent"},
							},
						},
					},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.NotContains(t, jsonStr, `"filesystem"`)
	})

	t.Run("filesystem allowWrite is omitted for the compose runtimes", func(t *testing.T) {
		for _, runtime := range []AgentRuntime{"", AgentRuntimeDocker, AgentRuntimeGVisor, AgentRuntimeDockerSbx} {
			config := AWFCommandConfig{
				EngineName: "copilot",
				WorkflowData: &WorkflowData{
					NetworkPermissions: &NetworkPermissions{
						Firewall: &FirewallConfig{Version: string(constants.AWFCloudHypervisorFilesystemAllowWriteMinVersion)},
					},
					SandboxConfig: &SandboxConfig{
						Agent: &AgentSandboxConfig{
							Runtime: runtime,
							Config: &SandboxRuntimeConfig{
								Filesystem: &SRTFilesystemConfig{
									AllowWrite: []string{"/tmp/gh-aw/agent"},
								},
							},
						},
					},
				},
			}

			jsonStr, err := BuildAWFConfigJSON(config)
			require.NoError(t, err)
			assert.NotContains(t, jsonStr, `"filesystem"`,
				"runtime %q must not emit filesystem.allowWrite: AWF narrows its own /tmp/awf-init mount and the agent container fails to start", runtime)
		}
	})

	t.Run("empty filesystem allowWrite is included for the cloud-hypervisor runtime", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName: "copilot",
			WorkflowData: &WorkflowData{
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Version: string(constants.AWFCloudHypervisorFilesystemAllowWriteMinVersion)},
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Runtime: AgentRuntimeCloudHypervisor,
						Config: &SandboxRuntimeConfig{
							Filesystem: &SRTFilesystemConfig{AllowWrite: []string{}},
						},
					},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"filesystem":{"allowWrite":[]}`)
	})

	t.Run("Cloud Hypervisor filesystem allowWrite requires the higher CH minimum version", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName: "copilot",
			WorkflowData: &WorkflowData{
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Version: string(constants.AWFFilesystemAllowWriteMinVersion)},
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Runtime: AgentRuntimeCloudHypervisor,
						Config: &SandboxRuntimeConfig{
							Filesystem: &SRTFilesystemConfig{
								AllowWrite: []string{"/workspace", "/workspace/.awf-home", "/tmp/gh-aw/agent"},
							},
						},
					},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.NotContains(t, jsonStr, `"filesystem"`, "v0.28.5 must not enable Cloud Hypervisor filesystem.allowWrite")

		config.WorkflowData.NetworkPermissions.Firewall.Version = string(constants.AWFCloudHypervisorFilesystemAllowWriteMinVersion)
		jsonStr, err = BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"filesystem":{"allowWrite":["/workspace","/workspace/.awf-home","/tmp/gh-aw/agent"]}`)
		require.NoError(t, validateAWFConfigJSON(jsonStr))
	})

	t.Run("network isolation emits isolation and topologyAttach", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Type: SandboxTypeAWF,
					},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err, "BuildAWFConfigJSON should not return an error")

		assert.Contains(t, jsonStr, `"isolation":true`, "should enable network isolation")
		assert.Contains(t, jsonStr, `"topologyAttach":["awmg-mcpg"]`, "should attach MCP gateway container to awf-net")
	})

	t.Run("runner topology arc-dind emits runner section without dockerHostPathPrefix", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
				RunnerConfig: &RunnerConfig{Topology: RunnerTopologyArcDind},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err, "BuildAWFConfigJSON should not return an error")

		assert.Contains(t, jsonStr, `"runner":{"topology":"arc-dind"}`, "should emit runner topology")
		assert.NotContains(t, jsonStr, `"dockerHostPathPrefix"`, "should NOT emit dockerHostPathPrefix for arc-dind (sysroot handles path visibility)")
		assert.Contains(t, jsonStr, `"proxyLogsDir":"${RUNNER_TEMP}/gh-aw/sandbox/firewall/logs"`, "should emit arc-dind proxyLogsDir")
		assert.Contains(t, jsonStr, `"auditDir":"${RUNNER_TEMP}/gh-aw/sandbox/firewall/audit"`, "should emit arc-dind auditDir")
	})

	t.Run("runner section omitted when no topology set", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err, "BuildAWFConfigJSON should not return an error")

		assert.NotContains(t, jsonStr, `"runner"`, "should not emit runner section when not configured")
	})

	t.Run("openai API target is included in apiProxy targets", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "codex",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "codex",
					Env: map[string]string{
						"OPENAI_BASE_URL": "https://my-proxy.internal.example.com/v1",
					},
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err, "BuildAWFConfigJSON should not return an error")

		assert.Contains(t, jsonStr, `"targets"`, "should include targets in apiProxy")
		assert.Contains(t, jsonStr, `"openai"`, "should include openai target")
		assert.Contains(t, jsonStr, "my-proxy.internal.example.com", "should include the openai host")
	})

	t.Run("HTTP API target schemes follow the effective AWF version", func(t *testing.T) {
		tests := []struct {
			name         string
			engine       string
			envVar       string
			baseURL      string
			version      string
			provider     string
			expectedHost string
		}{
			{
				name:         "openai preserves HTTP with the default AWF version",
				engine:       "codex",
				envVar:       "OPENAI_BASE_URL",
				baseURL:      "http://openai-gateway.example.com/v1",
				provider:     "openai",
				expectedHost: "http://openai-gateway.example.com",
			},
			{
				name:         "anthropic preserves HTTP at the minimum AWF version",
				engine:       "claude",
				envVar:       "ANTHROPIC_BASE_URL",
				baseURL:      "http://anthropic-gateway.example.com/v1",
				version:      string(constants.AWFHTTPAPITargetMinVersion),
				provider:     "anthropic",
				expectedHost: "http://anthropic-gateway.example.com",
			},
			{
				name:         "preserves an explicit HTTP port",
				engine:       "codex",
				envVar:       "OPENAI_BASE_URL",
				baseURL:      "http://openai-gateway.example.com:8080/v1",
				provider:     "openai",
				expectedHost: "http://openai-gateway.example.com:8080",
			},
			{
				name:         "gemini preserves HTTP with a newer AWF version",
				engine:       "gemini",
				envVar:       "GEMINI_API_BASE_URL",
				baseURL:      "http://gemini-gateway.example.com/v1",
				version:      "v0.28.14",
				provider:     "gemini",
				expectedHost: "http://gemini-gateway.example.com",
			},
			{
				name:         "older AWF versions keep HTTP targets bare",
				engine:       "codex",
				envVar:       "OPENAI_BASE_URL",
				baseURL:      "http://legacy-gateway.example.com/v1",
				version:      "v0.28.12",
				provider:     "openai",
				expectedHost: "legacy-gateway.example.com",
			},
			{
				name:         "HTTPS targets remain bare",
				engine:       "claude",
				envVar:       "ANTHROPIC_BASE_URL",
				baseURL:      "https://secure-gateway.example.com/v1",
				provider:     "anthropic",
				expectedHost: "secure-gateway.example.com",
			},
			{
				name:         "bare targets remain bare",
				engine:       "gemini",
				envVar:       "GEMINI_API_BASE_URL",
				baseURL:      "bare-gateway.example.com/v1",
				provider:     "gemini",
				expectedHost: "bare-gateway.example.com",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				config := AWFCommandConfig{
					EngineName:     tt.engine,
					AllowedDomains: "github.com",
					WorkflowData: &WorkflowData{
						EngineConfig: &EngineConfig{
							ID:  tt.engine,
							Env: map[string]string{tt.envVar: tt.baseURL},
						},
						NetworkPermissions: &NetworkPermissions{
							Firewall: &FirewallConfig{Enabled: true, Version: tt.version},
						},
					},
				}

				jsonStr, err := BuildAWFConfigJSON(config)
				require.NoError(t, err)

				var parsed AWFConfigFile
				require.NoError(t, json.Unmarshal([]byte(jsonStr), &parsed))
				require.NotNil(t, parsed.APIProxy)
				require.Contains(t, parsed.APIProxy.Targets, tt.provider)
				assert.Equal(t, tt.expectedHost, parsed.APIProxy.Targets[tt.provider].Host)
			})
		}
	})

	t.Run("default max-ai-credits is enabled when frontmatter is unset", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "copilot",
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.NotContains(t, jsonStr, `"maxAiCredits"`, "apiProxy should omit maxAiCredits when unset (resolved at runtime via vars expression)")
	})

	t.Run("enterprise default max-ai-credits env var is NOT used at compile time (resolved at action runtime)", func(t *testing.T) {
		t.Setenv(compilerenv.DefaultMaxAICredits, "2k")
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "copilot",
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		// env var is no longer read at compile time; maxAiCredits is deferred to runtime
		assert.NotContains(t, jsonStr, `"maxAiCredits"`, "apiProxy should omit maxAiCredits when unset (env var ignored at compile time)")
	})

	t.Run("frontmatter max-ai-credits takes precedence over runtime default", func(t *testing.T) {
		t.Setenv(compilerenv.DefaultMaxAICredits, "2k")
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID:           "copilot",
					MaxAICredits: 333,
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"maxAiCredits":333`, "apiProxy should bake in frontmatter maxAiCredits (skipping runtime expression)")
	})

	t.Run("default max-turn-cache-misses uses built-in default when unset", func(t *testing.T) {
		// Clear any ambient env override so this test actually exercises the built-in default path.
		t.Setenv(compilerenv.DefaultMaxTurnCacheMisses, "")
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "copilot",
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"maxCacheMisses":5`, "apiProxy should emit built-in maxCacheMisses default when unset")
	})

	t.Run("enterprise default max-turn-cache-misses env var overrides built-in default", func(t *testing.T) {
		t.Setenv(compilerenv.DefaultMaxTurnCacheMisses, "9")
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "copilot",
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"maxCacheMisses":9`, "apiProxy should emit env-managed default maxCacheMisses when frontmatter is unset")
	})

	t.Run("frontmatter max-turn-cache-misses takes precedence over env default", func(t *testing.T) {
		t.Setenv(compilerenv.DefaultMaxTurnCacheMisses, "9")
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID:                 "copilot",
					MaxTurnCacheMisses: 3,
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"maxCacheMisses":3`, "apiProxy should emit frontmatter maxCacheMisses ahead of env default")
	})

	// T-AIC-PR-007: Imported workflow max-ai-credits baked into AWF config JSON when no main
	// frontmatter value is present; imported value takes precedence over the runtime default.
	// The compiler_orchestrator_engine.go applies MergedMaxAICredits to EngineConfig when
	// the main workflow has no max-ai-credits frontmatter — the resulting non-zero MaxAICredits
	// on EngineConfig is treated identically to a direct frontmatter value in BuildAWFConfigJSON.
	t.Run("spec §10.3(2) / T-AIC-PR-007: imported config max-ai-credits baked into AWF config JSON", func(t *testing.T) {
		// Simulate what compiler_orchestrator_engine.go does: set MaxAICredits from imports
		// when the main workflow frontmatter has no max-ai-credits.
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID:           "copilot",
					MaxAICredits: 750, // from imported workflow config (first-wins)
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"maxAiCredits":750`, "apiProxy should bake in imported config maxAiCredits value")
		assert.NotContains(t, jsonStr, `"maxAiCredits":1000`, "should use imported value, not the built-in default")
	})

	t.Run("spec §10.3(2) / T-AIC-PR-007: frontmatter max-ai-credits overrides imported config value", func(t *testing.T) {
		// Both main-workflow frontmatter and imports set max-ai-credits; frontmatter wins.
		// In practice compiler_orchestrator_engine.go skips the import if MaxAICredits != 0.
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID:           "copilot",
					MaxAICredits: 500, // frontmatter wins; imports would have set a different value
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"maxAiCredits":500`, "frontmatter max-ai-credits MUST override any imported config value")
	})

	t.Run("token steering is enabled by default in apiProxy config", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "copilot",
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"enableTokenSteering":true`, "apiProxy should emit enableTokenSteering by default")
	})

	t.Run("token steering can be disabled in the sandbox config", func(t *testing.T) {
		disabled := false
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{TokenSteering: &disabled},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"enableTokenSteering":false`, "apiProxy should emit the sandbox token-steering override")
	})

	t.Run("token steering is disabled when max-ai-credits is negative", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID:           "copilot",
					MaxAICredits: -1,
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.NotContains(t, jsonStr, `"enableTokenSteering"`, "apiProxy should omit enableTokenSteering when max-ai-credits is negative")
		assert.NotContains(t, jsonStr, `"maxAiCredits"`, "apiProxy should omit maxAiCredits when negative (disabled)")
	})

	t.Run("token steering is skipped for unsupported AWF versions", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "copilot",
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{
						Enabled: true,
						Version: "v0.25.43",
					},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.NotContains(t, jsonStr, `"enableTokenSteering"`, "apiProxy should omit enableTokenSteering for unsupported AWF versions")
	})

	t.Run("configured max-runs is emitted in apiProxy config", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID:      "copilot",
					MaxRuns: 37,
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"maxRuns":37`, "apiProxy should emit configured maxRuns")
	})

	t.Run("default max-runs is emitted when not configured", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "copilot",
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, fmt.Sprintf(`"maxRuns":%d`, constants.DefaultMaxRuns), "apiProxy should emit default maxRuns when unset")
	})

	t.Run("anthropic API target is included in apiProxy targets", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "claude",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "claude",
					Env: map[string]string{
						"ANTHROPIC_BASE_URL": "https://corp-gateway.example.com/anthropic",
					},
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)

		assert.Contains(t, jsonStr, `"anthropic"`, "should include anthropic target")
		assert.Contains(t, jsonStr, "corp-gateway.example.com", "should include the anthropic host")
	})

	t.Run("no API targets section when no custom endpoints are configured", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)

		// No custom targets for the default copilot engine
		assert.NotContains(t, jsonStr, `"targets"`, "should not include targets when no custom endpoints")
	})

	t.Run("image tag with digest metadata is included in container section", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)

		assert.Contains(t, jsonStr, `"container"`, "should include container section")
		assert.Contains(t, jsonStr, `"imageTag"`, "should include imageTag in container section")
	})

	t.Run("empty allowed domains produces no network section", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)

		assert.NotContains(t, jsonStr, `"network"`, "should not include network section when no domains")
	})

	t.Run("output is compact valid JSON (no pretty-print indentation)", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)

		// Compact JSON: no leading whitespace inside the object, no newlines.
		// This is important for safe embedding in a printf command.
		assert.NotContains(t, jsonStr, "\n", "JSON output should not contain newlines (must be compact)")
		assert.NotContains(t, jsonStr, "    ", "JSON output should not contain indentation")
	})

	t.Run("github actions expressions preserve && operators in allowDomains", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "${{ env.MCP_ENV == 'staging' && env.MCP_URL_STAGING || env.MCP_URL_PROD }}",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err, "BuildAWFConfigJSON should not return an error")

		assert.Contains(t, jsonStr, "&&", "JSON output should preserve && in GitHub Actions expressions")
		assert.NotContains(t, jsonStr, "\\u0026", "JSON output should not HTML-escape '&' characters")
	})

	t.Run("openai authHeader from frontmatter sandbox.agent.targets is included", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "codex",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "codex"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Targets: map[string]*AgentAPIProxyTargetConfig{
							"openai": {AuthHeader: "api-key"},
						},
					},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"authHeader":"api-key"`, "should include openai authHeader in apiProxy targets")
		assert.Contains(t, jsonStr, `"openai"`, "should include openai target")
		assert.NotContains(t, jsonStr, `"host":""`, "should not emit empty host when only authHeader is set")
	})

	t.Run("anthropic authHeader from frontmatter sandbox.agent.targets is included", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "claude",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "claude"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Targets: map[string]*AgentAPIProxyTargetConfig{
							"anthropic": {AuthHeader: "api-key"},
						},
					},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"authHeader":"api-key"`, "should include anthropic authHeader in apiProxy targets")
		assert.Contains(t, jsonStr, `"anthropic"`, "should include anthropic target")
		assert.NotContains(t, jsonStr, `"host":""`, "should not emit empty host when only authHeader is set")
	})

	t.Run("authHeader coexists with host from engine.env", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "codex",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "codex",
					Env: map[string]string{
						"OPENAI_BASE_URL": "https://azure-openai.internal/v1",
					},
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Targets: map[string]*AgentAPIProxyTargetConfig{
							"openai": {AuthHeader: "api-key"},
						},
					},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, "azure-openai.internal", "should include host from OPENAI_BASE_URL")
		assert.Contains(t, jsonStr, `"authHeader":"api-key"`, "should include authHeader alongside host")
	})

	t.Run("authHeader is omitted when not configured in frontmatter", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "codex",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "codex",
					Env: map[string]string{
						"OPENAI_BASE_URL": "https://my-proxy.internal.example.com/v1",
					},
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.NotContains(t, jsonStr, `"authHeader"`, "authHeader should be absent when not configured")
	})

	t.Run("copilot extraHeaders from frontmatter sandbox.agent.targets.copilot are included", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Targets: map[string]*AgentAPIProxyTargetConfig{
							"copilot": {
								ExtraHeaders: map[string]string{
									"x-openrouter-title": "my-workflow",
									"http-referer":       "https://github.com/org/repo",
								},
							},
						},
					},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"copilot"`, "should include copilot target")
		assert.Contains(t, jsonStr, `"extraHeaders"`, "should include extraHeaders in copilot target")
		assert.Contains(t, jsonStr, `"x-openrouter-title"`, "should include x-openrouter-title header key")
		assert.Contains(t, jsonStr, `"my-workflow"`, "should include header value")
	})

	t.Run("copilot extraBodyFields from frontmatter sandbox.agent.targets.copilot are included", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Targets: map[string]*AgentAPIProxyTargetConfig{
							"copilot": {
								ExtraBodyFields: map[string]string{
									"custom-field": "custom-value",
								},
							},
						},
					},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"extraBodyFields"`, "should include extraBodyFields in copilot target")
		assert.Contains(t, jsonStr, `"custom-field"`, "should include body field key")
		assert.Contains(t, jsonStr, `"custom-value"`, "should include body field value")
	})

	t.Run("copilot sessionId from frontmatter sandbox.agent.targets.copilot is included", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Targets: map[string]*AgentAPIProxyTargetConfig{
							"copilot": {
								SessionId: "${{ github.run_id }}",
							},
						},
					},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"sessionId"`, "should include sessionId in copilot target")
		assert.Contains(t, jsonStr, `"${{ github.run_id }}"`, "should include sessionId value")
	})

	t.Run("copilot authHeader from frontmatter sandbox.agent.targets.copilot is included", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Targets: map[string]*AgentAPIProxyTargetConfig{
							"copilot": {
								AuthHeader: "api-key",
							},
						},
					},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"copilot"`, "should include copilot target")
		assert.Contains(t, jsonStr, `"authHeader":"api-key"`, "should include copilot authHeader in apiProxy targets")
		assert.NotContains(t, jsonStr, `"host":""`, "should not emit empty host when only authHeader is set")
	})

	t.Run("copilot BYOK fields coexist with host from GetCopilotAPITarget", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID:        "copilot",
					APITarget: "copilot-gateway.internal",
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Targets: map[string]*AgentAPIProxyTargetConfig{
							"copilot": {
								ExtraHeaders: map[string]string{
									"x-title": "my-workflow",
								},
								SessionId: "run-123",
							},
						},
					},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, "copilot-gateway.internal", "should include host from api-target")
		assert.Contains(t, jsonStr, `"extraHeaders"`, "should include extraHeaders alongside host")
		assert.Contains(t, jsonStr, `"sessionId"`, "should include sessionId alongside host")
	})

	t.Run("copilot BYOK fields create entry even without host override", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Targets: map[string]*AgentAPIProxyTargetConfig{
							"copilot": {
								ExtraHeaders: map[string]string{
									"x-title": "my-workflow",
								},
							},
						},
					},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"copilot"`, "should include copilot target entry even without host")
		assert.Contains(t, jsonStr, `"extraHeaders"`, "should include extraHeaders")
		assert.NotContains(t, jsonStr, `"host":""`, "should not emit empty host")
	})

	t.Run("copilot BYOK fields are absent when not configured", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.NotContains(t, jsonStr, `"extraHeaders"`, "extraHeaders should be absent when not configured")
		assert.NotContains(t, jsonStr, `"extraBodyFields"`, "extraBodyFields should be absent when not configured")
		assert.NotContains(t, jsonStr, `"sessionId"`, "sessionId should be absent when not configured")
	})

	t.Run("sandbox agent platform is emitted in awf platform config", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Type:     SandboxTypeAWF,
						Platform: "ghes",
					},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"platform":{"type":"ghes"}`, "should include AWF platform.type when configured")
	})

	t.Run("platform config is omitted when sandbox agent platform is not configured", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Type: SandboxTypeAWF,
					},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.NotContains(t, jsonStr, `"platform":`, "platform section should be absent when sandbox.agent.platform is unset")
	})

	t.Run("model-fallback is emitted when enabled is explicitly set to false", func(t *testing.T) {
		disabled := TemplatableBool("false")
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "copilot",
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						ModelFallback: &disabled,
					},
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"modelFallback"`, "apiProxy should emit modelFallback when configured")
		assert.Contains(t, jsonStr, `"enabled":false`, "apiProxy.modelFallback.enabled should be false")
	})

	t.Run("model-fallback is emitted when enabled is explicitly set to true", func(t *testing.T) {
		enabled := TemplatableBool("true")
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "copilot",
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						ModelFallback: &enabled,
					},
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"modelFallback"`, "apiProxy should emit modelFallback when configured")
		assert.Contains(t, jsonStr, `"enabled":true`, "apiProxy.modelFallback.enabled should be true")
	})

	t.Run("model-fallback supports GitHub Actions expressions", func(t *testing.T) {
		expr := TemplatableBool("${{ inputs.model-fallback }}")
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "copilot",
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						ModelFallback: &expr,
					},
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"modelFallback"`, "apiProxy should emit modelFallback when configured")
		assert.Contains(t, jsonStr, `"enabled":"${{ inputs.model-fallback }}"`, "apiProxy.modelFallback.enabled should preserve expressions")
	})

	t.Run("model-fallback is omitted when not configured in sandbox", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "copilot",
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.NotContains(t, jsonStr, `"modelFallback"`, "apiProxy should omit modelFallback when not configured")
	})

	t.Run("model-fallback is disabled by default for a custom anthropic API target", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "claude",
			AllowedDomains: "github.com,openrouter.ai",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "claude",
					Env: map[string]string{
						"ANTHROPIC_BASE_URL": "https://openrouter.ai/api/v1",
					},
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"modelFallback":{"enabled":false}`, "apiProxy should disable modelFallback for custom Anthropic providers")
	})

	t.Run("model-fallback is disabled by default for a custom openai API target", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "codex",
			AllowedDomains: "github.com,llm-router.internal.example.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "codex",
					Env: map[string]string{
						"OPENAI_BASE_URL": "https://llm-router.internal.example.com/v1",
					},
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"modelFallback":{"enabled":false}`, "apiProxy should disable modelFallback for custom OpenAI-compatible providers")
	})

	t.Run("model-fallback is disabled by default for expression-valued custom API targets", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "claude",
			AllowedDomains: "github.com,openrouter.ai",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "claude",
					Env: map[string]string{
						"ANTHROPIC_BASE_URL": "${{ vars.LLM_URL }}",
					},
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"modelFallback":{"enabled":false}`, "apiProxy should disable modelFallback for expression-valued custom API targets")
	})

	t.Run("explicit model-fallback overrides the custom API target default", func(t *testing.T) {
		enabled := TemplatableBool("true")
		config := AWFCommandConfig{
			EngineName:     "claude",
			AllowedDomains: "github.com,openrouter.ai",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "claude",
					Env: map[string]string{
						"ANTHROPIC_BASE_URL": "https://openrouter.ai/api/v1",
					},
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						ModelFallback: &enabled,
					},
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"modelFallback":{"enabled":true}`, "explicit sandbox.agent.model-fallback should win over the custom-target default")
	})

	t.Run("default-ai-credits-pricing is emitted for BYOK self-hosted model at zero cost", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "copilot",
				},
				DefaultAiCreditsPricing: &AiCreditsPricingConfig{
					Input:  0,
					Output: 0,
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"defaultAiCreditsPricing"`, "apiProxy should emit defaultAiCreditsPricing when configured")
		assert.Contains(t, jsonStr, `"input":0`, "apiProxy.defaultAiCreditsPricing.input should be 0")
		assert.Contains(t, jsonStr, `"output":0`, "apiProxy.defaultAiCreditsPricing.output should be 0")
	})

	t.Run("default-ai-credits-pricing is emitted with non-zero rates", func(t *testing.T) {
		cachedInput := 0.3
		cacheWrite := 3.0
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "copilot",
				},
				DefaultAiCreditsPricing: &AiCreditsPricingConfig{
					Input:       3.0,
					Output:      15.0,
					CachedInput: &cachedInput,
					CacheWrite:  &cacheWrite,
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"defaultAiCreditsPricing"`, "apiProxy should emit defaultAiCreditsPricing when configured")
		assert.Contains(t, jsonStr, `"input":3`, "apiProxy.defaultAiCreditsPricing.input should be 3")
		assert.Contains(t, jsonStr, `"output":15`, "apiProxy.defaultAiCreditsPricing.output should be 15")
		assert.Contains(t, jsonStr, `"cachedInput":0.3`, "apiProxy.defaultAiCreditsPricing.cachedInput should be emitted")
		assert.Contains(t, jsonStr, `"cacheWrite":3`, "apiProxy.defaultAiCreditsPricing.cacheWrite should be emitted")
	})

	t.Run("default-ai-credits-pricing is omitted when not configured", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "copilot",
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.NotContains(t, jsonStr, `"defaultAiCreditsPricing"`, "apiProxy should omit defaultAiCreditsPricing when not configured")
	})

	t.Run("models.providers cost overlay is emitted in apiProxy config when AWF supports providers", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "claude",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "claude",
				},
				ModelCosts: map[string]any{
					"providers": map[string]any{
						"anthropic": map[string]any{
							"models": map[string]any{
								"accounts/fireworks/models/minimax-m3": map[string]any{
									"cost": map[string]any{
										"input":       "3e-07",
										"output":      "1.5e-06",
										"cache_read":  "3e-08",
										"cache_write": "3.75e-07",
									},
								},
							},
						},
					},
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true, Version: string(constants.AWFAPIProxyProvidersMinVersion)},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)

		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(jsonStr), &parsed))
		apiProxy, ok := parsed["apiProxy"].(map[string]any)
		require.True(t, ok, "expected apiProxy object")
		providers, ok := apiProxy["providers"].(map[string]any)
		require.True(t, ok, "expected apiProxy.providers object")
		anthropic, ok := providers["anthropic"].(map[string]any)
		require.True(t, ok, "expected anthropic provider")
		models, ok := anthropic["models"].(map[string]any)
		require.True(t, ok, "expected anthropic.models object")
		model, ok := models["accounts/fireworks/models/minimax-m3"].(map[string]any)
		require.True(t, ok, "expected custom model key in providers")
		cost, ok := model["cost"].(map[string]any)
		require.True(t, ok, "expected model cost object")
		assert.Equal(t, "3e-08", cost["cache_read"], "apiProxy.providers should preserve custom cache_read pricing")
	})

	t.Run("models.providers is not emitted when AWF version does not support apiProxy.providers", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "claude",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "claude",
				},
				ModelCosts: map[string]any{
					"providers": map[string]any{
						"anthropic": map[string]any{
							"models": map[string]any{
								"accounts/fireworks/models/minimax-m3": map[string]any{
									"cost": map[string]any{
										"input":  "3e-07",
										"output": "1.5e-06",
									},
								},
							},
						},
					},
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true, Version: "v0.27.41"},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(jsonStr), &parsed))
		apiProxy, ok := parsed["apiProxy"].(map[string]any)
		require.True(t, ok, "expected apiProxy object")
		_, hasProviders := apiProxy["providers"]
		assert.False(t, hasProviders, "apiProxy should omit providers when AWF version does not support it")
	})
}

// TestBuildAWFConfigJSON_APIProxyCACert verifies that sandbox.agent.ca-cert is
// mapped to apiProxy.caCert and gated to AWF versions that support the field.
func TestBuildAWFConfigJSON_APIProxyCACert(t *testing.T) {
	t.Run("apiProxy.caCert is emitted when configured and AWF supports it", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "claude",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "claude"},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						CACert: "/etc/ssl/certs/internal-ca.pem",
					},
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true, Version: string(constants.AWFAPIProxyCACertMinVersion)},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(jsonStr), &parsed))
		apiProxy, ok := parsed["apiProxy"].(map[string]any)
		require.True(t, ok, "expected apiProxy object")
		assert.Equal(t, "/etc/ssl/certs/internal-ca.pem", apiProxy["caCert"], "apiProxy.caCert should be emitted from sandbox.agent.ca-cert")
	})

	t.Run("apiProxy.caCert is not emitted when AWF version does not support it", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "claude",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "claude"},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						CACert: "/etc/ssl/certs/internal-ca.pem",
					},
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true, Version: "v0.28.9"},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(jsonStr), &parsed))
		apiProxy, ok := parsed["apiProxy"].(map[string]any)
		require.True(t, ok, "expected apiProxy object")
		_, hasCACert := apiProxy["caCert"]
		assert.False(t, hasCACert, "apiProxy should omit caCert when AWF version does not support it")
	})

	t.Run("apiProxy.caCert is omitted when not configured", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "claude",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "claude"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.NotContains(t, jsonStr, `"caCert"`, "apiProxy should omit caCert when not configured")
	})
}

func TestExtractModelCostProviders(t *testing.T) {
	t.Run("returns a cloned providers map", func(t *testing.T) {
		workflowData := &WorkflowData{
			ModelCosts: map[string]any{
				"providers": map[string]any{
					"anthropic": map[string]any{"models": map[string]any{}},
				},
			},
		}

		got := extractModelCostProviders(workflowData)
		require.NotNil(t, got)
		got["openai"] = map[string]any{}

		origProviders := workflowData.ModelCosts["providers"].(map[string]any)
		_, mutatedOriginal := origProviders["openai"]
		assert.False(t, mutatedOriginal, "returned providers map should not alias ModelCosts.providers")
	})

	t.Run("returns nil when providers has unexpected type", func(t *testing.T) {
		workflowData := &WorkflowData{
			ModelCosts: map[string]any{
				"providers": map[string]string{"anthropic": "invalid"},
			},
		}
		assert.Nil(t, extractModelCostProviders(workflowData))
	})
}

// TestBuildAWFConfigSchemaURL verifies that buildAWFConfigSchemaURL returns a release-pinned
// URL that tracks the AWF version in use.
func TestBuildAWFConfigSchemaURL(t *testing.T) {
	tests := []struct {
		name           string
		firewallConfig *FirewallConfig
		wantContains   string
		wantURL        string // exact URL match, takes precedence over wantContains when set
	}{
		{
			name:           "nil config uses DefaultFirewallVersion",
			firewallConfig: nil,
			wantContains:   string(constants.DefaultFirewallVersion),
		},
		{
			name:           "empty version uses DefaultFirewallVersion",
			firewallConfig: &FirewallConfig{Enabled: true},
			wantContains:   string(constants.DefaultFirewallVersion),
		},
		{
			name:           "pinned version with v prefix",
			firewallConfig: &FirewallConfig{Enabled: true, Version: "v0.24.0"},
			wantContains:   "v0.24.0",
		},
		{
			name:           "pinned version without v prefix gets v added",
			firewallConfig: &FirewallConfig{Enabled: true, Version: "0.24.0"},
			wantContains:   "v0.24.0",
		},
		{
			name:           "latest version uses /releases/latest/download/ URL",
			firewallConfig: &FirewallConfig{Enabled: true, Version: "latest"},
			wantURL:        "https://github.com/github/gh-aw-firewall/releases/latest/download/awf-config.schema.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := buildAWFConfigSchemaURL(tt.firewallConfig)

			if tt.wantURL != "" {
				assert.Equal(t, tt.wantURL, url, "schema URL should match the expected URL exactly")
				return
			}

			assert.Contains(t, url, tt.wantContains, "schema URL should contain the expected version")
			assert.Contains(t, url, "https://github.com/github/gh-aw-firewall/releases/download/", "schema URL should use the release download path")
			assert.True(t, strings.HasSuffix(url, "awf-config.schema.json"), "schema URL should end with awf-config.schema.json")
		})
	}
}

// TestBuildAWFConfigJSON_SchemaURLIsVersionPinned verifies that the $schema field in the
// generated config uses a release-pinned URL that matches the AWF version in use.
func TestBuildAWFConfigJSON_SchemaURLIsVersionPinned(t *testing.T) {
	t.Run("default version when no version pinned", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)

		expectedVersion := string(constants.DefaultFirewallVersion)
		assert.Contains(t, jsonStr, expectedVersion, "schema URL should contain the default firewall version")
		assert.Contains(t, jsonStr, "releases/download/", "schema URL should use release download path")
		assert.Contains(t, jsonStr, "awf-config.schema.json", "schema URL should reference awf-config.schema.json")
	})

	t.Run("pinned version appears in schema URL", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true, Version: "v0.24.0"},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)

		assert.Contains(t, jsonStr, "v0.24.0", "schema URL should contain the pinned version")
		assert.NotContains(t, jsonStr, string(constants.DefaultFirewallVersion), "schema URL should not contain default version when version is pinned")
	})
}

// TestBuildAWFConfigJSON_DomainDeduplication verifies that duplicate domain entries
// in the comma-separated allowed domains list are removed.
func TestBuildAWFConfigJSON_DomainDeduplication(t *testing.T) {
	config := AWFCommandConfig{
		EngineName:     "copilot",
		AllowedDomains: "github.com,api.github.com,github.com", // github.com duplicated
		WorkflowData: &WorkflowData{
			EngineConfig: &EngineConfig{ID: "copilot"},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true},
			},
		},
	}

	jsonStr, err := BuildAWFConfigJSON(config)
	require.NoError(t, err)

	var parsed struct {
		Network struct {
			AllowDomains []string `json:"allowDomains"`
		} `json:"network"`
	}
	require.NoError(t, json.Unmarshal([]byte(jsonStr), &parsed))

	// github.com should appear exactly once
	count := 0
	for _, d := range parsed.Network.AllowDomains {
		if d == "github.com" {
			count++
		}
	}
	assert.Equal(t, 1, count, "github.com should appear exactly once after deduplication")
}

// TestBuildAWFConfigJSON_SchemaCompliance validates that BuildAWFConfigJSON always produces
// JSON that conforms to the embedded AWF config schema. This test provides coverage for the
// validateAWFConfigJSON path, which is triggered at compile time when --validate is used
// (WorkflowData.ValidateAWFConfig == true). Running it unconditionally here ensures schema
// compliance is verified in CI without the per-compile performance cost.
func TestBuildAWFConfigJSON_SchemaCompliance(t *testing.T) {
	cases := []struct {
		name   string
		config AWFCommandConfig
	}{
		{
			name: "basic config with firewall and allowed domains",
			config: AWFCommandConfig{
				EngineName:     "copilot",
				AllowedDomains: "github.com,api.github.com",
				WorkflowData: &WorkflowData{
					EngineConfig: &EngineConfig{ID: "copilot"},
					NetworkPermissions: &NetworkPermissions{
						Firewall: &FirewallConfig{Enabled: true},
					},
				},
			},
		},
		{
			name: "config without network section",
			config: AWFCommandConfig{
				EngineName: "copilot",
				WorkflowData: &WorkflowData{
					EngineConfig: &EngineConfig{ID: "copilot"},
				},
			},
		},
		{
			name: "config with pinned firewall version",
			config: AWFCommandConfig{
				EngineName:     "copilot",
				AllowedDomains: "github.com",
				WorkflowData: &WorkflowData{
					EngineConfig: &EngineConfig{ID: "copilot"},
					NetworkPermissions: &NetworkPermissions{
						Firewall: &FirewallConfig{Enabled: true, Version: "v0.24.0"},
					},
				},
			},
		},
		{
			name: "config with model-fallback disabled",
			config: AWFCommandConfig{
				EngineName:     "copilot",
				AllowedDomains: "github.com",
				WorkflowData: func() *WorkflowData {
					disabled := TemplatableBool("false")
					return &WorkflowData{
						EngineConfig: &EngineConfig{ID: "copilot"},
						SandboxConfig: &SandboxConfig{
							Agent: &AgentSandboxConfig{
								ModelFallback: &disabled,
							},
						},
						NetworkPermissions: &NetworkPermissions{
							Firewall: &FirewallConfig{Enabled: true},
						},
					}
				}(),
			},
		},
		{
			name: "config with model-fallback expression",
			config: AWFCommandConfig{
				EngineName:     "copilot",
				AllowedDomains: "github.com",
				WorkflowData: func() *WorkflowData {
					expr := TemplatableBool("${{ inputs.model-fallback }}")
					return &WorkflowData{
						EngineConfig: &EngineConfig{ID: "copilot"},
						SandboxConfig: &SandboxConfig{
							Agent: &AgentSandboxConfig{
								ModelFallback: &expr,
							},
						},
						NetworkPermissions: &NetworkPermissions{
							Firewall: &FirewallConfig{Enabled: true},
						},
					}
				}(),
			},
		},
		{
			name: "config with copilot BYOK extra headers and sessionId",
			config: AWFCommandConfig{
				EngineName:     "copilot",
				AllowedDomains: "github.com",
				WorkflowData: &WorkflowData{
					EngineConfig: &EngineConfig{ID: "copilot"},
					NetworkPermissions: &NetworkPermissions{
						Firewall: &FirewallConfig{Enabled: true},
					},
					SandboxConfig: &SandboxConfig{
						Agent: &AgentSandboxConfig{
							Targets: map[string]*AgentAPIProxyTargetConfig{
								"copilot": {
									ExtraHeaders: map[string]string{
										"x-openrouter-title": "my-workflow",
										"http-referer":       "https://github.com/org/repo",
									},
									ExtraBodyFields: map[string]string{
										"custom-field": "custom-value",
									},
									SessionId: "run-12345",
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			jsonStr, err := BuildAWFConfigJSON(tc.config)
			require.NoError(t, err, "BuildAWFConfigJSON should not return an error")
			require.NoError(t, validateAWFConfigJSON(jsonStr),
				"generated AWF config JSON must conform to the embedded schema")
		})
	}
}

// TestValidateAWFConfigJSON_RejectsInvalidJSON verifies that validateAWFConfigJSON is a
// genuine two-sided contract: valid JSON passes and deliberately invalid JSON is rejected.
// This ensures the validator itself is functional after the runtime guard was removed from
// the hot path.
func TestValidateAWFConfigJSON_RejectsInvalidJSON(t *testing.T) {
	cases := []struct {
		name string
		json string
	}{
		{
			name: "network field with wrong type (string instead of object)",
			json: `{"network": "invalid_string_not_object"}`,
		},
		{
			name: "unknown top-level key",
			json: `{"unexpected_key": true}`,
		},
		{
			name: "invalid JSON syntax",
			json: `{not valid json`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAWFConfigJSON(tc.json)
			require.Error(t, err, "validateAWFConfigJSON should reject invalid input: %s", tc.name)
		})
	}
}

func TestValidateAWFConfigJSON_AllowsTemplatableModelFallbackEnabled(t *testing.T) {
	err := validateAWFConfigJSON(`{"apiProxy":{"enabled":true,"modelFallback":{"enabled":"${{ inputs.model-fallback }}"}}}`)
	require.NoError(t, err, "modelFallback.enabled expressions should pass compile-time schema validation")
}

func TestValidateAWFConfigJSON_AllowsMaxTurnCacheMisses(t *testing.T) {
	err := validateAWFConfigJSON(`{"apiProxy":{"enabled":true,"maxCacheMisses":3}}`)
	require.NoError(t, err, "maxCacheMisses should pass compile-time schema validation")
}

func TestValidateAWFConfigJSON_AllowsFilesystemAllowWrite(t *testing.T) {
	err := validateAWFConfigJSON(`{"filesystem":{"allowWrite":[]}}`)
	require.NoError(t, err, "filesystem.allowWrite should pass AWF config schema validation")
}

func TestValidateAWFConfigJSON_AllowsSbxContainerRuntime(t *testing.T) {
	err := validateAWFConfigJSON(`{"container":{"containerRuntime":"sbx"}}`)
	require.NoError(t, err, "container.containerRuntime=sbx should pass compile-time schema validation")
}

func TestValidateAWFConfigJSON_AllowsGVisorContainerRuntime(t *testing.T) {
	err := validateAWFConfigJSON(`{"container":{"containerRuntime":"gvisor"}}`)
	require.NoError(t, err, "container.containerRuntime=gvisor should pass compile-time schema validation")
}

func TestValidateAWFConfigJSON_RejectsUnknownContainerRuntime(t *testing.T) {
	err := validateAWFConfigJSON(`{"container":{"containerRuntime":"runc"}}`)
	require.Error(t, err, "container.containerRuntime must only accept enum values; unknown runtime \"runc\" should be rejected")
}

func TestValidateAWFConfigJSON_AllowsDefaultAiCreditsPricing(t *testing.T) {
	err := validateAWFConfigJSON(`{"apiProxy":{"enabled":true,"maxRuns":500,"defaultAiCreditsPricing":{"input":0,"output":0}}}`)
	require.NoError(t, err, "apiProxy.defaultAiCreditsPricing with zero rates should pass AWF config schema validation")
}

func TestValidateAWFConfigJSON_AllowsDefaultAiCreditsPricingNonZero(t *testing.T) {
	err := validateAWFConfigJSON(`{"apiProxy":{"enabled":true,"maxRuns":500,"defaultAiCreditsPricing":{"input":3,"output":15}}}`)
	require.NoError(t, err, "apiProxy.defaultAiCreditsPricing with non-zero rates should pass AWF config schema validation")
}

func TestValidateAWFConfigJSON_AllowsDefaultAiCreditsPricingCachedFields(t *testing.T) {
	err := validateAWFConfigJSON(`{"apiProxy":{"enabled":true,"maxRuns":500,"defaultAiCreditsPricing":{"input":3,"output":15,"cachedInput":0.3,"cacheWrite":3}}}`)
	require.NoError(t, err, "apiProxy.defaultAiCreditsPricing should allow cachedInput and cacheWrite fields")
}

// TestBuildAWFConfigJSON_ValidateFlag verifies that schema validation runs when
// WorkflowData.ValidateAWFConfig is true (--validate mode) and is skipped otherwise.
func TestBuildAWFConfigJSON_ValidateFlag(t *testing.T) {
	t.Run("validation runs when ValidateAWFConfig is true", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig:      &EngineConfig{ID: "copilot"},
				ValidateAWFConfig: true,
			},
		}
		_, err := BuildAWFConfigJSON(config)
		require.NoError(t, err, "valid config with ValidateAWFConfig=true should not error")
	})

	t.Run("validation is skipped when ValidateAWFConfig is false", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig:      &EngineConfig{ID: "copilot"},
				ValidateAWFConfig: false,
			},
		}
		_, err := BuildAWFConfigJSON(config)
		require.NoError(t, err, "valid config with ValidateAWFConfig=false should not error")
	})

	t.Run("validation is skipped when WorkflowData is nil", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData:   nil,
		}
		_, err := BuildAWFConfigJSON(config)
		require.NoError(t, err, "valid config with nil WorkflowData should not error")
	})
}

func TestSplitDomainList(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "simple comma-separated list",
			input:    "github.com,api.github.com",
			expected: []string{"github.com", "api.github.com"},
		},
		{
			name:     "list with spaces after commas",
			input:    "github.com, api.github.com, raw.githubusercontent.com",
			expected: []string{"github.com", "api.github.com", "raw.githubusercontent.com"},
		},
		{
			name:     "single domain",
			input:    "github.com",
			expected: []string{"github.com"},
		},
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "wildcards are preserved",
			input:    "*.github.com,*.githubusercontent.com",
			expected: []string{"*.github.com", "*.githubusercontent.com"},
		},
		{
			name:     "duplicates are removed",
			input:    "github.com,api.github.com,github.com",
			expected: []string{"github.com", "api.github.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitDomainList(tt.input)
			assert.Equal(t, tt.expected, result, "splitDomainList(%q)", tt.input)
		})
	}
}

// TestBuildAWFCommand_UsesConfigFile verifies that BuildAWFCommand always produces a run step
// that writes a JSON config file and references it via --config.
func TestBuildAWFCommand_UsesConfigFile(t *testing.T) {
	config := AWFCommandConfig{
		EngineName:     "copilot",
		EngineCommand:  "copilot --prompt-file /tmp/prompt.txt",
		LogFile:        "/tmp/gh-aw/agent-stdio.log",
		AllowedDomains: "github.com,api.github.com",
		WorkflowData: &WorkflowData{
			EngineConfig: &EngineConfig{ID: "copilot"},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true},
			},
		},
	}

	command := BuildAWFCommand(config)

	// Should write the config file using printf
	assert.Contains(t, command, "printf", "expected printf command to write the config file")
	assert.Contains(t, command, "awf-config.json", "expected awf-config.json reference")

	// Should copy the config file to /tmp/gh-aw/awf-config.json for artifact upload
	assert.Contains(t, command, constants.AWFConfigFilePath, "expected awf-config.json to be copied to /tmp/gh-aw/")

	// Should reference the config file via --config
	assert.Contains(t, command, "--config", "expected --config flag in AWF invocation")

	// Should pass the merged models.json path (written by generate_aw_info.cjs in the activation job).
	assert.Contains(t, command, `export GH_AW_MODELS_JSON_PATH="/tmp/gh-aw/models.json"`, "expected AWF command to export the merged models.json path from /tmp/gh-aw")

	// Should NOT have --allow-domains as a CLI flag (moved to config file)
	assert.NotContains(t, command, "--allow-domains", "expected --allow-domains to be absent from CLI args")

	// Should NOT have --enable-api-proxy as a CLI flag (moved to config file)
	assert.NotContains(t, command, "--enable-api-proxy", "expected --enable-api-proxy to be absent from CLI args")

	// Should NOT have --image-tag as a CLI flag (moved to config file)
	assert.NotContains(t, command, "--image-tag", "expected --image-tag to be absent from CLI args")

	// The JSON content in the printf command should have the expected structure.
	// With ${GH_AW_MAX_AI_CREDITS} injected, shellEscapeArgWithVarPreserved uses
	// double-quote wrapping, so JSON double-quotes appear as \" in the shell command.
	assert.Contains(t, command, `\"allowDomains\"`, "config JSON should include allowDomains")
	assert.Contains(t, command, `\"enabled\":true`, "config JSON should have apiProxy enabled")
}

func TestBuildAWFCommand_EmbedsPlatformConfig(t *testing.T) {
	config := AWFCommandConfig{
		EngineName:     "copilot",
		EngineCommand:  "copilot --prompt-file /tmp/prompt.txt",
		LogFile:        "/tmp/gh-aw/agent-stdio.log",
		AllowedDomains: "github.com",
		WorkflowData: &WorkflowData{
			EngineConfig: &EngineConfig{ID: "copilot"},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true},
			},
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Type:     SandboxTypeAWF,
					Platform: "ghes",
				},
			},
		},
	}

	command := BuildAWFCommand(config)

	assert.Contains(t, command, `\"platform\":{\"type\":\"ghes\"}`, "expected awf-config JSON in command to include platform.type")
}

func TestBuildAWFCommand_ResolvesMaxAICreditsFromEnv(t *testing.T) {
	tests := []struct {
		name          string
		isDetection   bool
		isEvals       bool
		defaultBudget int64
	}{
		{
			name:          "agent run uses agent fallback",
			isDetection:   false,
			isEvals:       false,
			defaultBudget: constants.DefaultMaxAICredits,
		},
		{
			name:          "detection run uses detection fallback",
			isDetection:   true,
			isEvals:       false,
			defaultBudget: constants.DefaultDetectionMaxAICredits,
		},
		{
			name:          "evals run uses evals fallback",
			isDetection:   false,
			isEvals:       true,
			defaultBudget: constants.DefaultDetectionMaxAICredits,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := AWFCommandConfig{
				EngineName:                 "copilot",
				EngineCommand:              "copilot --prompt-file /tmp/prompt.txt",
				LogFile:                    "/tmp/gh-aw/agent-stdio.log",
				AllowedDomains:             "github.com,api.github.com",
				ResolveMaxAICreditsFromEnv: true,
				WorkflowData: &WorkflowData{
					IsDetectionRun: tt.isDetection,
					IsEvalsRun:     tt.isEvals,
					EngineConfig:   &EngineConfig{ID: "copilot"},
					NetworkPermissions: &NetworkPermissions{
						Firewall: &FirewallConfig{Enabled: true},
					},
				},
			}

			command := BuildAWFCommand(config)
			assert.Contains(t, command, fmt.Sprintf(`GH_AW_MAX_AI_CREDITS="${GH_AW_MAX_AI_CREDITS:-%d}"`, tt.defaultBudget))
			assert.Contains(t, command, `if [[ ! "$GH_AW_MAX_AI_CREDITS" =~ ^[0-9]+$ ]]; then`)
			assert.Contains(t, command, fmt.Sprintf(`GH_AW_MAX_AI_CREDITS="%d"`, tt.defaultBudget))
			assert.Contains(t, command, `\"maxAiCredits\":${GH_AW_MAX_AI_CREDITS}`)
			assert.NotContains(t, command, "vars.GH_AW_DEFAULT_MAX_AI_CREDITS")
			assert.NotContains(t, command, "vars.GH_AW_DEFAULT_DETECTION_MAX_AI_CREDITS")
			assert.NotContains(t, command, "${{")
		})
	}
}

func TestBuildAWFCommand_PreservesGitHubExpressionOperatorsInConfigJSON(t *testing.T) {
	config := AWFCommandConfig{
		EngineName:     "copilot",
		EngineCommand:  "copilot --prompt-file /tmp/prompt.txt",
		LogFile:        "/tmp/gh-aw/agent-stdio.log",
		AllowedDomains: "${{ env.MCP_ENV == 'staging' && env.MCP_URL_STAGING || env.MCP_URL_PROD }}",
		WorkflowData: &WorkflowData{
			EngineConfig: &EngineConfig{ID: "copilot"},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true},
			},
		},
	}

	command := BuildAWFCommand(config)

	assert.Contains(t, command, "env.MCP_ENV == 'staging'", "expected full GitHub Actions expression to be preserved")
	assert.Contains(t, command, "&&", "expected AWF config JSON in command to preserve &&")
	assert.NotContains(t, command, "\\u0026", "expected AWF config JSON in command to not HTML-escape '&'")
}

// TestBuildAWFCommand_SchemaKeyEscapedWhenExpressionPresent verifies that the $schema JSON
// key is written as \$schema in the shell command when the AWF config JSON is double-quoted
// (i.e. when AllowedDomains contains a GitHub Actions expression). Without the escaping,
// bash expands $schema as a variable — which is always unset — producing an empty key that
// causes AWF to reject the config with "config. is not supported".
func TestBuildAWFCommand_SchemaKeyEscapedWhenExpressionPresent(t *testing.T) {
	config := AWFCommandConfig{
		EngineName:    "copilot",
		EngineCommand: "copilot --prompt-file /tmp/prompt.txt",
		LogFile:       "/tmp/gh-aw/agent-stdio.log",
		// AllowedDomains contains a GitHub Actions expression, forcing double-quote
		// shell wrapping for the config JSON. The $schema key must still be safe.
		AllowedDomains: "${{ env.DOMAINS }}",
		WorkflowData: &WorkflowData{
			EngineConfig: &EngineConfig{ID: "copilot"},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true},
			},
		},
	}

	command := BuildAWFCommand(config)

	// The config JSON is double-quoted because AllowedDomains contains ${{ }}.
	// The $schema key must appear as \$schema so bash does not expand it.
	assert.Contains(t, command, `\$schema`, "expected \\$schema (escaped) in double-quoted config JSON to prevent bash variable expansion")

	// The GitHub Actions expression must remain unescaped for runner evaluation.
	assert.Contains(t, command, "${{ env.DOMAINS }}", "expected GitHub Actions expression to be preserved unescaped")
}

// TestBuildAWFCommand_ConfigFileWithPathSetup verifies that the config file write command
// is correctly integrated with the path setup section.
func TestBuildAWFCommand_ConfigFileWithPathSetup(t *testing.T) {
	config := AWFCommandConfig{
		EngineName:     "copilot",
		EngineCommand:  "copilot --prompt-file /tmp/prompt.txt",
		LogFile:        "/tmp/gh-aw/agent-stdio.log",
		AllowedDomains: "github.com",
		PathSetup:      "export GH_AW_NODE_BIN=$(command -v node || true)",
		WorkflowData: &WorkflowData{
			EngineConfig: &EngineConfig{ID: "copilot"},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true},
			},
		},
	}

	command := BuildAWFCommand(config)

	// PathSetup, config write, and AWF invocation must all appear in order
	pathSetupIdx := strings.Index(command, "GH_AW_NODE_BIN")
	configWriteIdx := strings.Index(command, "awf-config.json")
	modelsPathIdx := strings.Index(command, "GH_AW_MODELS_JSON_PATH")
	awfIdx := strings.Index(command, "awf ")

	assert.GreaterOrEqual(t, pathSetupIdx, 0, "path setup should appear in command")
	assert.GreaterOrEqual(t, configWriteIdx, 0, "config file write should appear in command")
	assert.GreaterOrEqual(t, modelsPathIdx, 0, "models.json path export should appear in command")
	assert.GreaterOrEqual(t, awfIdx, 0, "AWF invocation should appear in command")

	// Order must be: path setup → config write → models path export → AWF invocation
	assert.Less(t, pathSetupIdx, configWriteIdx, "path setup must precede config file write")
	assert.Less(t, configWriteIdx, modelsPathIdx, "config file write must precede models.json path export")
	assert.Less(t, modelsPathIdx, awfIdx, "models.json path export must precede AWF invocation")
	assert.Contains(t, command, awfShellcheckDirective, "should include scoped shellcheck suppression before awf invocation")
}

func TestBuildAWFCommand_AddsToolCacheMountProbe(t *testing.T) {
	config := AWFCommandConfig{
		EngineName:     "copilot",
		EngineCommand:  "copilot --prompt-file /tmp/prompt.txt",
		LogFile:        "/tmp/gh-aw/agent-stdio.log",
		AllowedDomains: "github.com",
		WorkflowData: &WorkflowData{
			EngineConfig: &EngineConfig{ID: "copilot"},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true},
			},
		},
	}

	command := BuildAWFCommand(config)

	assert.Contains(t, command, `GH_AW_TOOL_CACHE="${RUNNER_TOOL_CACHE:?RUNNER_TOOL_CACHE must be set}"`, "should require RUNNER_TOOL_CACHE instead of guessing fallback paths")
	assert.Contains(t, command, `GH_AW_TOOL_CACHE_MOUNT="$GH_AW_TOOL_CACHE:$GH_AW_TOOL_CACHE:ro"`, "should mount non-/opt tool cache paths")
	assert.NotContains(t, command, `/home/runner/work/_tool`, "should not assume a self-hosted runner tool-cache path")
	assert.NotContains(t, command, `:-/opt/hostedtoolcache`, "should not fall back to /opt/hostedtoolcache")
	assert.Contains(t, command, `${GH_AW_TOOL_CACHE_MOUNT:+--mount "$GH_AW_TOOL_CACHE_MOUNT"}`, "should inject tool-cache mount args into awf invocation")
	assert.Contains(t, command, awfShellcheckDirective, "should suppress intentional argument splitting in awf invocation")
}

func TestBuildAWFCommand_WorkflowCallNetworkAllowedUpdaterUsesRunnerTempEnv(t *testing.T) {
	config := AWFCommandConfig{
		EngineName:     "copilot",
		EngineCommand:  "copilot --prompt-file /tmp/prompt.txt",
		LogFile:        "/tmp/gh-aw/agent-stdio.log",
		AllowedDomains: "github.com",
		WorkflowData: &WorkflowData{
			On:           "workflow_call",
			EngineConfig: &EngineConfig{ID: "copilot"},
			NetworkPermissions: &NetworkPermissions{
				Allowed:      []string{"defaults"},
				AllowedInput: true,
				Firewall:     &FirewallConfig{Enabled: true},
			},
		},
	}

	command := BuildAWFCommand(config)

	assert.Contains(t, command, `update_network_allowed.cjs`, "workflow_call network updater should invoke the JavaScript implementation")
	assert.Contains(t, command, `GH_AW_ECOSYSTEM_MAP_JSON=`, "workflow_call network updater should pass ecosystem map via env var")
	assert.Contains(t, command, `"${RUNNER_TEMP}/gh-aw/actions/update_network_allowed.cjs"`, "workflow_call network updater should resolve RUNNER_TEMP at runtime via shell expansion")
	assert.NotContains(t, command, `os.environ.get("RUNNER_TEMP")`, "workflow_call network updater should not use Python os.environ")
	assert.NotContains(t, command, `Path("${RUNNER_TEMP}/gh-aw/awf-config.json")`, "workflow_call network updater should not embed an unexpanded RUNNER_TEMP literal")
}

// TestBuildAWFCommand_WritesAgentCLIStartTimestamp verifies that BuildAWFCommand
// always emits a printf command that writes the epoch-ms timestamp to
// AgentCLIStartMsPath at the very beginning of the run block, before any
// PathSetup or config-file write, so sendJobConclusionSpan can use it as the
// true start time of the Execute Agent CLI step.
func TestBuildAWFCommand_WritesAgentCLIStartTimestamp(t *testing.T) {
	tests := []struct {
		name      string
		pathSetup string
	}{
		{"with PathSetup", "touch " + AgentStepSummaryPath},
		{"without PathSetup", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config := AWFCommandConfig{
				EngineName:     "claude",
				EngineCommand:  "claude --print",
				LogFile:        "/tmp/gh-aw/agent-stdio.log",
				AllowedDomains: "api.anthropic.com",
				PathSetup:      tc.pathSetup,
				WorkflowData: &WorkflowData{
					EngineConfig: &EngineConfig{ID: "claude"},
					NetworkPermissions: &NetworkPermissions{
						Firewall: &FirewallConfig{Enabled: true},
					},
				},
			}

			command := BuildAWFCommand(config)

			// The timestamp write must appear in the command.
			assert.Contains(t, command, AgentCLIStartMsPath,
				"command must write agent CLI start timestamp to %s", AgentCLIStartMsPath)
			assert.Contains(t, command, "date +%s%3N",
				"command must use date +%%s%%3N to capture epoch milliseconds")

			// The timestamp write must appear before the AWF invocation so it captures
			// the step start time rather than the time after AWF container setup.
			tsIdx := strings.Index(command, AgentCLIStartMsPath)
			awfIdx := strings.Index(command, "awf ")
			assert.Less(t, tsIdx, awfIdx,
				"timestamp write must appear before AWF invocation")

			// The timestamp write must be the first substantive line after set -o pipefail.
			pipefailIdx := strings.Index(command, "set -o pipefail")
			assert.Less(t, pipefailIdx, tsIdx,
				"set -o pipefail must appear before timestamp write")
			assert.Contains(t, command, awfShellcheckDirective, "should include scoped shellcheck suppression before awf invocation")
			// Nothing between set -o pipefail and the timestamp write should reference
			// PathSetup content (timestamp must come first).
			if tc.pathSetup != "" {
				pathSetupIdx := strings.Index(command, tc.pathSetup)
				assert.Greater(t, pathSetupIdx, tsIdx,
					"timestamp write must appear before PathSetup content")
			}
		})
	}
}

func TestBuildAWFTopologyAttachList(t *testing.T) {
	t.Run("includes only MCP gateway when cli proxy is not needed", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: map[string]any{
				"github": map[string]any{
					"toolsets": []string{"repos"},
				},
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true},
			},
		}

		targets := buildAWFTopologyAttachList(workflowData)
		assert.Equal(t, []string{"awmg-mcpg"}, targets)
	})

	t.Run("includes CLI proxy when gh-proxy mode is enabled", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: map[string]any{
				"github": map[string]any{
					"mode":     "gh-proxy",
					"toolsets": []string{"repos"},
				},
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true, Version: "v0.26.0"},
			},
		}

		targets := buildAWFTopologyAttachList(workflowData)
		assert.Equal(t, []string{"awmg-mcpg", "awmg-cli-proxy"}, targets)
	})
}

func TestBuildAWFConfigJSON_EmitsModelPolicyFromWorkflowData(t *testing.T) {
	config := AWFCommandConfig{
		EngineName:     "copilot",
		AllowedDomains: "github.com",
		WorkflowData: &WorkflowData{
			EngineConfig: &EngineConfig{ID: "copilot"},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true},
			},
			ModelPolicyAllowed: []string{"gpt-5", "claude-sonnet"},
			ModelPolicyBlocked: []string{"gpt-5-pro", "claude-opus"},
		},
	}

	jsonStr, err := BuildAWFConfigJSON(config)
	require.NoError(t, err)
	assert.Contains(t, jsonStr, `"allowedModels":["gpt-5","claude-sonnet"]`)
	assert.Contains(t, jsonStr, `"disallowedModels":["gpt-5-pro","claude-opus"]`)
}

func TestBuildAWFConfigJSON_ModelPolicyEnvOverridePrecedence(t *testing.T) {
	t.Setenv(compilerenv.PolicyModelsAllowed, "gemini-pro,gpt-5-mini")
	t.Setenv(compilerenv.PolicyModelsBlocked, "claude-opus, gpt-5-pro")

	config := AWFCommandConfig{
		EngineName:     "copilot",
		AllowedDomains: "github.com",
		WorkflowData: &WorkflowData{
			EngineConfig: &EngineConfig{ID: "copilot"},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true},
			},
			ModelPolicyAllowed: []string{"frontmatter-allowed", "gpt-5-mini"},
			ModelPolicyBlocked: []string{"frontmatter-blocked"},
		},
	}

	jsonStr, err := BuildAWFConfigJSON(config)
	require.NoError(t, err)
	assert.Contains(t, jsonStr, `"allowedModels":["gpt-5-mini"]`)
	assert.Contains(t, jsonStr, `"disallowedModels":["frontmatter-blocked","claude-opus","gpt-5-pro"]`)
	assert.NotContains(t, jsonStr, "frontmatter-allowed")
	assert.NotContains(t, jsonStr, "gemini-pro")
}

func TestBuildAWFConfigJSON_ModelPolicyEnvOverride_IsPerList(t *testing.T) {
	t.Setenv(compilerenv.PolicyModelsAllowed, "gemini-pro")
	t.Setenv(compilerenv.PolicyModelsBlocked, "")

	config := AWFCommandConfig{
		EngineName:     "copilot",
		AllowedDomains: "github.com",
		WorkflowData: &WorkflowData{
			EngineConfig: &EngineConfig{ID: "copilot"},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true},
			},
			ModelPolicyAllowed: []string{"frontmatter-allowed", "gemini-pro"},
			ModelPolicyBlocked: []string{"frontmatter-disallowed"},
		},
	}

	jsonStr, err := BuildAWFConfigJSON(config)
	require.NoError(t, err)
	assert.Contains(t, jsonStr, `"allowedModels":["gemini-pro"]`)
	assert.Contains(t, jsonStr, `"disallowedModels":["frontmatter-disallowed"]`)
}

func TestBuildAWFConfigJSON_ModelPolicyEnvBlockedUnionOnly(t *testing.T) {
	t.Setenv(compilerenv.PolicyModelsAllowed, "")
	t.Setenv(compilerenv.PolicyModelsBlocked, "claude-opus")

	config := AWFCommandConfig{
		EngineName:     "copilot",
		AllowedDomains: "github.com",
		WorkflowData: &WorkflowData{
			EngineConfig: &EngineConfig{ID: "copilot"},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true},
			},
			ModelPolicyAllowed: []string{"frontmatter-allowed"},
			ModelPolicyBlocked: []string{"frontmatter-blocked"},
		},
	}

	jsonStr, err := BuildAWFConfigJSON(config)
	require.NoError(t, err)
	assert.Contains(t, jsonStr, `"allowedModels":["frontmatter-allowed"]`)
	assert.Contains(t, jsonStr, `"disallowedModels":["frontmatter-blocked","claude-opus"]`)
}

func TestBuildAWFConfigJSON_ModelPolicyEnvAllowedIntersectionCanBeEmpty(t *testing.T) {
	t.Setenv(compilerenv.PolicyModelsAllowed, "gemini-pro")
	t.Setenv(compilerenv.PolicyModelsBlocked, "")

	config := AWFCommandConfig{
		EngineName:     "copilot",
		AllowedDomains: "github.com",
		WorkflowData: &WorkflowData{
			EngineConfig: &EngineConfig{ID: "copilot"},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true},
			},
			ModelPolicyAllowed: []string{"frontmatter-allowed"},
			ModelPolicyBlocked: []string{"frontmatter-blocked"},
		},
	}

	jsonStr, err := BuildAWFConfigJSON(config)
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(jsonStr), &parsed))
	apiProxy, ok := parsed["apiProxy"].(map[string]any)
	require.True(t, ok)
	_, hasAllowedModels := apiProxy["allowedModels"]
	assert.False(t, hasAllowedModels)
	assert.Contains(t, jsonStr, `"disallowedModels":["frontmatter-blocked"]`)
}

func TestIntersectModelPolicyRules_EmptyOverrideKeepsLocal(t *testing.T) {
	got := intersectModelPolicyRules([]string{"gpt-5"}, nil)
	assert.Equal(t, []string{"gpt-5"}, got)
}

func TestIntersectModelPolicyRules_EmptyLocalUsesOverride(t *testing.T) {
	got := intersectModelPolicyRules(nil, []string{"gpt-5"})
	assert.Equal(t, []string{"gpt-5"}, got)
}

func TestIntersectModelPolicyRules_OverlapOnly(t *testing.T) {
	got := intersectModelPolicyRules([]string{"gpt-5", "claude-sonnet"}, []string{"gemini-pro", "gpt-5"})
	assert.Equal(t, []string{"gpt-5"}, got)
}

func TestBuildAWFConfigJSON_ModelPolicyConflictDisallowedWins(t *testing.T) {
	config := AWFCommandConfig{
		EngineName:     "copilot",
		AllowedDomains: "github.com",
		WorkflowData: &WorkflowData{
			EngineConfig: &EngineConfig{ID: "copilot"},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true},
			},
			ModelPolicyAllowed: []string{"gpt-5", "claude-sonnet"},
			ModelPolicyBlocked: []string{"gpt-5"},
		},
	}

	jsonStr, err := BuildAWFConfigJSON(config)
	require.NoError(t, err)
	assert.Contains(t, jsonStr, `"allowedModels":["claude-sonnet"]`)
	assert.Contains(t, jsonStr, `"disallowedModels":["gpt-5"]`)
}
