//go:build !integration && !windows

package workflow

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"mvdan.cc/sh/v3/syntax"
)

func TestBehaviorDefinedEngine_CopilotClientKey(t *testing.T) {
	for _, harness := range []bool{false, true} {
		for _, disabled := range []bool{false, true} {
			for _, model := range []string{"copilot/gpt-5", "anthropic/claude-sonnet-4.6", "openai/gpt-5"} {
				t.Run(model+"/harness="+strconv.FormatBool(harness)+"/disabled="+strconv.FormatBool(disabled), func(t *testing.T) {
					def := newHarnessEngineDefinition()
					def.Behaviors.SecretStrategy = behaviorSecretStrategyUniversalLLMConsumer
					def.Behaviors.Execution.ProviderEnvMode = behaviorProviderEnvModeUniversalLLMConsumer
					if !harness {
						def.Behaviors.HarnessScript = ""
					}
					engine, err := NewBehaviorDefinedEngine(def)
					require.NoError(t, err)
					data := &WorkflowData{
						Name: "test", Model: model, EngineConfig: &EngineConfig{},
						SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{ID: "awf", Disabled: disabled}},
					}
					firewallEnabled := engine.behaviorDefinedFirewallEnabled(data)
					env := engine.buildBehaviorDefinedExecutionEnv(def.Behaviors.Execution, data, firewallEnabled)
					command := engine.buildBehaviorDefinedExecutionCommand(def.Behaviors.Execution, data, "/tmp/test.log", "true", firewallEnabled)
					keyExport := `export OPENAI_API_KEY="$COPILOT_DUMMY_BYOK"`
					if model == "copilot/gpt-5" {
						assert.NotContains(t, env, "OPENAI_API_KEY")
					}
					if model == "copilot/gpt-5" && !disabled {
						assert.Equal(t, constants.CopilotBYOKDummyAPIKey, env[constants.CopilotBYOKDummyAPIKeyEnvVar])
						assert.Contains(t, command, "-- /bin/bash -c 'set +o histexpand; "+keyExport)
						step := strings.Join(engine.buildBehaviorDefinedExecutionStep(def.Behaviors.Execution, data, command, env), "\n")
						assert.Contains(t, step, "COPILOT_DUMMY_BYOK: "+constants.CopilotBYOKDummyAPIKey)
						assert.NotContains(t, step, "OPENAI_API_KEY:")
					} else {
						assert.NotContains(t, env, constants.CopilotBYOKDummyAPIKeyEnvVar)
						assert.NotContains(t, command, keyExport)
					}
				})
			}
		}
	}
}

func TestBehaviorDefinedEngine_CopilotAWFStartupEnvironment(t *testing.T) {
	for _, useCopilotRequests := range []bool{false, true} {
		t.Run(strconv.FormatBool(useCopilotRequests), func(t *testing.T) {
			def := newHarnessEngineDefinition()
			def.Behaviors.SecretStrategy = behaviorSecretStrategyUniversalLLMConsumer
			def.Behaviors.Execution.ProviderEnvMode = behaviorProviderEnvModeUniversalLLMConsumer
			engine, err := NewBehaviorDefinedEngine(def)
			require.NoError(t, err)
			dir := t.TempDir()
			data := &WorkflowData{
				Name: "test", Model: "copilot/gpt-5", EngineConfig: &EngineConfig{},
				SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{ID: "awf", Command: "fake_awf"}},
			}
			if useCopilotRequests {
				data.Permissions = "copilot-requests: write"
			}
			env := engine.buildBehaviorDefinedExecutionEnv(def.Behaviors.Execution, data, true)
			require.NotContains(t, env, "OPENAI_API_KEY")
			childCommand := `test "$OPENAI_API_KEY" = "$COPILOT_DUMMY_BYOK" && test -z "${COPILOT_GITHUB_TOKEN:-}" && printf 'client-placeholder-ok\n'`
			command := engine.buildFirewallCommand(def.Behaviors.Execution, data, filepath.Join(dir, "agent.log"), childCommand)
			tree, err := syntax.NewParser().Parse(strings.NewReader(command), "")
			require.NoError(t, err)
			var invocation bytes.Buffer
			syntax.Walk(tree, func(node syntax.Node) bool {
				call, ok := node.(*syntax.CallExpr)
				if ok && len(call.Args) > 0 && call.Args[0].Lit() == "fake_awf" {
					require.Empty(t, invocation.String(), "exactly one AWF invocation")
					require.NoError(t, syntax.NewPrinter().Print(&invocation, call))
				}
				return true
			})
			require.NotEmpty(t, invocation.String())
			script := `
fake_awf() {
  test -z "${OPENAI_API_KEY+x}" || return 10
  test "$COPILOT_GITHUB_TOKEN" = "test-copilot-credential" || return 11
  while [ "$#" -gt 0 ] && [ "$1" != "--" ]; do shift; done
  test "$#" -gt 0 || return 12
  shift
  unset COPILOT_GITHUB_TOKEN
  "$@"
}
` + invocation.String()
			cmd := exec.Command("/bin/bash", "-c", script)
			cmd.Env = []string{
				"PATH=" + os.Getenv("PATH"),
				"GITHUB_WORKSPACE=" + dir,
				"RUNNER_TEMP=" + dir,
				"RUNNER_TOOL_CACHE=" + dir,
				"COPILOT_GITHUB_TOKEN=test-copilot-credential",
				constants.CopilotBYOKDummyAPIKeyEnvVar + "=" + env[constants.CopilotBYOKDummyAPIKeyEnvVar],
			}
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", output)
			assert.Equal(t, "client-placeholder-ok\n", string(output))
		})
	}
}

func TestBehaviorDefinedEngine_ProtectedCopilotClientKey(t *testing.T) {
	def := newHarnessEngineDefinition()
	def.Behaviors.SecretStrategy = behaviorSecretStrategyUniversalLLMConsumer
	def.Behaviors.Execution.ProviderEnvMode = behaviorProviderEnvModeUniversalLLMConsumer
	def.Behaviors.Execution.ProtectedSessionCapture = true
	def.Behaviors.LogParser = `function parseLog() { return { markdown: "", logEntries: [] }; }`
	engine, err := NewBehaviorDefinedEngine(def)
	require.NoError(t, err)
	steps := engine.GetExecutionSteps(&WorkflowData{
		Name: "test", Model: "copilot/gpt-5", EngineConfig: &EngineConfig{},
	}, "/tmp/gh-aw/agent-stdio.log")
	step := strings.Join(steps[len(steps)-1], "\n")
	assert.Contains(t, step, `-- /bin/bash -c 'set +o histexpand; export OPENAI_API_KEY="$COPILOT_DUMMY_BYOK"`)
	assert.Contains(t, step, `exec "$GH_AW_NODE_EXEC"`)
	assert.Contains(t, step, "--capture-command-output")
	assert.NotContains(t, step, "OPENAI_API_KEY:")
}

func TestBehaviorDefinedEngine_NonUniversalProviderEnv(t *testing.T) {
	def := newHarnessEngineDefinition()
	engine, err := NewBehaviorDefinedEngine(def)
	require.NoError(t, err)
	data := &WorkflowData{Name: "test", Model: "copilot/gpt-5", EngineConfig: &EngineConfig{}}
	env := engine.buildBehaviorDefinedExecutionEnv(def.Behaviors.Execution, data, true)
	command := engine.buildFirewallCommand(def.Behaviors.Execution, data, "/tmp/test.log", "true")
	assert.NotContains(t, env, constants.CopilotBYOKDummyAPIKeyEnvVar)
	assert.NotContains(t, command, "export OPENAI_API_KEY")
}

func TestBehaviorDefinedEngine_CompiledCopilotCredentials(t *testing.T) {
	for _, permissions := range []string{"contents: read", "contents: read\n  copilot-requests: write"} {
		t.Run(permissions, func(t *testing.T) {
			def := newHarnessEngineDefinition()
			def.Behaviors.SecretStrategy = behaviorSecretStrategyUniversalLLMConsumer
			def.Behaviors.Execution.ProviderEnvMode = behaviorProviderEnvModeUniversalLLMConsumer
			source, err := yaml.Marshal(map[string]any{"engine": def})
			require.NoError(t, err)
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "engine.md"), []byte("---\n"+string(source)+"---\n"), 0o600))
			path := filepath.Join(dir, "test.md")
			workflow := "---\non: workflow_dispatch\npermissions:\n  " + permissions +
				"\nengine:\n  id: testharness\nmodel: copilot/gpt-5\nimports: [engine.md]\n---\nTest.\n"
			require.NoError(t, os.WriteFile(path, []byte(workflow), 0o600))
			require.NoError(t, NewCompiler().CompileWorkflow(path))
			lock, err := os.ReadFile(filepath.Join(dir, "test.lock.yml"))
			require.NoError(t, err)
			assert.NotContains(t, string(lock), "OPENAI_API_KEY:")
			assert.Contains(t, string(lock), `export OPENAI_API_KEY="$COPILOT_DUMMY_BYOK"`)
			assert.Contains(t, string(lock), "COPILOT_DUMMY_BYOK: "+constants.CopilotBYOKDummyAPIKey)
			expectedToken := "${{ secrets.COPILOT_GITHUB_TOKEN }}"
			if strings.Contains(permissions, "copilot-requests") {
				expectedToken = "${{ github.token }}"
			}
			assert.Contains(t, string(lock), "COPILOT_GITHUB_TOKEN: "+expectedToken)
		})
	}
}
