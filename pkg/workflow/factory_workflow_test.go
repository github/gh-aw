//go:build !integration

package workflow

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

var factoryExcludedEnv = []string{
	"FACTORY_API_KEY", "SECRET_FACTORY_API_KEY", "COPILOT_GITHUB_TOKEN",
	"OPENAI_API_KEY", "CODEX_API_KEY", "ANTHROPIC_API_KEY",
	"GITHUB_TOKEN", "GH_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "GH_ENTERPRISE_TOKEN",
	"GH_AW_GITHUB_TOKEN", "GH_AW_AGENT_TOKEN",
}

func loadFactorySample(t *testing.T) *EngineDefinition {
	t.Helper()
	content, err := os.ReadFile("../../.github/workflows/shared/factory.md")
	require.NoError(t, err)
	parts := strings.SplitN(string(content), "---", 3)
	require.Len(t, parts, 3)
	var frontmatter struct {
		Engine      EngineDefinition `yaml:"engine"`
		ExcludedEnv []string         `yaml:"excluded-env"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(parts[1]), &frontmatter))
	require.NotNil(t, frontmatter.Engine.Behaviors)
	assert.ElementsMatch(t, factoryExcludedEnv, frontmatter.ExcludedEnv)
	return &frontmatter.Engine
}

func TestFactorySampleExecution(t *testing.T) {
	def := loadFactorySample(t)
	engine, err := NewBehaviorDefinedEngine(def)
	require.NoError(t, err)
	assert.Equal(t, "factory", engine.GetID())
	assert.True(t, def.Experimental)
	assert.Empty(t, def.Auth, "provider credentials must be owned by AWF, not passed through engine auth bindings")
	assert.Equal(t, "universal-llm-consumer", def.Behaviors.SecretStrategy)
	assert.Empty(t, engine.GetLogParserScriptId(), "text output must not imply structured session support")
	assert.Equal(t, []string{"exec", "--auto", "high", "--output-format", "text"}, def.Behaviors.Execution.Args)
	assert.False(t, def.Behaviors.Installation.PostInstallScripts)

	data := &WorkflowData{
		Name:         "factory-test",
		Model:        "copilot/gpt-5.2",
		EngineConfig: &EngineConfig{ID: "factory", Version: def.Version},
		ExcludedEnv:  factoryExcludedEnv,
	}
	var installation strings.Builder
	for _, step := range engine.GetInstallationSteps(data) {
		installation.WriteString(strings.Join(step, "\n"))
	}
	assert.Contains(t, installation.String(), "@factory/cli@"+def.Version)
	assert.Contains(t, installation.String(), "--ignore-scripts")
	assert.Contains(t, installation.String(), "droid --version")
	assert.Contains(t, strings.Join(engine.GetSecretValidationStep(data), "\n"), "COPILOT_GITHUB_TOKEN")

	var execution strings.Builder
	for _, step := range engine.GetExecutionSteps(data, "/tmp/factory.log") {
		execution.WriteString(strings.Join(step, "\n"))
	}
	assert.Contains(t, execution.String(), "factory_harness.cjs")
	assert.Contains(t, execution.String(), "FACTORY_MODEL: copilot/gpt-5.2")
	assert.NotContains(t, execution.String(), "FACTORY_API_KEY: ${{ secrets.FACTORY_API_KEY }}")
	for _, name := range data.ExcludedEnv {
		assert.Contains(t, execution.String(), "--exclude-env "+name)
	}
	assert.Contains(t, execution.String(), "GH_AW_ENGINE_VERSION: "+def.Version)
	assert.Equal(t, "copilot", def.DetectionEngine)
	assert.Empty(t, def.Behaviors.Network.Defaults, "the engine must not add unrelated network defaults")
	assert.NotContains(t, def.Behaviors.Network.ProviderDomains, "factory")
}

func TestFactorySmokeContract(t *testing.T) {
	source, err := os.ReadFile("../../.github/workflows/smoke-factory.md")
	require.NoError(t, err)
	assert.Contains(t, string(source), "shared/factory.md")
	assert.Contains(t, string(source), "Use native GitHub MCP tools")
	assert.Contains(t, string(source), "droid --version")
	assert.Contains(t, string(source), "make build")
	assert.NotContains(t, string(source), "schedule:")

	lock, err := os.ReadFile("../../.github/workflows/smoke-factory.lock.yml")
	require.NoError(t, err)
	for _, expected := range []string{
		"npm install --ignore-scripts -g @factory/cli@" + loadFactorySample(t).Version,
		"GH_AW_ENGINE_VERSION: " + loadFactorySample(t).Version,
		"FACTORY_MODEL: copilot/gpt-5.2",
		"GH_AW_MCP_CONFIG: .factory/mcp.json",
		"factory_mcp_config_adapter.cjs",
		"proxy.golang.org",
		`"GITHUB_READ_ONLY": "1"`,
	} {
		assert.Contains(t, string(lock), expected)
	}
	assert.NotContains(t, string(lock), "--enable-gh-proxy")
	assert.NotContains(t, string(lock), "${{ secrets.FACTORY_API_KEY }}")
	assert.NotContains(t, string(lock), "*.factory.ai")
	for _, name := range factoryExcludedEnv {
		assert.Contains(t, string(lock), "--exclude-env "+name)
	}
}

func TestFactoryHarness(t *testing.T) {
	def := loadFactorySample(t)
	harness := filepath.Join(t.TempDir(), "factory_harness.cjs")
	require.NoError(t, os.WriteFile(harness, []byte(def.Behaviors.HarnessScript), 0o600))
	excludedEnvJSON, err := json.Marshal(factoryExcludedEnv)
	require.NoError(t, err)
	for _, scenario := range []string{
		"success", "copilot-chat-completions", "copilot-responses", "anthropic", "openai", "codex", "whitespace-model", "working-directory", "missing-model", "wrong-provider",
		"empty-model", "missing-prompt", "empty-prompt", "unreadable-prompt",
		"execution-failure", "signal", "spawn-error", "no-awf", "reflect-failure", "missing-endpoint", "missing-models-url", "wrong-endpoint",
	} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "node", "-e", factoryHarnessTestDriver, harness, scenario, string(excludedEnvJSON))
			cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", output)
		})
	}
}

const factoryHarnessTestDriver = `
(async () => {
const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");
const awfReflect = require("../../actions/setup/js/awf_reflect.cjs");
const [scriptPath, scenario, excludedEnvJSON] = process.argv.slice(1);
const excludedEnv = JSON.parse(excludedEnvJSON);
const calls = [];
const logs = [];
let settings;
let cleaned = false;
const prompt = "--model injected\n$(touch /not-executed)";
const env = {FACTORY_MODEL: "copilot/gpt-5.2", GH_AW_LLM_PROVIDER: "github", AWF_REFLECT_ENABLED: "1",
  GH_AW_PROMPT: "/prompt with spaces.md", GITHUB_WORKSPACE: "/workspace", PATH: "/fixture/bin"};
for (const name of excludedEnv) env[name] = "sentinel-" + name;
if (["anthropic", "openai", "codex"].includes(scenario)) {
  env.GH_AW_LLM_PROVIDER = scenario === "codex" ? "openai" : scenario;
  env.FACTORY_MODEL = scenario + "/fixture-model";
}
if (scenario === "copilot-chat-completions") env.FACTORY_MODEL = "copilot/claude-haiku-5.5";
if (scenario === "copilot-responses") env.FACTORY_MODEL = "copilot/gpt-5.3-codex";
if (scenario === "working-directory") env.GH_AW_ENGINE_CWD = "/workspace/subdir";
if (scenario === "missing-model") delete env.FACTORY_MODEL;
if (scenario === "wrong-provider") env.GH_AW_LLM_PROVIDER = "factory";
if (scenario === "empty-model") env.FACTORY_MODEL = "copilot/ ";
if (scenario === "whitespace-model") env.FACTORY_MODEL = "copilot/ \t gpt-5.2 \n ";
if (scenario === "missing-prompt") delete env.GH_AW_PROMPT;
if (scenario === "no-awf") delete env.AWF_REFLECT_ENABLED;
const args = ["exec", "--auto", "high", "--output-format", "text"];
const mockProcess = {argv: ["node", scriptPath, "droid", ...args], env,
  stderr: {write: text => logs.push(text)}};
await vm.runInNewContext(fs.readFileSync(scriptPath, "utf8"), {
  require: name => {
    if (name === "fs") return {
      readFileSync: path => {
        assert.equal(path, "/prompt with spaces.md");
        if (scenario === "unreadable-prompt") throw new Error("prompt read failed");
        return scenario === "empty-prompt" ? " \n" : prompt;
      },
      mkdirSync: path => assert.equal(path, "/tmp/gh-aw/agent"),
      mkdtempSync: prefix => {
        assert.equal(prefix, "/tmp/gh-aw/agent/factory-");
        return "/tmp/gh-aw/agent/factory-fixture";
      },
      writeFileSync: (path, content, options) => {
        assert.equal(path, "/tmp/gh-aw/agent/factory-fixture/settings.json");
        assert.equal(options.mode, 0o600);
        settings = JSON.parse(content);
      },
      rmSync: (path, options) => {
        assert.equal(path, "/tmp/gh-aw/agent/factory-fixture");
        assert.equal(options.recursive, true);
        cleaned = true;
      },
    };
    if (name === "./awf_reflect.cjs") return {
      ...awfReflect,
      fetchAWFReflect: async () => ({
        ok: scenario !== "reflect-failure",
        reflectData: {
          endpoints: scenario === "missing-endpoint" ? [] : [{
            configured: true,
            provider: scenario === "wrong-endpoint" ? "anthropic" : env.GH_AW_LLM_PROVIDER,
            models_url: scenario === "missing-models-url" ? null : "http://api-proxy:10001/v1/models",
            port: 10001,
          }],
        },
      }),
    };
    if (name === "child_process") return {spawnSync: (command, childArgs, options) => {
      calls.push({command, args: Array.from(childArgs), options});
      if (scenario === "execution-failure") return {status: 7};
      if (scenario === "signal") return {status: null, signal: "SIGTERM"};
      if (scenario === "spawn-error") return {error: new Error("spawn failed")};
      return {status: 0};
    }};
    return require(name);
  },
  process: mockProcess,
});
const output = logs.join("");
assert.equal(output.includes(prompt), false);
for (const name of excludedEnv) {
  assert.equal(env[name], "sentinel-" + name, "parent credential must remain unchanged: " + name);
  assert.equal(output.includes(env[name]), false, "credential must not be logged: " + name);
}
const failures = {
  "missing-model": "FACTORY_MODEL must use provider/model format",
  "wrong-provider": "GH_AW_LLM_PROVIDER must be github, anthropic, or openai",
  "empty-model": "FACTORY_MODEL must use provider/model format",
  "missing-prompt": "GH_AW_PROMPT is required",
  "empty-prompt": "GH_AW_PROMPT must contain a non-empty instruction",
  "unreadable-prompt": "prompt read failed",
  "execution-failure": "Droid exited with code 7",
  "signal": "Droid terminated by signal SIGTERM",
  "spawn-error": "spawn failed",
  "no-awf": "Factory requires the AWF API proxy",
  "reflect-failure": "Unable to discover the Factory LLM endpoint",
  "missing-endpoint": "No configured /reflect endpoint found",
  "missing-models-url": "No configured /reflect models endpoint found",
  "wrong-endpoint": "No configured /reflect endpoint found",
};
if (failures[scenario]) {
  assert.equal(mockProcess.exitCode, scenario === "execution-failure" ? 7 : 1);
  assert.ok(output.includes(failures[scenario]), output);
} else {
  assert.equal(mockProcess.exitCode || 0, 0);
}
const ran = ["success", "copilot-chat-completions", "copilot-responses", "anthropic", "openai", "codex", "whitespace-model", "working-directory", "execution-failure", "signal", "spawn-error"].includes(scenario);
assert.equal(calls.length, ran ? 1 : 0);
assert.equal(cleaned, ran);
if (ran) {
  assert.equal(calls[0].command, "droid");
  assert.deepEqual(calls[0].args, [...args, "--settings", "/tmp/gh-aw/agent/factory-fixture/settings.json",
    "--model", "custom:gh-aw-0", "--file", env.GH_AW_PROMPT]);
  assert.equal(calls[0].options.cwd, scenario === "working-directory" ? "/workspace/subdir" : "/workspace");
  assert.equal(calls[0].options.shell, undefined);
  assert.equal(calls[0].options.stdio, "inherit");
  assert.notEqual(calls[0].options.env, env);
  for (const name of excludedEnv) {
    assert.equal(calls[0].options.env[name], undefined);
  }
  for (const [name, value] of Object.entries(env)) {
    if (!excludedEnv.includes(name)) assert.equal(calls[0].options.env[name], value);
  }
  assert.equal(settings.cloudSessionSync, false);
  assert.equal(settings.blockOnMcpLoad, true);
  assert.equal(settings.customModels.length, 1);
  const model = settings.customModels[0];
  assert.equal(model.apiKey, "awf-proxy");
  assert.equal(model.model, scenario === "anthropic" || scenario === "openai" || scenario === "codex" ? "fixture-model" :
    scenario === "copilot-chat-completions" ? "claude-haiku-5.5" :
    scenario === "copilot-responses" ? "gpt-5.3-codex" : "gpt-5.2");
  assert.equal(model.provider, scenario === "anthropic" ? "anthropic" :
    scenario === "copilot-chat-completions" ? "generic-chat-completion-api" : "openai");
  assert.equal(model.baseUrl, scenario === "anthropic" ? "http://api-proxy:10001" : "http://api-proxy:10001/v1");
}
})().catch(error => { console.error(error); process.exitCode = 1; });
`

func TestFactoryMCPAdapter(t *testing.T) {
	adapter := loadFactorySample(t).Behaviors.MCP.ConfigAdapter
	for _, scenario := range []string{"success", "invalid-json", "non-http-server", "missing-workspace"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			gateway := `{"mcpServers":{
				"github":{"url":"http://localhost:8080/mcp/github","headers":{"Authorization":"fixture-secret"},"tools":["search"]},
				"safeoutputs":{"url":"http://localhost:8080/mcp/safeoutputs"}
			}}`
			switch scenario {
			case "invalid-json":
				gateway = "{"
			case "non-http-server":
				gateway = `{"mcpServers":{"local":{"command":"node","args":["server.cjs"]}}}`
			}
			gatewayPath := filepath.Join(dir, "gateway.json")
			require.NoError(t, os.WriteFile(gatewayPath, []byte(gateway), 0o600))
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "node", "-e", adapter)
			cmd.Dir = "../../actions/setup/js"
			cmd.Env = []string{
				"PATH=" + os.Getenv("PATH"), "MCP_GATEWAY_OUTPUT=" + gatewayPath,
				"MCP_GATEWAY_DOMAIN=host.docker.internal", "MCP_GATEWAY_PORT=8080",
				`GH_AW_MCP_CLI_SERVERS=["safeoutputs"]`,
			}
			if scenario != "missing-workspace" {
				cmd.Env = append(cmd.Env, "GITHUB_WORKSPACE="+dir)
			}
			output, err := cmd.CombinedOutput()
			assert.NotContains(t, string(output), "fixture-secret")
			if scenario != "success" {
				require.Error(t, err, "%s", output)
				_, statErr := os.Stat(filepath.Join(dir, ".factory", "mcp.json"))
				require.True(t, os.IsNotExist(statErr))
				return
			}
			require.NoError(t, err, "%s", output)
			assert.Empty(t, output)
			configPath := filepath.Join(dir, ".factory", "mcp.json")
			content, err := os.ReadFile(configPath)
			require.NoError(t, err)
			var config struct {
				Servers map[string]map[string]any `json:"mcpServers"`
			}
			require.NoError(t, json.Unmarshal(content, &config))
			require.Len(t, config.Servers, 1)
			assert.Equal(t, map[string]any{
				"type": "http", "url": "http://host.docker.internal:8080/mcp/github",
				"oauth": false, "headers": map[string]any{"Authorization": "fixture-secret"},
			}, config.Servers["github"])
			info, err := os.Stat(configPath)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		})
	}
}
