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

func loadFactorySample(t *testing.T) *EngineDefinition {
	t.Helper()
	content, err := os.ReadFile("../../.github/workflows/shared/factory.md")
	require.NoError(t, err)
	parts := strings.SplitN(string(content), "---", 3)
	require.Len(t, parts, 3)
	var frontmatter struct {
		Engine EngineDefinition `yaml:"engine"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(parts[1]), &frontmatter))
	require.NotNil(t, frontmatter.Engine.Behaviors)
	return &frontmatter.Engine
}

func TestFactorySampleExecution(t *testing.T) {
	def := loadFactorySample(t)
	engine, err := NewBehaviorDefinedEngine(def)
	require.NoError(t, err)
	assert.Equal(t, "factory", engine.GetID())
	assert.True(t, def.Experimental)
	assert.Equal(t, []string{"FACTORY_API_KEY"}, engine.GetSupportedEnvVarKeys())
	assert.Empty(t, engine.GetLogParserScriptId(), "text output must not imply structured session support")
	assert.Equal(t, []string{"exec", "--auto", "high", "--output-format", "text"}, def.Behaviors.Execution.Args)
	assert.False(t, def.Behaviors.Installation.PostInstallScripts)

	data := &WorkflowData{
		Name:         "factory-test",
		Model:        "factory/claude-sonnet-4-6",
		EngineConfig: &EngineConfig{ID: "factory", Version: def.Version},
	}
	var installation strings.Builder
	for _, step := range engine.GetInstallationSteps(data) {
		installation.WriteString(strings.Join(step, "\n"))
	}
	assert.Contains(t, installation.String(), "@factory/cli@"+def.Version)
	assert.Contains(t, installation.String(), "--ignore-scripts")
	assert.Contains(t, installation.String(), "droid --version")
	assert.Contains(t, strings.Join(engine.GetSecretValidationStep(data), "\n"), "FACTORY_API_KEY")

	var execution strings.Builder
	for _, step := range engine.GetExecutionSteps(data, "/tmp/factory.log") {
		execution.WriteString(strings.Join(step, "\n"))
	}
	assert.Contains(t, execution.String(), "factory_harness.cjs")
	assert.Contains(t, execution.String(), "FACTORY_MODEL: factory/claude-sonnet-4-6")
	assert.Contains(t, execution.String(), "FACTORY_API_KEY: ${{ secrets.FACTORY_API_KEY }}")
	assert.Contains(t, execution.String(), "GH_AW_ENGINE_VERSION: "+def.Version)
	assert.Equal(t, "copilot", def.DetectionEngine)
	assert.Contains(t, def.Behaviors.Network.ProviderDomains["factory"], "factory.ai")
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
		"FACTORY_MODEL: factory/claude-sonnet-4-6",
		"GH_AW_MCP_CONFIG: .factory/mcp.json",
		"factory_mcp_config_adapter.cjs",
		"*.factory.ai",
		"proxy.golang.org",
		`"GITHUB_READ_ONLY": "1"`,
	} {
		assert.Contains(t, string(lock), expected)
	}
	assert.NotContains(t, string(lock), "--enable-gh-proxy")
}

func TestFactoryHarness(t *testing.T) {
	def := loadFactorySample(t)
	harness := filepath.Join(t.TempDir(), "factory_harness.cjs")
	require.NoError(t, os.WriteFile(harness, []byte(def.Behaviors.HarnessScript), 0o600))
	for _, scenario := range []string{
		"success", "secret-fallback", "missing-key", "missing-model", "wrong-provider",
		"empty-model", "missing-prompt", "empty-prompt", "unreadable-prompt",
		"execution-failure", "signal", "spawn-error",
	} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "node", "-e", factoryHarnessTestDriver, harness, scenario)
			cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", output)
		})
	}
}

const factoryHarnessTestDriver = `
const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");
const [scriptPath, scenario] = process.argv.slice(1);
const calls = [];
const logs = [];
const prompt = "--model injected\n$(touch /not-executed)";
const key = "fixture-factory-key";
const env = {FACTORY_API_KEY: key, FACTORY_MODEL: "factory/claude-sonnet-4-6",
  GH_AW_PROMPT: "/prompt with spaces.md", GITHUB_WORKSPACE: "/workspace"};
if (scenario === "secret-fallback") {
  delete env.FACTORY_API_KEY;
  env.SECRET_FACTORY_API_KEY = key;
}
if (scenario === "missing-key") delete env.FACTORY_API_KEY;
if (scenario === "missing-model") delete env.FACTORY_MODEL;
if (scenario === "wrong-provider") env.FACTORY_MODEL = "openai/gpt-5.2";
if (scenario === "empty-model") env.FACTORY_MODEL = "factory/ ";
if (scenario === "missing-prompt") delete env.GH_AW_PROMPT;
const args = ["exec", "--auto", "high", "--output-format", "text"];
const mockProcess = {argv: ["node", scriptPath, "droid", ...args], env,
  stderr: {write: text => logs.push(text)}};
vm.runInNewContext(fs.readFileSync(scriptPath, "utf8"), {
  require: name => {
    if (name === "fs") return {readFileSync: path => {
      assert.equal(path, "/prompt with spaces.md");
      if (scenario === "unreadable-prompt") throw new Error("prompt read failed");
      return scenario === "empty-prompt" ? " \n" : prompt;
    }};
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
assert.equal(output.includes(key), false);
assert.equal(output.includes(prompt), false);
const failures = {
  "missing-key": "FACTORY_API_KEY is required",
  "missing-model": "FACTORY_MODEL must use factory/model format",
  "wrong-provider": "FACTORY_MODEL must use factory/model format",
  "empty-model": "FACTORY_MODEL must use factory/model format",
  "missing-prompt": "GH_AW_PROMPT is required",
  "empty-prompt": "GH_AW_PROMPT must contain a non-empty instruction",
  "unreadable-prompt": "prompt read failed",
  "execution-failure": "Droid exited with code 7",
  "signal": "Droid terminated by signal SIGTERM",
  "spawn-error": "spawn failed",
};
if (failures[scenario]) {
  assert.equal(mockProcess.exitCode, scenario === "execution-failure" ? 7 : 1);
  assert.ok(output.includes(failures[scenario]), output);
} else {
  assert.equal(mockProcess.exitCode || 0, 0);
}
const ran = ["success", "secret-fallback", "execution-failure", "signal", "spawn-error"].includes(scenario);
assert.equal(calls.length, ran ? 1 : 0);
if (ran) {
  assert.equal(calls[0].command, "droid");
  assert.deepEqual(calls[0].args, [...args, "--model", "claude-sonnet-4-6", "--file", env.GH_AW_PROMPT]);
  assert.equal(calls[0].options.cwd, "/workspace");
  assert.equal(calls[0].options.shell, undefined);
  assert.equal(calls[0].options.stdio, "inherit");
  assert.equal(calls[0].options.env.FACTORY_API_KEY, key);
}
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
