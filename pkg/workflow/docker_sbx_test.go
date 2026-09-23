//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateDockerSbxInstallSteps verifies that all docker-sbx install step
// generators produce non-empty output with the expected step names and script references.
func TestGenerateDockerSbxInstallSteps(t *testing.T) {
	t.Run("KVM check step", func(t *testing.T) {
		step := generateDockerSbxKVMCheckStep()
		require.NotEmpty(t, step, "KVM check step must not be empty")
		content := strings.Join(step, "\n")
		assert.Contains(t, content, "Check KVM availability for docker-sbx", "must have correct step name")
		assert.Contains(t, content, "docker_sbx_kvm_check.sh", "must reference kvm check script")
		assert.Contains(t, content, "${RUNNER_TEMP}/gh-aw/actions/", "must use RUNNER_TEMP script path")
	})

	t.Run("secrets check step", func(t *testing.T) {
		step := generateDockerSbxSecretsCheckStep()
		require.NotEmpty(t, step, "secrets check step must not be empty")
		content := strings.Join(step, "\n")
		assert.Contains(t, content, "Check Docker Hub secrets for docker-sbx", "must have correct step name")
		assert.Contains(t, content, "id: docker-sbx-secrets", "must expose step outputs")
		assert.Contains(t, content, "docker_sbx_secrets_check.sh", "must reference secrets check script")
		assert.Contains(t, content, "DOCKER_PAT_VAL: ${{ secrets.DOCKER_PAT }}", "must pass DOCKER_PAT via env")
		assert.Contains(t, content, "DOCKER_USERNAME_VAL: ${{ secrets.DOCKER_USERNAME }}", "must pass DOCKER_USERNAME via env")
	})

	t.Run("install step", func(t *testing.T) {
		step := generateDockerSbxInstallStep()
		require.NotEmpty(t, step, "install step must not be empty")
		content := strings.Join(step, "\n")
		assert.Contains(t, content, "Install docker-sbx", "must have correct step name")
		assert.Contains(t, content, "sudo_docker_sbx_install.sh", "must reference sudo install script")
		assert.Contains(t, content, "${RUNNER_TEMP}/gh-aw/actions/", "must use RUNNER_TEMP script path")
	})

	t.Run("auth and daemon step", func(t *testing.T) {
		step := generateDockerSbxAuthAndDaemonStep()
		require.NotEmpty(t, step, "auth and daemon step must not be empty")
		content := strings.Join(step, "\n")
		assert.Contains(t, content, "Start docker-sbx daemon and authenticate", "must have correct step name")
		assert.Contains(t, content, "docker_sbx_daemon.sh", "must reference daemon script")
		assert.Contains(t, content, "DOCKER_PAT_VAL: ${{ secrets.DOCKER_PAT }}", "must pass DOCKER_PAT via env")
		assert.Contains(t, content, "DOCKER_USERNAME_VAL: ${{ secrets.DOCKER_USERNAME }}", "must pass DOCKER_USERNAME via env")
	})

	t.Run("pre-flight step", func(t *testing.T) {
		step := generateDockerSbxPreFlightStep()
		require.NotEmpty(t, step, "pre-flight step must not be empty")
		content := strings.Join(step, "\n")
		assert.Contains(t, content, "Run docker-sbx pre-flight smoke test", "must have correct step name")
		assert.Contains(t, content, "docker_sbx_preflight.sh", "must reference preflight script")
		assert.Contains(t, content, "${RUNNER_TEMP}/gh-aw/actions/", "must use RUNNER_TEMP script path")
	})

	t.Run("credential refresh step", func(t *testing.T) {
		step := generateDockerSbxCredentialRefreshStep()
		require.NotEmpty(t, step, "credential refresh step must not be empty")
		content := strings.Join(step, "\n")
		assert.Contains(t, content, "Refresh sbx credentials", "must have correct step name")
		assert.Contains(t, content, "docker_sbx_credential_refresh.sh", "must reference credential refresh script")
		assert.Contains(t, content, "DOCKER_PAT_VAL: ${{ secrets.DOCKER_PAT }}", "must pass DOCKER_PAT via env")
		assert.Contains(t, content, "DOCKER_USERNAME_VAL: ${{ secrets.DOCKER_USERNAME }}", "must pass DOCKER_USERNAME via env")
	})
}

// TestDockerSbxInstallStepOrderInBuildNpmEngineInstallStepsWithAWF verifies that all
// docker-sbx pre-flight steps are emitted BEFORE the AWF install step.
func TestDockerSbxInstallStepOrderInBuildNpmEngineInstallStepsWithAWF(t *testing.T) {
	workflowData := &WorkflowData{
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{
				ID:      "awf",
				Runtime: AgentRuntimeDockerSbx,
			},
		},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true},
		},
	}

	steps := BuildNpmEngineInstallStepsWithAWF(nil, workflowData)
	require.NotEmpty(t, steps, "must generate installation steps")

	// Locate the key steps by their content.
	kvmIdx := -1
	secretsIdx := -1
	installIdx := -1
	authIdx := -1
	preflightIdx := -1
	awfIdx := -1
	for i, step := range steps {
		content := strings.Join(step, "\n")
		switch {
		case strings.Contains(content, "Check KVM availability"):
			kvmIdx = i
		case strings.Contains(content, "Check Docker Hub secrets"):
			secretsIdx = i
		case strings.Contains(content, "Install docker-sbx"):
			installIdx = i
		case strings.Contains(content, "Start docker-sbx daemon"):
			authIdx = i
		case strings.Contains(content, "pre-flight smoke test"):
			preflightIdx = i
		case strings.Contains(content, "install_awf_binary.sh"):
			awfIdx = i
		}
	}

	require.NotEqual(t, -1, kvmIdx, "KVM check step must be present")
	require.NotEqual(t, -1, secretsIdx, "secrets check step must be present")
	require.NotEqual(t, -1, installIdx, "docker-sbx install step must be present")
	require.NotEqual(t, -1, authIdx, "auth and daemon step must be present")
	require.NotEqual(t, -1, preflightIdx, "pre-flight step must be present")
	require.NotEqual(t, -1, awfIdx, "AWF install step must be present")

	// All docker-sbx steps must precede the AWF install step.
	assert.Less(t, kvmIdx, awfIdx, "KVM check step must come before AWF install")
	assert.Less(t, secretsIdx, awfIdx, "secrets check step must come before AWF install")
	assert.Less(t, installIdx, awfIdx, "docker-sbx install step must come before AWF install")
	assert.Less(t, authIdx, awfIdx, "auth and daemon step must come before AWF install")
	assert.Less(t, preflightIdx, awfIdx, "pre-flight step must come before AWF install")
	// And they must be in the correct logical order relative to each other.
	assert.Less(t, kvmIdx, secretsIdx, "KVM check must come before secrets check")
	assert.Less(t, secretsIdx, installIdx, "secrets check must come before install")
	assert.Less(t, installIdx, authIdx, "install must come before auth/daemon")
	assert.Less(t, authIdx, preflightIdx, "auth/daemon must come before pre-flight")
}

// TestDockerSbxAWFArgs verifies that --container-runtime sbx is added to AWF args
// when docker-sbx runtime is configured.
func TestDockerSbxAWFArgs(t *testing.T) {
	config := AWFCommandConfig{
		EngineName: "copilot",
		WorkflowData: &WorkflowData{
			EngineConfig: &EngineConfig{ID: "copilot"},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true, Version: string(constants.AWFContainerRuntimeMinVersion)},
			},
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					ID:      "awf",
					Runtime: AgentRuntimeDockerSbx,
				},
			},
		},
	}

	args := BuildAWFArgs(config)

	// Must include --container-runtime sbx
	found := false
	for i, arg := range args {
		if arg == "--container-runtime" && i+1 < len(args) && args[i+1] == "sbx" {
			found = true
			break
		}
	}
	assert.True(t, found, "AWF args must include --container-runtime sbx for docker-sbx runtime")
}

func TestDockerSbxAWFArgsSuppressesTTY(t *testing.T) {
	config := AWFCommandConfig{
		EngineName: "claude",
		UsesTTY:    true,
		WorkflowData: &WorkflowData{
			EngineConfig: &EngineConfig{ID: "claude"},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true, Version: string(constants.AWFContainerRuntimeMinVersion)},
			},
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					ID:      "awf",
					Runtime: AgentRuntimeDockerSbx,
				},
			},
		},
	}

	args := BuildAWFArgs(config)
	assert.NotContains(t, strings.Join(args, " "), "--tty", "docker-sbx must suppress --tty to avoid sbx pty timeouts")
}

// TestDockerSbxAWFArgsAbsentByDefault verifies that --container-runtime sbx is NOT
// added when no runtime is configured.
func TestDockerSbxAWFArgsAbsentByDefault(t *testing.T) {
	config := AWFCommandConfig{
		EngineName: "copilot",
		WorkflowData: &WorkflowData{
			EngineConfig: &EngineConfig{ID: "copilot"},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true},
			},
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					ID: "awf",
				},
			},
		},
	}

	args := BuildAWFArgs(config)
	argStr := strings.Join(args, " ")
	assert.NotContains(t, argStr, "--container-runtime", "AWF args must not include --container-runtime when no runtime is set")
}

// TestDockerSbxAWFArgsVersionGated verifies that --container-runtime sbx is omitted when
// the effective AWF version predates AWFContainerRuntimeMinVersion.
func TestDockerSbxAWFArgsVersionGated(t *testing.T) {
	config := AWFCommandConfig{
		EngineName: "copilot",
		WorkflowData: &WorkflowData{
			EngineConfig: &EngineConfig{ID: "copilot"},
			NetworkPermissions: &NetworkPermissions{
				// Pin to a version that predates containerRuntime support.
				Firewall: &FirewallConfig{Enabled: true, Version: "v0.27.29"},
			},
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					ID:      "awf",
					Runtime: AgentRuntimeDockerSbx,
				},
			},
		},
	}

	args := BuildAWFArgs(config)
	assert.NotContains(t, strings.Join(args, " "), "--container-runtime",
		"AWF args must omit --container-runtime when the AWF version is too old")
}

// TestDockerSbxAWFConfigJSON verifies that the AWF config JSON for docker-sbx does NOT
// include containerRuntime (docker-sbx is not an OCI runtime) but DOES include
// host.docker.internal in allowDomains and sets network.isolation: true.
func TestDockerSbxAWFConfigJSON(t *testing.T) {
	config := AWFCommandConfig{
		EngineName:     "copilot",
		AllowedDomains: "github.com",
		WorkflowData: &WorkflowData{
			EngineConfig:   &EngineConfig{ID: "copilot"},
			TimeoutMinutes: "timeout-minutes: 30",
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true},
			},
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					ID:      "awf",
					Runtime: AgentRuntimeDockerSbx,
				},
			},
		},
	}

	jsonStr, err := BuildAWFConfigJSON(config)
	require.NoError(t, err)

	// docker-sbx is not an OCI runtime — containerRuntime must NOT appear in JSON.
	assert.NotContains(t, jsonStr, `"containerRuntime"`,
		"docker-sbx must not set container.containerRuntime in AWF config JSON")

	// host.docker.internal must be in network.allowDomains so the microVM can
	// reach host-published services (api-proxy, MCP gateway, Squid proxy).
	assert.Contains(t, jsonStr, "host.docker.internal",
		"AWF config JSON must include host.docker.internal in network.allowDomains for docker-sbx")

	// network.isolation must be true for docker-sbx.
	assert.Contains(t, jsonStr, `"isolation":true`,
		"AWF config JSON must have network.isolation: true for docker-sbx")

	// docker-sbx must pass a concrete agent timeout to AWF.
	assert.Contains(t, jsonStr, `"agentTimeout":30`,
		"AWF config JSON must include container.agentTimeout for docker-sbx")
}

func TestDockerSbxEngineCLIWiring(t *testing.T) {
	workflowData := &WorkflowData{
		Name:          "test-workflow",
		EngineConfig:  &EngineConfig{ID: "claude"},
		SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{ID: "awf", Runtime: AgentRuntimeDockerSbx}},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true},
		},
	}

	t.Run("claude install and execution use sbx-visible CLI path", func(t *testing.T) {
		engine := NewClaudeEngine()
		installSteps := engine.GetInstallationSteps(workflowData)
		installContent := strings.Join(flattenSteps(installSteps), "\n")
		assert.Contains(t, installContent, `npm install --prefix "${RUNNER_TEMP}/gh-aw/engine-cli" @anthropic-ai/claude-code@`+string(constants.DefaultClaudeCodeVersion))
		assert.Contains(t, installContent, `ln -sf "../node_modules/.bin/claude" "${RUNNER_TEMP}/gh-aw/engine-cli/bin/claude"`)

		execSteps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
		require.NotEmpty(t, execSteps)
		execContent := strings.Join(execSteps[0], "\n")
		assert.Contains(t, execContent, `export PATH="${RUNNER_TEMP}/gh-aw/engine-cli/bin:$PATH"`)
	})

	t.Run("docker-sbx keeps engine and MCP CLI PATH setup independent", func(t *testing.T) {
		workflowData.ParsedTools = &ToolsConfig{
			CLIProxy: true,
			Custom: map[string]MCPServerConfig{
				"myserver": {},
			},
		}
		workflowData.Tools = map[string]any{
			"myserver": map[string]any{
				"mode": "remote",
			},
		}

		engine := NewClaudeEngine()
		execSteps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
		require.NotEmpty(t, execSteps)
		execContent := strings.Join(execSteps[0], "\n")

		assert.Contains(t, execContent, `export PATH="${RUNNER_TEMP}/gh-aw/mcp-cli/bin:$PATH"`)
		assert.Contains(t, execContent, `export PATH="${RUNNER_TEMP}/gh-aw/engine-cli/bin:$PATH"`)

		mcpIdx := strings.Index(execContent, `export PATH="${RUNNER_TEMP}/gh-aw/mcp-cli/bin:$PATH"`)
		engineIdx := strings.Index(execContent, `export PATH="${RUNNER_TEMP}/gh-aw/engine-cli/bin:$PATH"`)
		assert.GreaterOrEqual(t, mcpIdx, 0)
		assert.GreaterOrEqual(t, engineIdx, 0)
		assert.Less(t, mcpIdx, engineIdx, "engine path export must run after MCP export so engine CLI takes PATH precedence")
	})

	t.Run("codex install and execution use sbx-visible CLI path", func(t *testing.T) {
		engine := NewCodexEngine()
		workflowData.EngineConfig = &EngineConfig{ID: "codex"}
		installSteps := engine.GetInstallationSteps(workflowData)
		installContent := strings.Join(flattenSteps(installSteps), "\n")
		assert.Contains(t, installContent, `npm install --ignore-scripts --prefix "${RUNNER_TEMP}/gh-aw/engine-cli" @openai/codex@`+string(constants.DefaultCodexVersion))
		assert.Contains(t, installContent, `ln -sf "../node_modules/.bin/codex" "${RUNNER_TEMP}/gh-aw/engine-cli/bin/codex"`)

		execSteps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
		require.NotEmpty(t, execSteps)
		execContent := strings.Join(execSteps[0], "\n")
		assert.Contains(t, execContent, `export PATH="${RUNNER_TEMP}/gh-aw/engine-cli/bin:$PATH"`)
	})

	t.Run("pi install and execution use sbx-visible CLI path", func(t *testing.T) {
		engine := NewPiEngine()
		workflowData.EngineConfig = &EngineConfig{ID: "pi"}
		installSteps := engine.GetInstallationSteps(workflowData)
		installContent := strings.Join(flattenSteps(installSteps), "\n")
		assert.Contains(t, installContent, `npm install --ignore-scripts --prefix "${RUNNER_TEMP}/gh-aw/engine-cli" @earendil-works/pi-coding-agent@`+string(constants.DefaultPiVersion))
		assert.Contains(t, installContent, `ln -sf "../node_modules/.bin/pi" "${RUNNER_TEMP}/gh-aw/engine-cli/bin/pi"`)

		execSteps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
		require.NotEmpty(t, execSteps)
		execContent := strings.Join(execSteps[0], "\n")
		assert.Contains(t, execContent, `export PATH="${RUNNER_TEMP}/gh-aw/engine-cli/bin:$PATH"`)
	})

	t.Run("pi execution uses sbx-visible CLI path even with firewall disabled", func(t *testing.T) {
		// sandbox.agent.runtime: docker-sbx can be configured alongside an explicit
		// network.firewall: false, which takes the non-AWF execution path. The
		// staged CLI PATH export must still be present so the microVM can see `pi`.
		nonFirewallWorkflowData := &WorkflowData{
			Name:          "test-workflow",
			EngineConfig:  &EngineConfig{ID: "pi"},
			SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{ID: "awf", Runtime: AgentRuntimeDockerSbx}},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: false},
			},
		}

		engine := NewPiEngine()
		execSteps := engine.GetExecutionSteps(nonFirewallWorkflowData, "/tmp/gh-aw/test.log")
		require.NotEmpty(t, execSteps)
		execContent := strings.Join(execSteps[0], "\n")
		assert.Contains(t, execContent, `export PATH="${RUNNER_TEMP}/gh-aw/engine-cli/bin:$PATH"`)
	})
}

// flattenSteps joins a small slice of GitHubActionStep values so docker-sbx tests can
// assert across multi-step install blocks without repeating nested loops in each case.
func flattenSteps(steps []GitHubActionStep) []string {
	var lines []string
	for _, step := range steps {
		lines = append(lines, step...)
	}
	return lines
}

// TestDockerSbxNetworkIsolationAlwaysTrue verifies that the docker-sbx runtime profile
// always enables network isolation.
func TestDockerSbxNetworkIsolationAlwaysTrue(t *testing.T) {
	workflowData := &WorkflowData{
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{
				ID:      "awf",
				Runtime: AgentRuntimeDockerSbx,
			},
		},
	}

	assert.True(t, isAWFNetworkIsolationEnabled(workflowData),
		"docker-sbx must always use network isolation")
}

// TestDockerSbxContainerRuntimeEmpty verifies that getAgentContainerRuntime returns
// an empty string for docker-sbx (it is not an OCI runtime).
func TestDockerSbxContainerRuntimeEmpty(t *testing.T) {
	workflowData := &WorkflowData{
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{
				ID:      "awf",
				Runtime: AgentRuntimeDockerSbx,
			},
		},
	}

	assert.Empty(t, getAgentContainerRuntime(workflowData),
		"docker-sbx must return empty string from getAgentContainerRuntime")
}

// TestDockerSbxValidation_ArcDindIncompatible verifies that docker-sbx + arc-dind is
// a compile-time error.
func TestDockerSbxValidation_ArcDindIncompatible(t *testing.T) {
	workflowData := &WorkflowData{
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{
				ID:      "awf",
				Runtime: AgentRuntimeDockerSbx,
			},
		},
		RunnerConfig: &RunnerConfig{Topology: RunnerTopologyArcDind},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true},
		},
		Tools: map[string]any{"github": map[string]any{"mode": "remote"}},
	}

	err := validateSandboxConfig(workflowData)
	require.Error(t, err, "docker-sbx + arc-dind must produce a compile-time error")
	require.ErrorContains(t, err, "arc-dind", "error must mention arc-dind")
	require.ErrorContains(t, err, "docker-sbx", "error must mention docker-sbx")
}

// TestDockerSbxValidation_RuntimeInstallFalseAllowsPreinstalledRuntime verifies that
// preinstalled docker-sbx runners can skip installation.
func TestDockerSbxValidation_RuntimeInstallFalseAllowsPreinstalledRuntime(t *testing.T) {
	falseVal := false
	workflowData := &WorkflowData{
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{
				ID:             "awf",
				Runtime:        AgentRuntimeDockerSbx,
				RuntimeInstall: &falseVal,
			},
		},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true, Version: "v0.28.0"},
		},
		Tools: map[string]any{"github": map[string]any{"mode": "remote"}},
	}

	err := validateSandboxConfig(workflowData)
	require.NoError(t, err, "docker-sbx with runtime-install: false should allow preinstalled runtimes without sudo: true")
}

// TestDockerSbxValidation_DefaultVersionRejected verifies that docker-sbx is rejected
// when the effective AWF version predates --container-runtime support.
func TestDockerSbxValidation_DefaultVersionRejected(t *testing.T) {
	workflowData := &WorkflowData{
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{
				ID:      "awf",
				Runtime: AgentRuntimeDockerSbx,
				// Pin to a version that predates containerRuntime support.
				Version: "v0.27.29",
			},
		},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true},
		},
		Tools: map[string]any{"github": map[string]any{"mode": "remote"}},
	}

	err := validateSandboxConfig(workflowData)
	require.Error(t, err, "docker-sbx with an AWF version predating containerRuntime support must fail validation")
	require.ErrorContains(t, err, string(constants.AWFContainerRuntimeMinVersion))
}

// TestDockerSbxValidation_MinVersionSatisfied verifies that docker-sbx passes validation
// when the effective AWF version supports --container-runtime.
func TestDockerSbxValidation_MinVersionSatisfied(t *testing.T) {
	workflowData := &WorkflowData{
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{
				ID:      "awf",
				Runtime: AgentRuntimeDockerSbx,
				Version: string(constants.AWFContainerRuntimeMinVersion),
			},
		},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true},
		},
		Tools: map[string]any{"github": map[string]any{"mode": "remote"}},
	}

	err := validateSandboxConfig(workflowData)
	assert.NoError(t, err, "docker-sbx with a supported AWF version must pass validation")
}

// TestDockerSbxStrictModeSudoSuppressed verifies that runtime: docker-sbx does NOT
// produce a strict-mode error: the compiler derives the required install privileges
// from the runtime profile.
func TestDockerSbxStrictModeSudoSuppressed(t *testing.T) {
	sandboxConfig := &SandboxConfig{
		Agent: &AgentSandboxConfig{
			ID:      "awf",
			Runtime: AgentRuntimeDockerSbx,
		},
	}

	compiler := NewCompiler()
	compiler.strictMode = true

	err := compiler.validateStrictSandboxCustomization(sandboxConfig)
	assert.NoError(t, err, "runtime:docker-sbx must NOT produce a strict-mode error")
}

// TestIsDockerSbxRuntime verifies the isDockerSbxRuntime helper.
func TestIsDockerSbxRuntime(t *testing.T) {
	t.Run("returns false for nil workflow data", func(t *testing.T) {
		assert.False(t, isDockerSbxRuntime(nil))
	})

	t.Run("returns false when no sandbox config", func(t *testing.T) {
		assert.False(t, isDockerSbxRuntime(&WorkflowData{}))
	})

	t.Run("returns false when runtime is not docker-sbx", func(t *testing.T) {
		assert.False(t, isDockerSbxRuntime(&WorkflowData{
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{ID: "awf"},
			},
		}))
	})

	t.Run("returns false when agent is disabled", func(t *testing.T) {
		assert.False(t, isDockerSbxRuntime(&WorkflowData{
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					ID:       "awf",
					Runtime:  AgentRuntimeDockerSbx,
					Disabled: true,
				},
			},
		}))
	})

	t.Run("returns true when runtime is docker-sbx", func(t *testing.T) {
		assert.True(t, isDockerSbxRuntime(&WorkflowData{
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					ID:      "awf",
					Runtime: AgentRuntimeDockerSbx,
				},
			},
		}))
	})
}

// TestDockerSbxFrontmatterExtraction verifies end-to-end that a workflow with
// sandbox.agent.runtime: docker-sbx compiles correctly and produces the expected output.
func TestDockerSbxFrontmatterExtraction(t *testing.T) {
	workflowsDir := t.TempDir()

	markdown := `---
on:
  workflow_dispatch:
engine: copilot
strict: false
network:
  allowed:
    - "example.com"
sandbox:
  agent:
    id: awf
    runtime: docker-sbx
    version: v0.28.0
---

# Test docker-sbx Runtime
`

	testFile := filepath.Join(workflowsDir, "test-docker-sbx.md")
	err := os.WriteFile(testFile, []byte(markdown), 0644)
	require.NoError(t, err)

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "compilation with runtime: docker-sbx must succeed")

	lockContent, err := os.ReadFile(filepath.Join(workflowsDir, "test-docker-sbx.lock.yml"))
	require.NoError(t, err)
	lockStr := string(lockContent)

	// KVM check step must be present.
	assert.Contains(t, lockStr, "Check KVM availability", "compiled workflow must include KVM availability check")
	// Secrets check step must be present.
	assert.Contains(t, lockStr, "Check Docker Hub secrets", "compiled workflow must include Docker Hub secrets check")
	assert.Contains(t, lockStr, "docker_sbx_secrets_result: ${{ steps.docker-sbx-secrets.outputs.verification_result }}", "activation job must expose docker-sbx secret check result")
	assert.Contains(t, lockStr, "DOCKER_SBX_SECRETS_SOFT_FAIL: 'true'", "activation docker-sbx secret check must soft-fail")
	assert.Contains(t, lockStr, "needs.activation.outputs.docker_sbx_secrets_result != 'failed'", "agent job must skip when docker-sbx secrets are missing")
	assert.Contains(t, lockStr, "needs.activation.outputs.docker_sbx_secrets_result == 'failed'", "conclusion job must run when docker-sbx secrets are missing")
	assert.Contains(t, lockStr, "GH_AW_DOCKER_SBX_SECRETS_RESULT: ${{ needs.activation.outputs.docker_sbx_secrets_result }}", "conclusion job must receive docker-sbx secret check result")
	// docker-sbx install step must be present.
	assert.Contains(t, lockStr, "Install docker-sbx", "compiled workflow must include docker-sbx install step")
	// Auth and daemon step must be present.
	assert.Contains(t, lockStr, "Start docker-sbx daemon", "compiled workflow must include sbx daemon step")
	// Pre-flight step must be present.
	assert.Contains(t, lockStr, "pre-flight smoke test", "compiled workflow must include pre-flight step")
	// AWF install step must also be present.
	assert.Contains(t, lockStr, "Install AWF binary", "compiled workflow must include AWF install step")

	// All docker-sbx steps must appear before AWF install step.
	kvmPos := strings.Index(lockStr, "Check KVM availability")
	awfPos := strings.Index(lockStr, "Install AWF binary")
	assert.Less(t, kvmPos, awfPos, "KVM check step must precede AWF install step")

	// containerRuntime must NOT appear (docker-sbx is not an OCI runtime).
	assert.NotContains(t, lockStr, `"containerRuntime"`, "containerRuntime must not appear for docker-sbx")

	// host.docker.internal must be in the allowed domains.
	assert.Contains(t, lockStr, "host.docker.internal", "host.docker.internal must be in allowed domains")

	// --container-runtime sbx must appear in the AWF invocation.
	assert.Contains(t, lockStr, "--container-runtime sbx", "AWF invocation must include --container-runtime sbx")

	// Credential refresh step must be present.
	assert.Contains(t, lockStr, "Refresh sbx credentials", "compiled workflow must include credential refresh step before execution")

	// Credential refresh step must appear AFTER pre-flight but BEFORE execution.
	refreshPos := strings.Index(lockStr, "Refresh sbx credentials")
	preflightPos := strings.Index(lockStr, "pre-flight smoke test")
	execPos := strings.Index(lockStr, "agentic_execution")
	assert.Greater(t, refreshPos, preflightPos, "credential refresh must come after pre-flight step")
	assert.Less(t, refreshPos, execPos, "credential refresh must come before agent execution step")
}

// TestDockerSbxFrontmatterExtractionRuntimeInstallFalse verifies direct workflow
// frontmatter accepts runtime-install: false and still emits credential refresh.
func TestDockerSbxFrontmatterExtractionRuntimeInstallFalse(t *testing.T) {
	workflowsDir := t.TempDir()

	markdown := `---
on:
  workflow_dispatch:
engine: copilot
strict: false
network:
  allowed:
    - "example.com"
sandbox:
  agent:
    id: awf
    runtime: docker-sbx
    runtime-install: false
    version: v0.28.0
---

# Test preinstalled docker-sbx Runtime
`

	testFile := filepath.Join(workflowsDir, "test-docker-sbx-runtime-install-false.md")
	err := os.WriteFile(testFile, []byte(markdown), 0o644)
	require.NoError(t, err)

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "compilation with runtime-install: false must succeed without sandbox.agent.sudo")

	lockContent, err := os.ReadFile(filepath.Join(workflowsDir, "test-docker-sbx-runtime-install-false.lock.yml"))
	require.NoError(t, err)
	lockStr := string(lockContent)

	assert.NotContains(t, lockStr, "Check KVM availability", "compiled workflow must omit KVM check when runtime-install: false")
	assert.Contains(t, lockStr, "Check Docker Hub secrets", "compiled workflow must include activation Docker Hub secrets check when runtime-install: false")
	assert.Contains(t, lockStr, "docker_sbx_secrets_result: ${{ steps.docker-sbx-secrets.outputs.verification_result }}", "activation job must expose docker-sbx secret check result when runtime-install: false")
	assert.NotContains(t, lockStr, "Install docker-sbx", "compiled workflow must omit docker-sbx install when runtime-install: false")
	assert.NotContains(t, lockStr, "Start docker-sbx daemon", "compiled workflow must omit sbx daemon step when runtime-install: false")
	assert.NotContains(t, lockStr, "pre-flight smoke test", "compiled workflow must omit pre-flight step when runtime-install: false")
	assert.Contains(t, lockStr, "Refresh sbx credentials", "compiled workflow must still include credential refresh when runtime-install: false")
}

// TestDockerSbxShellScriptContent verifies that the shell scripts referenced by
// the step generators exist and contain the expected key operations.
func TestDockerSbxShellScriptContent(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)
	shDir := filepath.Join(wd, "..", "..", "actions", "setup", "sh")

	tests := []struct {
		script      string
		contains    []string
		notContains []string
	}{
		{
			script: "docker_sbx_kvm_check.sh",
			contains: []string{
				"lsmod", "kvm", "/dev/kvm", "exit 1",
			},
		},
		{
			script: "docker_sbx_secrets_check.sh",
			contains: []string{
				"DOCKER_PAT_VAL", "DOCKER_USERNAME_VAL", "DOCKER_SBX_SECRETS_SOFT_FAIL", "verification_result=failed", "exit 1",
			},
		},
		{
			script: "sudo_docker_sbx_install.sh",
			contains: []string{
				"sudo", "apt-get install", "docker-sbx", "sbx version", "/dev/kvm",
				"DOCKER_GPG_FINGERPRINT", "gpg --show-keys", "mktemp", "signed-by=",
				"VERSION_CODENAME", "download.docker.com",
			},
			notContains: []string{"get.docker.com", "| sudo REPO_ONLY=1 sh"},
		},
		{
			script: "docker_sbx_daemon.sh",
			contains: []string{
				"sbx daemon start", "docker login", "sbx login",
				"sbx policy reset", "sbx policy init allow-all",
				"DOCKER_PAT_VAL", "DOCKER_USERNAME_VAL",
				"DOCKER_CONFIG", "mktemp",
				"for _ in $(seq 1 10); do",
				"exit 1", // must fail fast when daemon does not start
			},
		},
		{
			script: "docker_sbx_preflight.sh",
			contains: []string{
				"sbx create", "test-sandbox-direct", "sbx exec", "uname -a",
				"trap cleanup EXIT", "sbx stop", "sbx rm",
			},
		},
		{
			script: "docker_sbx_credential_refresh.sh",
			contains: []string{
				"sbx login", "DOCKER_PAT_VAL", "DOCKER_USERNAME_VAL",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.script, func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join(shDir, tc.script))
			require.NoError(t, err, "script file must exist: %s", tc.script)
			for _, s := range tc.contains {
				assert.Contains(t, string(content), s, "script %s must contain %q", tc.script, s)
			}
			for _, s := range tc.notContains {
				assert.NotContains(t, string(content), s, "script %s must not contain %q", tc.script, s)
			}
		})
	}
}

// TestSudoDockerSbxInstallScriptRequiresSudo verifies that the install script
// is named with the sudo_ prefix (indicating it requires elevated privileges) and
// that it actually invokes sudo.
func TestSudoDockerSbxInstallScriptRequiresSudo(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)
	script := filepath.Join(wd, "..", "..", "actions", "setup", "sh", "sudo_docker_sbx_install.sh")
	content, err := os.ReadFile(script)
	require.NoError(t, err, "sudo_docker_sbx_install.sh must exist")
	assert.Contains(t, string(content), "sudo", "install script must invoke sudo")
}

// TestIsRuntimeInstallEnabled verifies the helper returns true by default and
// respects the RuntimeInstall field only when a runtime is configured.
func TestIsRuntimeInstallEnabled(t *testing.T) {
	falseVal := false
	trueVal := true

	t.Run("nil workflowData returns true", func(t *testing.T) {
		assert.True(t, isRuntimeInstallEnabled(nil))
	})

	t.Run("no runtime set - always true regardless of field", func(t *testing.T) {
		wd := &WorkflowData{
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					RuntimeInstall: &falseVal, // set to false but no runtime
				},
			},
		}
		assert.True(t, isRuntimeInstallEnabled(wd), "noop when runtime is not set")
	})

	t.Run("runtime set, field nil - defaults to true", func(t *testing.T) {
		wd := &WorkflowData{
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{Runtime: AgentRuntimeDockerSbx},
			},
		}
		assert.True(t, isRuntimeInstallEnabled(wd))
	})

	t.Run("runtime set, runtime-install: true - returns true", func(t *testing.T) {
		wd := &WorkflowData{
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Runtime:        AgentRuntimeDockerSbx,
					RuntimeInstall: &trueVal,
				},
			},
		}
		assert.True(t, isRuntimeInstallEnabled(wd))
	})

	t.Run("runtime set, runtime-install: false - returns false", func(t *testing.T) {
		wd := &WorkflowData{
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Runtime:        AgentRuntimeDockerSbx,
					RuntimeInstall: &falseVal,
				},
			},
		}
		assert.False(t, isRuntimeInstallEnabled(wd))
	})

	t.Run("gvisor runtime, runtime-install: false - returns false", func(t *testing.T) {
		wd := &WorkflowData{
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Runtime:        AgentRuntimeGVisor,
					RuntimeInstall: &falseVal,
				},
			},
		}
		assert.False(t, isRuntimeInstallEnabled(wd))
	})
}

// TestDockerSbxRuntimeInstallFalseOmitsInstallSteps verifies that when
// runtime-install: false is set, the five installation steps (KVM check, secrets
// check, install, daemon, pre-flight) are omitted from the install-step builder.
// Credential refresh is validated separately in TestDockerSbxFrontmatterExtractionRuntimeInstallFalse.
func TestDockerSbxRuntimeInstallFalseOmitsInstallSteps(t *testing.T) {
	falseVal := false
	workflowData := &WorkflowData{
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{
				ID:             "awf",
				Runtime:        AgentRuntimeDockerSbx,
				RuntimeInstall: &falseVal,
			},
		},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true},
		},
	}

	steps := BuildNpmEngineInstallStepsWithAWF(nil, workflowData)
	content := strings.Join(flattenSteps(steps), "\n")

	// Install steps must be absent.
	assert.NotContains(t, content, "Check KVM availability", "KVM check must be omitted when runtime-install: false")
	assert.NotContains(t, content, "Check Docker Hub secrets", "secrets check must be omitted when runtime-install: false")
	assert.NotContains(t, content, "sudo_docker_sbx_install.sh", "install step must be omitted when runtime-install: false")
	assert.NotContains(t, content, "docker_sbx_daemon.sh", "daemon step must be omitted when runtime-install: false")
	assert.NotContains(t, content, "docker-sbx pre-flight smoke test", "pre-flight must be omitted when runtime-install: false")
}

// TestDockerSbxBehaviorDefinedEngineCLIWiring verifies that behavior-defined engines
// installed through npm (e.g. Crush) also stage their CLI into the sbx-visible path and
// prepend it to the sandbox PATH. Without this the CLI is only present in the runner tool
// cache, which is not mounted into the microVM, and the harness fails with ENOENT.
func TestDockerSbxBehaviorDefinedEngineCLIWiring(t *testing.T) {
	def := &EngineDefinition{
		ID:          "sbxcrush",
		DisplayName: "SbxCrush",
		Behaviors: &EngineBehaviorDefinition{
			Installation: &EngineInstallationDefinition{
				PackageManager:     "npm",
				PackageName:        "@charmland/crush",
				Version:            "0.88.0",
				StepName:           "Install SbxCrush",
				IncludeNodeSetup:   true,
				PostInstallScripts: true,
			},
			Execution: &EngineExecutionDefinition{
				CommandName: "sbxcrush",
				Args:        []string{"run"},
				StepName:    "Execute SbxCrush CLI",
			},
			HarnessScript: "// harness\n",
		},
	}
	engine, err := NewBehaviorDefinedEngine(def)
	require.NoError(t, err)

	for _, runtime := range []AgentRuntime{AgentRuntimeDockerSbx, AgentRuntimeCloudHypervisor} {
		t.Run(string(runtime)+" install and execution use sbx-visible CLI path", func(t *testing.T) {
			sbxWorkflow := &WorkflowData{
				Name:          "test-workflow",
				EngineConfig:  &EngineConfig{ID: "sbxcrush"},
				SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{ID: "awf", Runtime: runtime}},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			}

			installContent := strings.Join(flattenSteps(engine.GetInstallationSteps(sbxWorkflow)), "\n")
			assert.Contains(t, installContent, `npm install --prefix "${RUNNER_TEMP}/gh-aw/engine-cli" @charmland/crush@0.88.0`)
			assert.Contains(t, installContent, `ln -sf "../node_modules/.bin/sbxcrush" "${RUNNER_TEMP}/gh-aw/engine-cli/bin/sbxcrush"`)

			execSteps := engine.GetExecutionSteps(sbxWorkflow, "/tmp/gh-aw/test.log")
			require.NotEmpty(t, execSteps)
			execContent := strings.Join(execSteps[len(execSteps)-1], "\n")
			assert.Contains(t, execContent, `export PATH="${RUNNER_TEMP}/gh-aw/engine-cli/bin:$PATH"`)
		})
	}

	t.Run("non-microVM runtimes keep the host install only", func(t *testing.T) {
		defaultWorkflow := &WorkflowData{
			Name:         "test-workflow",
			EngineConfig: &EngineConfig{ID: "sbxcrush"},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true},
			},
		}
		installContent := strings.Join(flattenSteps(engine.GetInstallationSteps(defaultWorkflow)), "\n")
		assert.NotContains(t, installContent, `${RUNNER_TEMP}/gh-aw/engine-cli`)

		execSteps := engine.GetExecutionSteps(defaultWorkflow, "/tmp/gh-aw/test.log")
		require.NotEmpty(t, execSteps)
		execContent := strings.Join(execSteps[len(execSteps)-1], "\n")
		assert.NotContains(t, execContent, `${RUNNER_TEMP}/gh-aw/engine-cli/bin`)
	})
}
