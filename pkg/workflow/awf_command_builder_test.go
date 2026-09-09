//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildAWFArgsAuditDir(t *testing.T) {
	t.Run("non-arc-dind omits audit-dir and proxy-logs-dir from CLI flags", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		config := AWFCommandConfig{
			EngineName:     "copilot",
			WorkflowData:   workflowData,
			AllowedDomains: "github.com",
		}

		args := BuildAWFArgs(config)
		argsStr := strings.Join(args, " ")

		// Non-ARC/DinD: these should be in config, not CLI flags
		assert.NotContains(t, argsStr, "--audit-dir", "audit-dir should be in config for non-arc-dind")
		assert.NotContains(t, argsStr, "--proxy-logs-dir", "proxy-logs-dir should be in config for non-arc-dind")
	})

	t.Run("arc-dind also omits audit-dir and proxy-logs-dir from CLI flags", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
			RunnerConfig: &RunnerConfig{Topology: RunnerTopologyArcDind},
		}

		config := AWFCommandConfig{
			EngineName:     "copilot",
			WorkflowData:   workflowData,
			AllowedDomains: "github.com",
		}

		args := BuildAWFArgs(config)
		argsStr := strings.Join(args, " ")

		assert.NotContains(t, argsStr, "--audit-dir", "arc-dind audit-dir should be emitted via config JSON")
		assert.NotContains(t, argsStr, "--proxy-logs-dir", "arc-dind proxy-logs-dir should be emitted via config JSON")
	})
}

// TestBuildAWFArgsAllowHostPorts tests that BuildAWFArgs includes --allow-host-ports
// with port 80, 443, and the MCP gateway port so the AWF agent container can reach
// the gateway through the firewall's iptables rules.
func TestBuildAWFArgsAllowHostPorts(t *testing.T) {
	t.Run("includes default MCP gateway port 8080", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName: "copilot",
			WorkflowData: &WorkflowData{
				Name:         "test-workflow",
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{Runtime: AgentRuntimeDockerSudoIptables},
				},
			},
			AllowedDomains: "github.com",
		}

		args := BuildAWFArgs(config)
		argsStr := strings.Join(args, " ")

		assert.Contains(t, argsStr, "--allow-host-ports", "Should include --allow-host-ports flag")
		assert.Equal(t, "80,443,8080", argValue(args, "--allow-host-ports"), "Should allow default gateway port 8080 alongside 80 and 443")
	})

	t.Run("uses custom MCP gateway port from sandbox config", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName: "copilot",
			WorkflowData: &WorkflowData{
				Name:         "test-workflow",
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{Runtime: AgentRuntimeDockerSudoIptables},
					MCP:   &MCPGatewayRuntimeConfig{Port: 9090},
				},
			},
			AllowedDomains: "github.com",
		}

		args := BuildAWFArgs(config)
		argsStr := strings.Join(args, " ")

		assert.Contains(t, argsStr, "--allow-host-ports", "Should include --allow-host-ports flag")
		assert.Equal(t, "80,443,9090", argValue(args, "--allow-host-ports"), "Should use custom gateway port from sandbox config")
		assert.NotContains(t, argsStr, "8080", "Should not include default port when custom port is set")
	})

	t.Run("handles nil SandboxConfig gracefully — strict mode skips host-access", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName: "copilot",
			WorkflowData: &WorkflowData{
				Name:         "test-workflow",
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
			AllowedDomains: "github.com",
		}

		args := BuildAWFArgs(config)
		argsStr := strings.Join(args, " ")

		assert.NotContains(t, argsStr, "--allow-host-ports", "Strict mode (default) should not emit --allow-host-ports")
		assert.NotContains(t, argsStr, "--enable-host-access", "Strict mode (default) should not emit --enable-host-access")
	})

	t.Run("strict mode ignores services and explicit ports", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName: "copilot",
			WorkflowData: &WorkflowData{
				Name:         "test-workflow",
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
				Services: `services:
  postgres:
    image: postgres:18
    ports:
      - 5432:5432
`,
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{ID: "awf", AllowHostPorts: []int{9200}},
				},
			},
			AllowedDomains: "github.com",
		}

		args := BuildAWFArgs(config)
		argsStr := strings.Join(args, " ")

		assert.NotContains(t, argsStr, "--allow-host-ports", "--allow-host-ports requires --enable-host-access, so strict mode (the default) must not emit it")
		assert.NotContains(t, argsStr, "--enable-host-access", "Strict mode should not imply broad host access")
	})

	t.Run("skips --allow-host-ports and warns when AWF version is too old", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName: "copilot",
			WorkflowData: &WorkflowData{
				Name:         "test-workflow",
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{
						Enabled: true,
						Version: "v0.25.23",
					},
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						ID:             "awf",
						Runtime:        AgentRuntimeDockerSudoIptables,
						AllowHostPorts: []int{9000},
					},
				},
			},
			AllowedDomains: "github.com",
		}

		var args []string
		stderr := captureStderr(func() {
			args = BuildAWFArgs(config)
		})
		argsStr := strings.Join(args, " ")

		assert.NotContains(t, argsStr, "--allow-host-ports", "Should skip --allow-host-ports for AWF versions below minimum support")
		assert.Contains(t, stderr, string(constants.AWFAllowHostPortsMinVersion), "Warning should name the minimum AWF version")
	})

	t.Run("skips host-access flags when network isolation is enabled", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName: "copilot",
			WorkflowData: &WorkflowData{
				Name:         "test-workflow",
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
			AllowedDomains: "github.com",
		}

		args := BuildAWFArgs(config)
		argsStr := strings.Join(args, " ")

		assert.NotContains(t, argsStr, "--enable-host-access", "Should skip --enable-host-access in network isolation mode")
		assert.NotContains(t, argsStr, "--allow-host-ports", "Should skip --allow-host-ports in network isolation mode")
	})

	t.Run("legacy security keeps host access and merges explicit ports, ignoring services", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName: "copilot",
			WorkflowData: &WorkflowData{
				Name:         "test-workflow",
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
				Services: `services:
  postgres:
    image: postgres:18
    ports:
      - 5432:5432
`,
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						ID:             "awf",
						Runtime:        AgentRuntimeDockerSudoIptables,
						AllowHostPorts: []int{9000, 80},
					},
				},
			},
			AllowedDomains: "github.com",
		}

		args := BuildAWFArgs(config)
		argsStr := strings.Join(args, " ")

		assert.Contains(t, argsStr, "--enable-host-access", "Legacy mode should still emit broad host access")
		assert.Equal(t, "80,443,8080,9000", argValue(args, "--allow-host-ports"), "Legacy mode should merge default and explicit ports; services are reached via --allow-host-service-ports, not a static allowlist")
	})
}

// TestBuildAWFArgsDiagnosticLogs tests that BuildAWFArgs includes --diagnostic-logs
// only when features.awf-diagnostic-logs is enabled.
func TestBuildAWFArgsDiagnosticLogs(t *testing.T) {
	baseWorkflow := func(features map[string]any) *WorkflowData {
		return &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true},
			},
			Features: features,
		}
	}

	t.Run("does not include --diagnostic-logs when feature flag is absent", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			WorkflowData:   baseWorkflow(nil),
			AllowedDomains: "github.com",
		}

		args := BuildAWFArgs(config)
		argsStr := strings.Join(args, " ")

		assert.NotContains(t, argsStr, "--diagnostic-logs", "Should not include --diagnostic-logs when feature flag is absent")
	})

	t.Run("includes --diagnostic-logs when awf-diagnostic-logs is enabled", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName: "copilot",
			WorkflowData: baseWorkflow(map[string]any{
				string(constants.AwfDiagnosticLogsFeatureFlag): true,
			}),
			AllowedDomains: "github.com",
		}

		args := BuildAWFArgs(config)
		argsStr := strings.Join(args, " ")

		assert.Contains(t, argsStr, "--diagnostic-logs", "Should include --diagnostic-logs when feature flag is enabled")
	})
}

// TestBuildAWFArgsMemoryLimit tests that BuildAWFArgs passes --memory-limit
// when sandbox.agent.memory is configured in the workflow frontmatter
func TestBuildAWFArgsMemoryLimit(t *testing.T) {
	t.Run("includes --memory-limit flag when memory is configured", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Memory: "6g",
				},
			},
		}

		config := AWFCommandConfig{
			EngineName:     "copilot",
			WorkflowData:   workflowData,
			AllowedDomains: "github.com",
		}

		args := BuildAWFArgs(config)
		argsStr := strings.Join(args, " ")

		assert.Contains(t, argsStr, "--memory-limit", "Should include --memory-limit flag")
		assert.Contains(t, argsStr, "6g", "Should include the memory value")
	})

	t.Run("does not include --memory-limit flag when memory is not configured", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		config := AWFCommandConfig{
			EngineName:     "copilot",
			WorkflowData:   workflowData,
			AllowedDomains: "github.com",
		}

		args := BuildAWFArgs(config)
		argsStr := strings.Join(args, " ")

		assert.NotContains(t, argsStr, "--memory-limit", "Should not include --memory-limit when memory is not configured")
	})

	t.Run("includes correct memory value when multiple sizes configured", func(t *testing.T) {
		for _, memory := range []string{"512m", "4g", "8g"} {
			t.Run(memory, func(t *testing.T) {
				workflowData := &WorkflowData{
					Name: "test-workflow",
					EngineConfig: &EngineConfig{
						ID: "copilot",
					},
					SandboxConfig: &SandboxConfig{
						Agent: &AgentSandboxConfig{
							Memory: memory,
						},
					},
				}

				config := AWFCommandConfig{
					EngineName:     "copilot",
					WorkflowData:   workflowData,
					AllowedDomains: "github.com",
				}

				args := BuildAWFArgs(config)
				argsStr := strings.Join(args, " ")

				assert.Contains(t, argsStr, "--memory-limit", "Should include --memory-limit flag")
				assert.Contains(t, argsStr, memory, "Should include the correct memory value")
			})
		}
	})
}

func TestBuildAWFArgsCliProxy(t *testing.T) {
	baseWorkflow := func(features map[string]any, tools map[string]any) *WorkflowData {
		return &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true},
			},
			Features: features,
			Tools:    tools,
		}
	}

	t.Run("does not include cli-proxy flags when feature flag is absent", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			WorkflowData:   baseWorkflow(nil, nil),
			AllowedDomains: "github.com",
		}

		args := BuildAWFArgs(config)
		argsStr := strings.Join(args, " ")

		assert.NotContains(t, argsStr, "--difc-proxy-host", "Should not include --difc-proxy-host when feature flag is absent")
		assert.NotContains(t, argsStr, "--difc-proxy-ca-cert", "Should not include --difc-proxy-ca-cert when feature flag is absent")
		assert.NotContains(t, argsStr, "--enable-cli-proxy", "Should not include deprecated --enable-cli-proxy")
		assert.NotContains(t, argsStr, "--cli-proxy-policy", "Should not include deprecated --cli-proxy-policy")
	})

	t.Run("includes --difc-proxy-host and --difc-proxy-ca-cert when cli-proxy is enabled", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName: "copilot",
			WorkflowData: &WorkflowData{
				Name: "test-workflow",
				EngineConfig: &EngineConfig{
					ID: "copilot",
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true, Version: "v0.26.0"},
				},
				Features: map[string]any{"cli-proxy": true},
			},
			AllowedDomains: "github.com",
		}

		args := BuildAWFArgs(config)
		argsStr := strings.Join(args, " ")

		assert.Contains(t, argsStr, "--difc-proxy-host", "Should include --difc-proxy-host when cli-proxy is enabled")
		assert.Contains(t, argsStr, "host.docker.internal:18443", "Should use host.docker.internal:18443 as proxy host")
		assert.Contains(t, argsStr, "--difc-proxy-ca-cert", "Should include --difc-proxy-ca-cert")
		assert.Contains(t, argsStr, "/tmp/gh-aw/difc-proxy-tls/ca.crt", "Should use the correct CA cert path")
		assert.NotContains(t, argsStr, "--enable-cli-proxy", "Should not include deprecated --enable-cli-proxy")
		assert.NotContains(t, argsStr, "--cli-proxy-policy", "Should not include deprecated --cli-proxy-policy")
	})

	t.Run("uses internal cli proxy host when network isolation is enabled", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName: "copilot",
			WorkflowData: &WorkflowData{
				Name: "test-workflow",
				EngineConfig: &EngineConfig{
					ID: "copilot",
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true, Version: "v0.26.0"},
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Type: SandboxTypeAWF,
					},
				},
				Features: map[string]any{"cli-proxy": true},
			},
			AllowedDomains: "github.com",
		}

		args := BuildAWFArgs(config)
		argsStr := strings.Join(args, " ")

		assert.Contains(t, argsStr, "--difc-proxy-host", "Should include --difc-proxy-host when cli-proxy is enabled")
		assert.Contains(t, argsStr, "awmg-cli-proxy:18443", "Should use internal awf-net CLI proxy address in isolation mode")
		assert.NotContains(t, argsStr, "host.docker.internal:18443", "Should not use host.docker.internal in isolation mode")
	})

	t.Run("does not include cli-proxy flags for copilot by default", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName: "copilot",
			WorkflowData: &WorkflowData{
				Name: "test-workflow",
				EngineConfig: &EngineConfig{
					ID: "copilot",
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true, Version: "v0.26.0"},
				},
				Features: map[string]any{},
			},
			AllowedDomains: "github.com",
		}

		args := BuildAWFArgs(config)
		argsStr := strings.Join(args, " ")

		assert.NotContains(t, argsStr, "--difc-proxy-host", "Should not include --difc-proxy-host for copilot by default")
		assert.NotContains(t, argsStr, "--difc-proxy-ca-cert", "Should not include --difc-proxy-ca-cert for copilot by default")
	})

	t.Run("does not include deprecated flags even with guard policy configured", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName: "copilot",
			WorkflowData: &WorkflowData{
				Name: "test-workflow",
				EngineConfig: &EngineConfig{
					ID: "copilot",
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true, Version: "v0.26.0"},
				},
				Features: map[string]any{"cli-proxy": true},
				Tools: map[string]any{
					"github": map[string]any{
						"min-integrity": "approved",
					},
				},
			},
			AllowedDomains: "github.com",
		}

		args := BuildAWFArgs(config)
		argsStr := strings.Join(args, " ")

		assert.Contains(t, argsStr, "--difc-proxy-host", "Should include --difc-proxy-host")
		assert.Contains(t, argsStr, "--difc-proxy-ca-cert", "Should include --difc-proxy-ca-cert")
		assert.NotContains(t, argsStr, "--enable-cli-proxy", "Should not include deprecated --enable-cli-proxy")
		assert.NotContains(t, argsStr, "--cli-proxy-policy", "Should not include deprecated --cli-proxy-policy")
	})

	t.Run("skips all cli-proxy flags when AWF version is too old", func(t *testing.T) {
		// Simulate a workflow that pins an AWF version older than AWFCliProxyMinVersion
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
					Version: "v0.25.16", // older than AWFCliProxyMinVersion v0.25.17
				},
			},
			Features: map[string]any{
				"cli-proxy": true,
			},
			Tools: map[string]any{
				"github": map[string]any{
					"min-integrity": "approved",
				},
			},
		}

		config := AWFCommandConfig{
			EngineName:     "copilot",
			WorkflowData:   workflowData,
			AllowedDomains: "github.com",
		}

		args := BuildAWFArgs(config)
		argsStr := strings.Join(args, " ")

		assert.NotContains(t, argsStr, "--difc-proxy-host", "Should not include --difc-proxy-host for old AWF")
		assert.NotContains(t, argsStr, "--difc-proxy-ca-cert", "Should not include --difc-proxy-ca-cert for old AWF")
		assert.NotContains(t, argsStr, "--enable-cli-proxy", "Should not include deprecated --enable-cli-proxy")
	})
}

func TestBuildModelsJSONPathExportScript(t *testing.T) {
	t.Run("uses tmp path by default", func(t *testing.T) {
		assert.Equal(t, `export GH_AW_MODELS_JSON_PATH="/tmp/gh-aw/models.json"`, buildModelsJSONPathExportScript(false))
	})

	t.Run("uses runner temp path for arc-dind", func(t *testing.T) {
		assert.Equal(t, `export GH_AW_MODELS_JSON_PATH="${RUNNER_TEMP}/gh-aw/models.json"`, buildModelsJSONPathExportScript(true))
	})
}

func TestGetAWFCommandPrefixNetworkIsolation(t *testing.T) {
	t.Run("returns awf (no sudo) for the default docker runtime profile", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name:         "test-workflow",
			EngineConfig: &EngineConfig{ID: "copilot"},
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					ID: "awf",
				},
			},
		}
		cmd := GetAWFCommandPrefix(workflowData)
		assert.Equal(t, "awf", cmd, "Should return rootless 'awf' for the default docker runtime profile")
		assert.NotContains(t, cmd, "sudo", "Should not contain sudo for the default docker runtime profile")
	})

	t.Run("returns awf (no sudo) for the explicit docker runtime", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name:         "test-workflow",
			EngineConfig: &EngineConfig{ID: "copilot"},
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					ID:      "awf",
					Runtime: AgentRuntimeDocker,
				},
			},
		}
		cmd := GetAWFCommandPrefix(workflowData)
		assert.Equal(t, "awf", cmd, "Should return 'awf' (no sudo) for the default docker runtime profile")
	})

	t.Run("returns awf (no sudo) when no sandbox config is set", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name:         "test-workflow",
			EngineConfig: &EngineConfig{ID: "copilot"},
		}
		cmd := GetAWFCommandPrefix(workflowData)
		assert.Equal(t, "awf", cmd, "Should return 'awf' (no sudo) when there is no sandbox config")
	})

	t.Run("preserves PATH after sudo when legacy-security is enabled", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name:         "test-workflow",
			EngineConfig: &EngineConfig{ID: "copilot"},
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					ID:      "awf",
					Runtime: AgentRuntimeDockerSudoIptables,
				},
			},
		}
		cmd := GetAWFCommandPrefix(workflowData)
		assert.Equal(t, constants.AWFLegacySecurityCommand, cmd)
	})

	t.Run("matches the non-rootless AWF installer path", func(t *testing.T) {
		installerPath := filepath.Join("..", "..", "actions", "setup", "sh", "install_awf_binary.sh")
		installer, err := os.ReadFile(installerPath)
		require.NoError(t, err)

		installDir := regexp.MustCompile(`(?m)^AWF_INSTALL_DIR="([^"]+)"$`).FindSubmatch(installer)
		installName := regexp.MustCompile(`(?m)^AWF_INSTALL_NAME="([^"]+)"$`).FindSubmatch(installer)
		if assert.Len(t, installDir, 2) && assert.Len(t, installName, 2) {
			assert.Contains(t, constants.AWFLegacySecurityCommand, filepath.Join(string(installDir[1]), string(installName[1])))
		}
	})

	t.Run("custom command takes precedence over the runtime profile", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name:         "test-workflow",
			EngineConfig: &EngineConfig{ID: "copilot"},
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					ID:      "awf",
					Command: "custom-awf",
				},
			},
		}
		cmd := GetAWFCommandPrefix(workflowData)
		assert.Equal(t, "custom-awf", cmd, "Custom command should take precedence over the runtime profile command")
	})
}

func TestBuildAWFArgs_LegacySecurityVersionGuard(t *testing.T) {
	t.Run("emits --legacy-security when AWF version supports it", func(t *testing.T) {
		config := AWFCommandConfig{
			WorkflowData: &WorkflowData{
				Name:         "test-workflow",
				EngineConfig: &EngineConfig{ID: "copilot"},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						ID:      "awf",
						Runtime: AgentRuntimeDockerSudoIptables,
					},
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{
						Version: "0.27.32",
					},
				},
			},
			EngineName: "copilot",
		}
		args := BuildAWFArgs(config)
		assert.Contains(t, args, "--legacy-security", "Should emit --legacy-security for AWF >= v0.27.32")
	})

	t.Run("skips --legacy-security when AWF version is too old", func(t *testing.T) {
		config := AWFCommandConfig{
			WorkflowData: &WorkflowData{
				Name:         "test-workflow",
				EngineConfig: &EngineConfig{ID: "copilot"},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						ID:      "awf",
						Runtime: AgentRuntimeDockerSudoIptables,
					},
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{
						Version: "0.27.30",
					},
				},
			},
			EngineName: "copilot",
		}
		args := BuildAWFArgs(config)
		assert.NotContains(t, args, "--legacy-security", "Should NOT emit --legacy-security for AWF < v0.27.32")
		// But should still emit --enable-host-access for backward compat
		assert.Contains(t, args, "--enable-host-access", "Should still emit --enable-host-access for legacy mode")
	})
}

func TestBuildAWFCommand_ServicePortsRequireLegacy(t *testing.T) {
	t.Run("emits --allow-host-service-ports when legacy-security is enabled", func(t *testing.T) {
		config := AWFCommandConfig{
			WorkflowData: &WorkflowData{
				Name:                   "test-workflow",
				EngineConfig:           &EngineConfig{ID: "copilot"},
				ServicePortExpressions: "${{ job.services.db.ports['5432'] }}",
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						ID:      "awf",
						Runtime: AgentRuntimeDockerSudoIptables,
					},
				},
			},
			EngineName:    "copilot",
			EngineCommand: "copilot-agent",
		}
		cmd := BuildAWFCommand(config)
		assert.Contains(t, cmd, "--allow-host-service-ports", "Should emit --allow-host-service-ports in legacy mode")
	})

	t.Run("skips --allow-host-service-ports in strict mode", func(t *testing.T) {
		config := AWFCommandConfig{
			WorkflowData: &WorkflowData{
				Name:                   "test-workflow",
				EngineConfig:           &EngineConfig{ID: "copilot"},
				ServicePortExpressions: "${{ job.services.db.ports['5432'] }}",
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						ID: "awf",
					},
				},
			},
			EngineName:    "copilot",
			EngineCommand: "copilot-agent",
		}
		cmd := BuildAWFCommand(config)
		assert.NotContains(t, cmd, "--allow-host-service-ports", "Should NOT emit --allow-host-service-ports in strict mode")
	})
}

func TestBuildAWFCommandScript_OptionalSections(t *testing.T) {
	base := buildAWFCommandScriptInput{
		writeAgentCLIStartMs:   "start",
		preCreateLog:           "pre",
		modelsJSONPathExport:   "models",
		arcDindDockerHostProbe: "probe",
		arcDindPrefixProbe:     "prefix",
		toolCacheMountProbe:    "tool",
		awfCommand:             "awf",
		expandableArgs:         "--expand",
		toolCacheMountRef:      "--tool-ref",
		arcDindDockerHostRef:   "--docker-ref",
		awfArgs:                []string{"--arg", "value"},
		shellWrappedCommand:    "wrapped",
		logFile:                "/tmp/test.log",
	}

	tests := []struct {
		name       string
		pathSetup  string
		configFile string
		expect     string
	}{
		{
			name:       "includes both path and config setup when provided",
			pathSetup:  "path",
			configFile: "cfg",
			expect:     "\npath\npre\ncfg\nmodels\n",
		},
		{
			name:      "includes only path setup when config is empty",
			pathSetup: "path",
			expect:    "\npath\npre\nmodels\n",
		},
		{
			name:       "includes only config setup when path is empty",
			configFile: "cfg",
			expect:     "\npre\ncfg\nmodels\n",
		},
		{
			name:   "omits both optional sections when empty",
			expect: "\npre\nmodels\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := base
			input.pathSetup = tt.pathSetup
			input.configFileSetup = tt.configFile
			command := buildAWFCommandScript(input)
			assert.Contains(t, command, tt.expect)
			assert.Contains(t, command, "awf --expand --tool-ref --docker-ref")
			assert.Contains(t, command, "-- wrapped 2>&1 | tee -a /tmp/test.log")
		})
	}
}

func TestBuildAWFCommandScript_RetriesEngineStartupFailuresOutsideHarness(t *testing.T) {
	input := buildAWFCommandScriptInput{
		writeAgentCLIStartMs: "start",
		preCreateLog:         "pre",
		awfCommand:           "awf",
		expandableArgs:       "--expand",
		awfArgs:              []string{"--arg", "value"},
		shellWrappedCommand:  "wrapped",
		logFile:              "/tmp/test.log",
		engineName:           "codex",
		retryStartupFailures: true,
	}

	command := buildAWFCommandScript(input)

	assert.Contains(t, command, `bash "${RUNNER_TEMP}/gh-aw/actions/run_awf_with_startup_retries.sh" --`)
	assert.Contains(t, command, `GH_AW_AWF_ENGINE_NAME=codex`)
	assert.Contains(t, command, `GH_AW_AWF_HARNESS_MARKER='[codex-harness]'`)
	assert.Contains(t, command, `GH_AW_AWF_LOG_FILE=/tmp/test.log`)
	assert.Contains(t, command, `GH_AW_AWF_ATTEMPT_LOG_NAME=codex`)
	assert.Contains(t, command, "awf --expand   --arg value \\\n  -- wrapped")
	assert.NotContains(t, command, "while true; do")
	assert.NotContains(t, command, "Fatal error:|Process exiting with code:|Refusing to use symlink as bind mountpoint")
}

func TestBuiltInEngineAWFWrapsOuterInvocationWithStartupRetry(t *testing.T) {
	tests := []struct {
		name          string
		engineID      string
		buildStep     func(*WorkflowData) []GitHubActionStep
		harnessMarker string
	}{
		{
			name:     "claude",
			engineID: "claude",
			buildStep: func(workflowData *WorkflowData) []GitHubActionStep {
				return NewClaudeEngine().GetExecutionSteps(workflowData, "/tmp/gh-aw/agent-stdio.log")
			},
			harnessMarker: "[claude-harness]",
		},
		{
			name:     "codex",
			engineID: "codex",
			buildStep: func(workflowData *WorkflowData) []GitHubActionStep {
				return NewCodexEngine().GetExecutionSteps(workflowData, "/tmp/gh-aw/agent-stdio.log")
			},
			harnessMarker: "[codex-harness]",
		},
		{
			name:     "copilot",
			engineID: "copilot",
			buildStep: func(workflowData *WorkflowData) []GitHubActionStep {
				return NewCopilotEngine().GetExecutionSteps(workflowData, "/tmp/gh-aw/agent-stdio.log")
			},
			harnessMarker: "[copilot-harness]",
		},
		{
			name:     "gemini",
			engineID: "gemini",
			buildStep: func(workflowData *WorkflowData) []GitHubActionStep {
				return NewGeminiEngine().GetExecutionSteps(workflowData, "/tmp/gh-aw/agent-stdio.log")
			},
			harnessMarker: "[gemini-harness]",
		},
		{
			name:     "pi",
			engineID: "pi",
			buildStep: func(workflowData *WorkflowData) []GitHubActionStep {
				return NewPiEngine().GetExecutionSteps(workflowData, "/tmp/gh-aw/agent-stdio.log")
			},
			harnessMarker: "[pi-harness]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowData := &WorkflowData{
				Name:         "test-workflow",
				EngineConfig: &EngineConfig{ID: tt.engineID},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{Type: SandboxTypeAWF},
				},
			}

			steps := tt.buildStep(workflowData)
			if len(steps) == 0 {
				t.Fatalf("Expected at least 1 execution step")
			}
			stepContent := strings.Join([]string(steps[len(steps)-1]), "\n")

			assert.Contains(t, stepContent, `bash "${RUNNER_TEMP}/gh-aw/actions/run_awf_with_startup_retries.sh" --`)
			assert.Contains(t, stepContent, `GH_AW_AWF_ENGINE_NAME=`+tt.engineID)
			assert.Contains(t, stepContent, `GH_AW_AWF_HARNESS_MARKER='`+tt.harnessMarker+`'`)
			assert.Contains(t, stepContent, `GH_AW_AWF_ATTEMPT_LOG_NAME=`+tt.engineID)
		})
	}
}

func argValue(args []string, flag string) string {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
