//go:build !integration

package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func decodeCodexBootstrap(t *testing.T, output string) map[string]any {
	t.Helper()
	for line := range strings.SplitSeq(output, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "GH_AW_CODEX_CONFIG_JSON:") {
			continue
		}
		var env map[string]string
		require.NoError(t, yaml.Unmarshal([]byte(strings.TrimSpace(line)), &env))
		cmd := exec.Command("node", "-e", `
const {mergeConfig, serializeConfig} = require("./actions/setup/js/codex_config.cjs");
const cfg = JSON.parse(require("fs").readFileSync(0,"utf8"));
let merged = mergeConfig(cfg.defaults, cfg.overrides);
if (cfg.disablePlugins) merged = mergeConfig(merged, {features:{plugins:false}});
process.stdout.write(serializeConfig(merged));`)
		cmd.Dir = filepath.Join("..", "..")
		cmd.Stdin = strings.NewReader(env["GH_AW_CODEX_CONFIG_JSON"])
		content, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", content)
		var config map[string]any
		require.NoError(t, toml.Unmarshal(content, &config))
		return config
	}
	t.Fatal("Codex compiled configuration environment variable was not found")
	return nil
}

func renderCodexMCPConfigForTest(t *testing.T, engine *CodexEngine, output *strings.Builder, tools map[string]any, mcpTools []string, data *WorkflowData) error {
	t.Helper()
	step, err := engine.renderConfigurationStep(data, mcpTools, false)
	if err != nil {
		return err
	}
	for _, line := range step {
		output.WriteString(line)
		output.WriteByte('\n')
	}
	return engine.RenderMCPConfig(output, tools, mcpTools, data)
}

func TestCodexNativeConfigStructuralOverrides(t *testing.T) {
	engine := NewCodexEngine()
	data := &WorkflowData{
		Name: "native-config",
		EngineConfig: &EngineConfig{
			ID: "codex",
			Config: "model_reasoning_effort = \"high\"\n[features]\nplugins = false\nshell_tool = false\n" +
				"[mcp_servers.github]\ntool_timeout_sec = 180\n",
		},
		ToolsTimeout: "120",
	}
	config, err := engine.buildNativeConfig(data, []string{"github"})
	require.NoError(t, err)
	payload, err := json.Marshal(config)
	require.NoError(t, err)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	require.NoError(t, os.WriteFile(configPath, payload, 0o600))
	cmd := exec.Command("node", "-e", `
const {buildConfig, serializeConfig} = require("./actions/setup/js/codex_config.cjs");
process.stdout.write(serializeConfig(buildConfig({github: {headers: {Authorization: 'Bearer "quoted"\\token'}}}, "http://gateway:80")));
`)
	cmd.Dir = filepath.Join("..", "..")
	cmd.Env = append(os.Environ(), "GH_AW_CODEX_CONFIG="+configPath, "GH_AW_TOOL_TIMEOUT=150", "GH_AW_STARTUP_TIMEOUT=240")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	var parsed map[string]any
	require.NoError(t, toml.Unmarshal(output, &parsed))
	assert.Equal(t, "high", parsed["model_reasoning_effort"])
	features := parsed["features"].(map[string]any)
	assert.Equal(t, false, features["plugins"])
	assert.Equal(t, false, features["shell_tool"])
	assert.Equal(t, "none", parsed["otel"].(map[string]any)["metrics_exporter"])
	server := parsed["mcp_servers"].(map[string]any)["github"].(map[string]any)
	assert.Equal(t, int64(180), server["tool_timeout_sec"])
	assert.Equal(t, int64(240), server["startup_timeout_sec"])
	assert.Equal(t, "http://gateway:80/mcp/github", server["url"])
	headers := server["http_headers"].(map[string]any)
	assert.Equal(t, "native-config", headers["User-Agent"])
	assert.Equal(t, "Bearer \"quoted\"\\token", headers["Authorization"])
}

func TestCodexNativeConfigShellEnvironment(t *testing.T) {
	data := &WorkflowData{
		EngineConfig: &EngineConfig{Env: map[string]string{
			"CUSTOM_REGION": "east",
			"CUSTOM_TOKEN":  "${{ secrets.PRIVATE_TOKEN }}",
			"APP_TOKEN":     "${{ needs.auth.outputs.token }}",
			"OMITTED":       "excluded",
		}},
		ExcludedEnv: []string{"OMITTED"},
	}

	config, err := NewCodexEngine().buildNativeConfig(data, nil)
	require.NoError(t, err)
	policy := config.Defaults["shell_environment_policy"].(map[string]any)
	assert.Equal(t, "all", policy["inherit"])
	assert.Equal(t, false, policy["ignore_default_excludes"])
	names := policy["include_only"].([]string)
	for _, name := range []string{"HOME", "PATH", "PLAYWRIGHT_BROWSERS_PATH", "CUSTOM_REGION", "GIT_AUTHOR_NAME"} {
		assert.Contains(t, names, name)
	}
	for _, name := range []string{"^PATH$", "CODEX_API_KEY", "OPENAI_API_KEY", "CUSTOM_TOKEN", "APP_TOKEN", "OMITTED"} {
		assert.NotContains(t, names, name)
	}
}

func TestCodexNativeConfigExcludesEnvironmentWithoutOverrides(t *testing.T) {
	for _, env := range []map[string]string{nil, {}, {"CUSTOM_REGION": "east", "CUSTOM_MODE": "test", "PATH": "/custom/bin"}} {
		t.Run(fmt.Sprintf("overrides-%d", len(env)), func(t *testing.T) {
			names := codexShellEnvironmentVars(&WorkflowData{
				EngineConfig: &EngineConfig{Env: env},
				ExcludedEnv:  []string{"PATH", "HOME", "CUSTOM_REGION"},
			})
			assert.NotContains(t, names, "PATH")
			assert.NotContains(t, names, "HOME")
			assert.NotContains(t, names, "CUSTOM_REGION")
			assert.Contains(t, names, "SHELL")
		})
	}
}

func TestCodexDetectionConfigKeepsInferenceSettingsWithoutAgentMCP(t *testing.T) {
	config, err := NewCodexEngine().buildNativeConfig(&WorkflowData{
		IsDetectionRun: true,
		EngineConfig: &EngineConfig{
			Config: "model_reasoning_effort = \"high\"\n[mcp_servers.github]\ntool_timeout_sec = 180\n",
		},
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, "high", config.Overrides["model_reasoning_effort"])
	assert.NotContains(t, config.Overrides, "mcp_servers")
}

func TestCodexModelArgumentsDoNotDuplicateCompilerModel(t *testing.T) {
	for _, args := range [][]string{{"-m", "gpt-5.4"}, {"--model=gpt-5.4"}, {"--model", "gpt-5.4"}} {
		data := &WorkflowData{EngineConfig: &EngineConfig{Args: args}}
		command := NewCodexEngine().buildCodexCommand(data, "codex", "codex_harness.cjs", false, "MODEL", "")
		assert.NotContains(t, command, `${MODEL:+ --model "$MODEL"}`)
		assert.Contains(t, command, strings.Join(args, " "))
	}
}

func TestCodexExplicitProviderMarker(t *testing.T) {
	for _, provider := range []LLMProvider{"", LLMProviderOpenAI, LLMProviderGitHub} {
		env := NewCodexEngine().buildCodexExecutionEnv(&WorkflowData{
			EngineConfig: &EngineConfig{ID: "codex", LLMProvider: provider},
		}, true, false, "MODEL")
		if provider == "" {
			assert.NotContains(t, env, "GH_AW_LLM_PROVIDER_EXPLICIT")
		} else {
			assert.Equal(t, "1", env["GH_AW_LLM_PROVIDER_EXPLICIT"])
		}
	}
}

func TestCodexNativeConfigValidation(t *testing.T) {
	for _, config := range []string{
		"[features]\nplugins = false\nplugins = true",
		"model = \"${{ inputs.model }}\"",
		"threshold = inf",
		"threshold = 9007199254740992",
		"date = 2026-10-02",
	} {
		t.Run(config, func(t *testing.T) {
			_, err := parseCodexConfig(config)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "engine.config")
		})
	}
	for _, config := range []string{
		"[mcp_servers.unknown]\ntool_timeout_sec = 30",
		"[mcp_servers.github]\nurl = \"https://example.com\"",
		"model_provider = \"custom\"",
		"[model_providers.openai-proxy]\nbase_url = \"https://example.com\"",
	} {
		t.Run(config, func(t *testing.T) {
			_, err := NewCodexEngine().buildNativeConfig(&WorkflowData{
				EngineConfig:       &EngineConfig{Config: config},
				NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}},
			}, []string{"github"})
			require.Error(t, err)
		})
	}
}

func TestCodexCompatibilityValidation(t *testing.T) {
	compiler := NewCompiler()
	for _, test := range []struct {
		name     string
		provider LLMProvider
		model    string
		firewall bool
		wantErr  string
	}{
		{"OpenAI without sandbox", LLMProviderOpenAI, "gpt-5.4", false, ""},
		{"GitHub with sandbox", LLMProviderGitHub, "gpt-5.3-codex", true, ""},
		{"GitHub requires sandbox", "", "copilot/gpt-5.3-codex", false, "requires the AWF sandbox"},
		{"Anthropic requires a bridge", LLMProviderAnthropic, "claude-sonnet", true, "Responses-compatible"},
		{"Runtime model keeps default provider", "", "${{ inputs.model }}", true, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := compiler.validateCodexCompatibility(&WorkflowData{
				EngineConfig:       &EngineConfig{ID: "codex", LLMProvider: test.provider},
				Model:              test.model,
				NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: test.firewall}},
			})
			if test.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, test.wantErr)
			}
		})
	}
}

func TestCodexPluginsRegisterAfterConfiguration(t *testing.T) {
	compiler := NewCompiler(WithVersion("dev"))
	data := &WorkflowData{
		Name:         "plugins-order",
		AI:           "codex",
		EngineConfig: &EngineConfig{ID: "codex"},
		Plugins:      []string{"octo-org/agent-plugin@" + testPluginSHA},
	}
	var generated strings.Builder
	_, err := compiler.generateEngineInstallAndPreAgentSteps(&generated, data, false)
	require.NoError(t, err)
	output := generated.String()
	checkout := strings.Index(output, "name: Checkout agent plugin")
	config := strings.Index(output, "name: Configure Codex")
	registration := strings.Index(output, "name: Install agent plugin")
	require.GreaterOrEqual(t, checkout, 0)
	require.GreaterOrEqual(t, config, 0)
	require.GreaterOrEqual(t, registration, 0)
	assert.Less(t, checkout, config)
	assert.Less(t, config, registration)
	assert.Contains(t, output[registration:], "CODEX_HOME: /tmp/gh-aw/mcp-config")
}

func TestCodexAPITargetUsesEffectiveProvider(t *testing.T) {
	for _, provider := range []LLMProvider{LLMProviderOpenAI, LLMProviderGitHub} {
		t.Run(string(provider), func(t *testing.T) {
			data := &WorkflowData{EngineConfig: &EngineConfig{
				ID: "codex", LLMProvider: provider, APITarget: "llm.example.test",
			}}
			serialized, err := BuildAWFConfigJSON(AWFCommandConfig{EngineName: "codex", WorkflowData: data})
			require.NoError(t, err)
			var config AWFConfigFile
			require.NoError(t, json.Unmarshal([]byte(serialized), &config))
			target := "openai"
			if provider == LLMProviderGitHub {
				target = "copilot"
			}
			require.Contains(t, config.APIProxy.Targets, target)
			assert.Equal(t, "llm.example.test", config.APIProxy.Targets[target].Host)
			assert.Len(t, config.APIProxy.Targets, 1)
		})
	}
}
