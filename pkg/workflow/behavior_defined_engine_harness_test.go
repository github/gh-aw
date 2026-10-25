//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newHarnessEngineDefinition returns a minimal EngineDefinition with harness-script set.
func newHarnessEngineDefinition() *EngineDefinition {
	return &EngineDefinition{
		ID:          "testharness",
		DisplayName: "TestHarness",
		Description: "A test engine with a harness script",
		Behaviors: &EngineBehaviorDefinition{
			Execution: &EngineExecutionDefinition{
				CommandName: "testharness-cli",
				Args:        []string{"run"},
				StepName:    "Execute TestHarness CLI",
			},
			HarnessScript: `"use strict";
// Minimal test harness
const cmd = process.argv[2];
const { spawnSync } = require("child_process");
spawnSync(cmd, process.argv.slice(3), { stdio: "inherit" });
`,
		},
	}
}

// TestBehaviorDefinedEngineHarnessScript verifies that harness-script is wired correctly
// into the engine's execution steps.
func TestBehaviorDefinedEngineHarnessScript(t *testing.T) {
	def := newHarnessEngineDefinition()
	engine, err := NewBehaviorDefinedEngine(def)
	require.NoError(t, err)

	t.Run("harness_write_step_included", func(t *testing.T) {
		workflowData := &WorkflowData{Name: "test"}
		steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")

		// Steps: [harness write, execution]
		require.Len(t, steps, 2, "should generate harness-write step and execution step")

		harnessStepContent := strings.Join(steps[0], "\n")
		assert.Contains(t, harnessStepContent, "Write TestHarness harness script", "step name should include engine display name")
		assert.Contains(t, harnessStepContent, "testharness_harness.cjs", "step should write the correct harness filename")
		assert.Contains(t, harnessStepContent, "gh-aw/actions", "step should write to the setup action destination directory")
		assert.Contains(t, harnessStepContent, `mkdir -p "${RUNNER_TEMP}/gh-aw/actions"`, "setup action directory should be quoted for shellcheck")
		assert.Contains(t, harnessStepContent, `> "${RUNNER_TEMP}/gh-aw/actions/testharness_harness.cjs"`, "harness output path should be quoted for shellcheck")
		assert.Contains(t, harnessStepContent, `chmod 755 "${RUNNER_TEMP}/gh-aw/actions/testharness_harness.cjs"`, "chmod path should be quoted for shellcheck")
		assert.Contains(t, harnessStepContent, harnessScriptHeredocDelimiter, "step should use heredoc delimiter")
		assert.Contains(t, harnessStepContent, "use strict", "step should embed harness script content")
	})

	t.Run("execution_step_uses_node_harness", func(t *testing.T) {
		workflowData := &WorkflowData{Name: "test"}
		steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
		require.Len(t, steps, 2, "should generate harness-write step and execution step")

		execStepContent := strings.Join(steps[1], "\n")
		assert.Contains(t, execStepContent, "id: agentic_execution", "execution step should have agentic_execution ID")
		assert.Contains(t, execStepContent, "GH_AW_NODE_EXEC", "execution should use node runtime resolution command")
		assert.Contains(t, execStepContent, "testharness_harness.cjs", "execution should invoke the harness file")
		assert.Contains(t, execStepContent, "testharness-cli", "execution should pass command name to harness")
		// When harness is set, inline prompt substitution must NOT appear
		assert.NotContains(t, execStepContent, `"$(cat /tmp/gh-aw/aw-prompts/prompt.txt)"`, "harness execution must not include inline prompt substitution")
	})

	t.Run("awf_reflect_enabled_when_firewall_on", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test",
			NetworkPermissions: &NetworkPermissions{
				Allowed:  []string{"defaults"},
				Firewall: &FirewallConfig{Enabled: true},
			},
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
		require.GreaterOrEqual(t, len(steps), 2, "should have at least harness-write and execution steps")

		// AWF_REFLECT_ENABLED must be set when firewall is on and harness-script is present
		execStepContent := strings.Join(steps[len(steps)-1], "\n")
		assert.Contains(t, execStepContent, "AWF_REFLECT_ENABLED: 1", "AWF_REFLECT_ENABLED must be set when harness-script and firewall are both active")
	})

	t.Run("awf_forced_and_reflect_enabled_without_explicit_firewall", func(t *testing.T) {
		// harness-script always forces AWF so the harness can read /reflect from the API proxy.
		// AWF_REFLECT_ENABLED must therefore be present even when no explicit firewall is configured.
		workflowData := &WorkflowData{Name: "test"}
		steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
		require.GreaterOrEqual(t, len(steps), 2, "should have at least harness-write and execution steps")

		execStepContent := strings.Join(steps[len(steps)-1], "\n")
		assert.Contains(t, execStepContent, "AWF_REFLECT_ENABLED: 1", "AWF_REFLECT_ENABLED must be set when harness-script forces AWF execution")
		assert.Contains(t, execStepContent, "awf --config", "execution step must use AWF when harness-script is present")
	})

	t.Run("env_vars_still_set", func(t *testing.T) {
		workflowData := &WorkflowData{Name: "test"}
		steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
		require.GreaterOrEqual(t, len(steps), 2)

		execStepContent := strings.Join(steps[len(steps)-1], "\n")
		assert.Contains(t, execStepContent, "GH_AW_PROMPT:", "GH_AW_PROMPT must be set for harness to read prompt file")
		assert.Contains(t, execStepContent, "RUNNER_TEMP:", "RUNNER_TEMP must be set")
	})

	t.Run("protected_session_capture_uses_runner_only_log", func(t *testing.T) {
		def := newHarnessEngineDefinition()
		def.Behaviors.Execution.ProtectedSessionCapture = true
		def.Behaviors.Execution.Env = map[string]string{"RUNNER_TEMP": "/tmp/overridden"}
		def.Behaviors.LogParser = `function parseLog() { return { markdown: "", logEntries: [] }; }`
		protectedEngine, err := NewBehaviorDefinedEngine(def)
		require.NoError(t, err)

		steps := protectedEngine.GetExecutionSteps(&WorkflowData{
			Name: "test",
			EngineConfig: &EngineConfig{Env: map[string]string{
				"RUNNER_TEMP": "/tmp/engine-overridden",
			}},
			SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{Env: map[string]string{
				"RUNNER_TEMP":                     "/tmp/agent-overridden",
				"GH_AW_PROTECTED_SESSION_CAPTURE": "0",
			}}},
		}, "/tmp/gh-aw/agent-stdio.log")
		require.Len(t, steps, 3)
		execStepContent := strings.Join(steps[len(steps)-1], "\n")

		assert.Contains(t, execStepContent, "GH_AW_PROTECTED_SESSION_CAPTURE: 1")
		assert.Contains(t, execStepContent, "RUNNER_TEMP: ${{ runner.temp }}")
		assert.NotContains(t, execStepContent, "/tmp/overridden")
		assert.NotContains(t, execStepContent, "/tmp/engine-overridden")
		assert.NotContains(t, execStepContent, "/tmp/agent-overridden")
		assert.Contains(t, execStepContent, `--capture-command-output "${RUNNER_TEMP}/gh-aw-protected-capture-${GH_AW_PROTECTED_CAPTURE_ID}"`)
		assert.Contains(t, execStepContent, "GH_AW_PROTECTED_CAPTURE_ID: "+protectedSessionCaptureID)
		assert.Contains(t, execStepContent, `exec "$GH_AW_NODE_EXEC"`)
		assert.NotContains(t, execStepContent, "tee ")
		assert.Contains(t, execStepContent, `printf "%s" "$gh_aw_exit_code" > /tmp/gh-aw/agent_execution_exit_code.txt`)
		assert.NotContains(t, execStepContent, "tee -a /tmp/gh-aw/agent-stdio.log")
		assert.Equal(t, protectedSessionCaptureLogPath, protectedEngine.GetLogFileForParsing())

		var parserStep strings.Builder
		parserStep.WriteString("      - name: Configure secret\n        env:\n          VALUE: ${{ secrets.CAPTURE_TEST_SECRET }}\n")
		NewCompiler().generateLogParsing(&parserStep, &WorkflowData{}, protectedEngine)
		assert.Contains(t, parserStep.String(), "GH_AW_AGENT_OUTPUT: "+protectedSessionCaptureLogPath)
		assert.Contains(t, parserStep.String(), "GH_AW_PROTECTED_SESSION_CAPTURE: 1")
		assert.Contains(t, parserStep.String(), "id: parse_agent_logs")
		NewCompiler().generateTrackedSecretRedactionStep(&parserStep, parserStep.String(), &WorkflowData{})
		parserStepYAML := parserStep.String()
		redactionStepStart := strings.Index(parserStepYAML, "      - name: Redact secrets in logs\n")
		require.NotEqual(t, -1, redactionStepStart)
		parserYAML := parserStepYAML[:redactionStepStart]
		redactionYAML := parserStepYAML[redactionStepStart:]
		assert.Contains(t, parserYAML, "GH_AW_SECRET_NAMES: 'CAPTURE_TEST_SECRET'")
		assert.Contains(t, parserYAML, "SECRET_CAPTURE_TEST_SECRET: ${{ secrets.CAPTURE_TEST_SECRET }}")
		assert.Contains(t, redactionYAML, "SECRET_CAPTURE_TEST_SECRET: ${{ secrets.CAPTURE_TEST_SECRET }}")
	})

	t.Run("default_session_capture_remains_shared", func(t *testing.T) {
		steps := engine.GetExecutionSteps(&WorkflowData{Name: "test"}, "/tmp/gh-aw/agent-stdio.log")
		require.Len(t, steps, 2)
		execStepContent := strings.Join(steps[1], "\n")

		assert.NotContains(t, execStepContent, "GH_AW_PROTECTED_SESSION_CAPTURE")
		assert.NotContains(t, execStepContent, "gh-aw-protected-capture")
		assert.Contains(t, execStepContent, "tee -a /tmp/gh-aw/agent-stdio.log")
	})

	t.Run("harness_filename", func(t *testing.T) {
		assert.Equal(t, "testharness_harness.cjs", engine.harnessScriptFilename())
	})

	t.Run("heredoc_delimiter_collision_skips_harness_write_step", func(t *testing.T) {
		// An engine whose harness-script contains the heredoc delimiter at line start must
		// not generate a harness write step (to avoid premature heredoc termination).
		collisionDef := &EngineDefinition{
			ID:          "collision",
			DisplayName: "Collision",
			Behaviors: &EngineBehaviorDefinition{
				Execution: &EngineExecutionDefinition{
					CommandName: "collision-cli",
					StepName:    "Execute",
				},
				HarnessScript: "// legit JS\n" + harnessScriptHeredocDelimiter + "\nconsole.log('hi');",
			},
		}
		eng, err := NewBehaviorDefinedEngine(collisionDef)
		require.NoError(t, err)
		harnessStep := eng.buildHarnessWriteStep()
		assert.Nil(t, harnessStep, "harness write step must be skipped when script contains the heredoc delimiter")
	})
}

func TestBehaviorDefinedEngineCustomCommandWithHarnessInstallsNode(t *testing.T) {
	engine, err := NewBehaviorDefinedEngine(newHarnessEngineDefinition())
	require.NoError(t, err)

	steps := engine.GetInstallationSteps(&WorkflowData{
		Name:         "test",
		EngineConfig: &EngineConfig{Command: "/custom/testharness"},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true},
		},
	})
	stepContent := strings.Join(flattenSteps(steps), "\n")

	assert.Contains(t, stepContent, "Setup Node.js")
	assert.Contains(t, stepContent, "Install AWF binary")
	assert.NotContains(t, stepContent, "Copy Copilot CLI to daemon-visible path")
}

func TestBehaviorDefinedEngineMCPCapability(t *testing.T) {
	t.Run("defaults to supported", func(t *testing.T) {
		engine, err := NewBehaviorDefinedEngine(newHarnessEngineDefinition())
		require.NoError(t, err)
		assert.True(t, engine.GetCapabilities().MCP)
	})

	t.Run("uses engine definition", func(t *testing.T) {
		def := newHarnessEngineDefinition()
		def.MCP = new(false)
		engine, err := NewBehaviorDefinedEngine(def)
		require.NoError(t, err)
		assert.False(t, engine.GetCapabilities().MCP)
	})
}

func TestBehaviorDefinedEngineHarnessValidatesAuthSecret(t *testing.T) {
	def := newHarnessEngineDefinition()
	def.Auth = []AuthBinding{{Role: "api-key", Secret: "TESTHARNESS_API_KEY"}}
	engine, err := NewBehaviorDefinedEngine(def)
	require.NoError(t, err)

	step := strings.Join(engine.GetSecretValidationStep(&WorkflowData{
		Tools: map[string]any{"github": map[string]any{}},
	}), "\n")
	assert.Contains(t, step, "Validate TESTHARNESS_API_KEY secret")
	assert.Contains(t, step, "TESTHARNESS_API_KEY: ${{ secrets.TESTHARNESS_API_KEY }}")
	assert.NotContains(t, step, "GITHUB_MCP_SERVER_TOKEN")
}

func TestBehaviorDefinedEngineHarnessPassesAuthSecretToAWF(t *testing.T) {
	def := newHarnessEngineDefinition()
	def.Auth = []AuthBinding{{Role: "api-key", Secret: "TESTHARNESS_API_KEY"}}
	engine, err := NewBehaviorDefinedEngine(def)
	require.NoError(t, err)

	steps := engine.GetExecutionSteps(&WorkflowData{Name: "test"}, "/tmp/test.log")
	require.Len(t, steps, 2)

	executionStep := strings.Join(steps[1], "\n")
	assert.Contains(t, executionStep, "TESTHARNESS_API_KEY: ${{ secrets.TESTHARNESS_API_KEY }}")
	assert.NotContains(t, executionStep, "--exclude-env TESTHARNESS_API_KEY")
}

// TestBehaviorDefinedEngineNoHarnessScript verifies that engines without harness-script
// continue to use the direct command execution path (inline prompt substitution).
func TestBehaviorDefinedEngineNoHarnessScript(t *testing.T) {
	def := &EngineDefinition{
		ID:          "noharness",
		DisplayName: "NoHarness",
		Behaviors: &EngineBehaviorDefinition{
			Execution: &EngineExecutionDefinition{
				CommandName: "noharness-cli",
				Args:        []string{"run"},
				StepName:    "Execute NoHarness CLI",
			},
		},
	}
	engine, err := NewBehaviorDefinedEngine(def)
	require.NoError(t, err)

	workflowData := &WorkflowData{Name: "test"}
	steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")

	// No config-file, no harness: only the execution step
	require.Len(t, steps, 1, "should generate only execution step when no harness-script and no config-file")

	execStepContent := strings.Join(steps[0], "\n")
	assert.Contains(t, execStepContent, "noharness-cli run", "should invoke the command directly")
	assert.Contains(t, execStepContent, `"$(cat /tmp/gh-aw/aw-prompts/prompt.txt)"`, "direct execution must include inline prompt substitution")
	assert.NotContains(t, execStepContent, "GH_AW_NODE_EXEC", "should not use node harness when harness-script is absent")
	assert.NotContains(t, execStepContent, "GHAW_HARNESS_SCRIPT_EOF", "no harness write step should be present")

	def.Behaviors.Execution.ProtectedSessionCapture = true
	def.Behaviors.LogParser = `function parseLog() { return { markdown: "", logEntries: [] }; }`
	protectedEngine, err := NewBehaviorDefinedEngine(def)
	require.NoError(t, err)
	protectedSteps := protectedEngine.GetExecutionSteps(workflowData, "/tmp/test.log")
	require.Len(t, protectedSteps, 2)
	protectedStepContent := strings.Join(protectedSteps[len(protectedSteps)-1], "\n")
	assert.Contains(t, protectedStepContent, "GH_AW_PROTECTED_SESSION_CAPTURE: 1")
	assert.Contains(t, protectedStepContent, "awf --config")
	assert.Contains(t, protectedStepContent, "gh-aw-protected-capture")
	assert.Contains(t, protectedStepContent, "exec noharness-cli run")
}

func TestBehaviorDefinedEngineProtectedSessionCaptureValidation(t *testing.T) {
	def := newHarnessEngineDefinition()
	def.Behaviors.Execution.ProtectedSessionCapture = true

	_, err := NewBehaviorDefinedEngine(def)
	require.ErrorContains(t, err, "protected-session-capture without a log-parser")

	def.Behaviors.LogParser = `function parseLog() { return { markdown: "", logEntries: [] }; }`
	engine, err := NewBehaviorDefinedEngine(def)
	require.NoError(t, err)
	require.NoError(t, validateProtectedSessionCaptureSandbox(engine, nil))
	require.ErrorContains(
		t,
		validateProtectedSessionCaptureSandbox(engine, &WorkflowData{SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{Disabled: true}}}),
		"protected-session-capture requires sandbox.agent to be enabled",
	)

	def.Behaviors.LogParser = "function parseLog() {}\n" + logParserHeredocDelimiter + "\n"
	_, err = NewBehaviorDefinedEngine(def)
	require.ErrorContains(t, err, "protected-session-capture without a usable log-parser")
}

func TestProtectedSessionCaptureRequiresNativeAWF(t *testing.T) {
	for _, test := range []struct {
		name    string
		version string
		runtime AgentRuntime
		args    []string
		wantErr string
	}{
		{name: "default"},
		{name: "first_supported", version: "v0.28.67"},
		{name: "old", version: "v0.28.66", wantErr: "requires AWF v0.28.67"},
		{name: "cloud_hypervisor", runtime: AgentRuntimeCloudHypervisor, wantErr: "does not support microVM"},
		{name: "nvx", runtime: AgentRuntimeNVX, wantErr: "does not support microVM"},
		{name: "legacy_docker", runtime: AgentRuntimeDockerSudoIptables},
		{name: "tty_argument", args: []string{"--tty"}, wantErr: "does not support sandbox.agent.args flag --tty"},
		{name: "tty_equals_argument", args: []string{"--tty=true"}, wantErr: "does not support sandbox.agent.args flag --tty"},
		{name: "dind_argument", args: []string{"--enable-dind"}, wantErr: "does not support sandbox.agent.args flag --enable-dind"},
		{name: "runtime_argument", args: []string{"--container-runtime", "nvx"}, wantErr: "does not support sandbox.agent.args flag --container-runtime"},
		{name: "runtime_equals_argument", args: []string{"--container-runtime=nvx"}, wantErr: "does not support sandbox.agent.args flag --container-runtime"},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := &WorkflowData{SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{Version: test.version, Runtime: test.runtime, Args: test.args}}}
			err := validateNativeSessionCapture(data)
			if test.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, test.wantErr)
				command := BuildAWFCommand(AWFCommandConfig{ProtectLogCapture: true, WorkflowData: data})
				assert.NotContains(t, command, "tee ")
				assert.Contains(t, command, "exit 1")
			}
		})
	}
}

func TestBehaviorDefinedEngineConfigFileDisablesSC2016(t *testing.T) {
	def := &EngineDefinition{
		ID:          "config",
		DisplayName: "Config",
		Behaviors: &EngineBehaviorDefinition{
			ConfigFile: &EngineConfigFileDefinition{
				Path:          ".config.json",
				StepName:      "Write Config",
				Content:       `{"$schema":"https://example.com/schema.json"}`,
				MergeStrategy: behaviorConfigMergeJSON,
			},
		},
	}
	engine, err := NewBehaviorDefinedEngine(def)
	require.NoError(t, err)

	step := strings.Join(engine.buildConfigFileStep(), "\n")
	assert.Contains(t, step, "# shellcheck disable=SC2016\n          BASE_CONFIG=")

	def.Behaviors.ConfigFile.Content = `{"option":true}`
	step = strings.Join(engine.buildConfigFileStep(), "\n")
	assert.NotContains(t, step, "# shellcheck disable=SC2016")
}

// TestBehaviorDefinedEngineRenderMCPConfig verifies that the MCP gateway startup command
// is always emitted, even for engines that declare no behaviors.mcp.config-path (e.g.
// engines with `mcp: false`). Skipping it would leave the gateway container unstarted
// while AWF still attempts to attach it to the internal network.
func TestBehaviorDefinedEngineRenderMCPConfig(t *testing.T) {
	workflowData := &WorkflowData{
		Name:  "test",
		Tools: map[string]any{"safe-outputs": map[string]any{}},
	}

	t.Run("uses default config path when mcp behavior is absent", func(t *testing.T) {
		engine, err := NewBehaviorDefinedEngine(newHarnessEngineDefinition())
		require.NoError(t, err)

		var sb strings.Builder
		require.NoError(t, engine.RenderMCPConfig(&sb, workflowData.Tools, []string{"safe-outputs"}, workflowData))
		assert.Contains(t, sb.String(), "start_mcp_gateway.cjs", "gateway startup command must be emitted")
	})

	t.Run("uses engine config path when declared", func(t *testing.T) {
		def := newHarnessEngineDefinition()
		def.Behaviors.MCP = &EngineMCPDefinition{ConfigPath: ".custom/mcp.json"}
		engine, err := NewBehaviorDefinedEngine(def)
		require.NoError(t, err)

		var sb strings.Builder
		require.NoError(t, engine.RenderMCPConfig(&sb, workflowData.Tools, []string{"safe-outputs"}, workflowData))
		assert.Contains(t, sb.String(), "start_mcp_gateway.cjs", "gateway startup command must be emitted")
	})
}

// TestBehaviorDefinedEngineVersionEnvInjection verifies that when engine.version is set,
// GH_AW_ENGINE_VERSION is injected into the execution step environment.
func TestBehaviorDefinedEngineVersionEnvInjection(t *testing.T) {
	def := &EngineDefinition{
		ID:          "goose",
		DisplayName: "Goose",
		Behaviors: &EngineBehaviorDefinition{
			Execution: &EngineExecutionDefinition{
				CommandName: "goose",
				Args:        []string{"run"},
				StepName:    "Execute Goose",
			},
		},
	}
	engine, err := NewBehaviorDefinedEngine(def)
	require.NoError(t, err)

	t.Run("version injected when set", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name:         "test",
			EngineConfig: &EngineConfig{ID: "goose", Version: "1.2.3"},
		}
		steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
		require.NotEmpty(t, steps, "execution steps must not be empty")
		execStepContent := strings.Join(steps[len(steps)-1], "\n")
		assert.Contains(t, execStepContent, "GH_AW_ENGINE_VERSION: 1.2.3", "execution env should include engine version")
	})

	t.Run("version expression injected when set", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name:         "test",
			EngineConfig: &EngineConfig{ID: "goose", Version: "${{ inputs.engine-version }}"},
		}
		steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
		require.NotEmpty(t, steps, "execution steps must not be empty")
		execStepContent := strings.Join(steps[len(steps)-1], "\n")
		assert.Contains(t, execStepContent, "GH_AW_ENGINE_VERSION: ${{ inputs.engine-version }}", "execution env should include expression version")
	})

	t.Run("version not set when absent", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name:         "test",
			EngineConfig: &EngineConfig{ID: "goose"},
		}
		steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
		require.NotEmpty(t, steps, "execution steps must not be empty")
		execStepContent := strings.Join(steps[len(steps)-1], "\n")
		assert.NotContains(t, execStepContent, "GH_AW_ENGINE_VERSION", "execution env should not include version when not configured")
	})
}
