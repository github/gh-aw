//go:build !integration && !windows

package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func loadGooseSample(t *testing.T) *EngineDefinition {
	t.Helper()
	source, err := os.ReadFile("../../.github/workflows/shared/goose.md")
	require.NoError(t, err)
	parts := strings.SplitN(string(source), "---", 3)
	require.Len(t, parts, 3)
	var frontmatter struct {
		Engine EngineDefinition `yaml:"engine"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(parts[1]), &frontmatter))
	require.NotNil(t, frontmatter.Engine.Behaviors)
	return &frontmatter.Engine
}

func TestGooseSampleExecution(t *testing.T) {
	def := loadGooseSample(t)
	require.Equal(t, "1.53.0", def.Version)
	engine, err := NewBehaviorDefinedEngine(def)
	require.NoError(t, err)
	assert.True(t, engine.GetCapabilities().MCP)
	assert.True(t, engine.GetCapabilities().MaxTurns)
	assert.True(t, engine.GetCapabilities().ToolsAllowlist)
	steps := engine.GetExecutionSteps(&WorkflowData{
		Name: "Goose", Model: "copilot/auto",
		EngineConfig: &EngineConfig{ID: "goose", Version: def.Version, MaxTurns: "30"},
	}, "/tmp/gh-aw/agent-stdio.log")
	execution := strings.Join(steps[len(steps)-1], "\n")
	assert.Contains(t, execution, "GH_AW_ENGINE_VERSION: 1.53.0")
	assert.Contains(t, execution, "GH_AW_MAX_TURNS: 30")
	assert.Contains(t, execution, "GOOSE_MODEL: copilot/auto")
	assert.Contains(t, execution, "GH_AW_LLM_PROVIDER: github")
	assert.Contains(t, execution, "AWF_REFLECT_ENABLED: 1")
	assert.NotContains(t, def.Behaviors.HarnessScript, "172.30.0.30")
	assert.NotContains(t, def.Behaviors.HarnessScript, `"-t", prompt`)
}

func TestGooseMCPAdapterUsesContainerGateway(t *testing.T) {
	def := loadGooseSample(t)
	dir := t.TempDir()
	adapter := filepath.Join(dir, "adapter.cjs")
	require.NoError(t, os.WriteFile(adapter, []byte(def.Behaviors.MCP.ConfigAdapter), 0o600))
	for _, name := range []string{"convert_gateway_config_shared.cjs", "error_helpers.cjs"} {
		content, err := os.ReadFile(filepath.Join("../../actions/setup/js", name))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), content, 0o600))
	}
	gateway := filepath.Join(dir, "gateway.json")
	require.NoError(t, os.WriteFile(gateway, []byte(`{"mcpServers":{
		"github":{"url":"http://localhost:8080/mcp/github","headers":{"Authorization":"Bearer test-only"},"tools":["read"]},
		"local":{"command":"node","args":["server.cjs"]},
		"safeoutputs":{"url":"http://localhost:8080/mcp/safeoutputs"}
	}}`), 0o600))

	for _, domain := range []string{"awmg-mcpg", "host.docker.internal"} {
		t.Run(domain, func(t *testing.T) {
			cmd := exec.Command("node", adapter)
			cmd.Env = append(os.Environ(), "MCP_GATEWAY_OUTPUT="+gateway,
				"GITHUB_WORKSPACE="+dir, "MCP_GATEWAY_DOMAIN="+domain,
				"MCP_GATEWAY_HOST_DOMAIN=localhost", "MCP_GATEWAY_PORT=8080",
				`GH_AW_MCP_CLI_SERVERS=["safeoutputs"]`)
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", output)
			configPath := filepath.Join(dir, ".goose", "mcp.json")
			content, err := os.ReadFile(configPath)
			require.NoError(t, err)
			var config struct {
				Servers map[string]map[string]any `json:"mcpServers"`
			}
			require.NoError(t, json.Unmarshal(content, &config))
			assert.Equal(t, "http://"+domain+":8080/mcp/github", config.Servers["github"]["url"])
			assert.Equal(t, "streamable_http", config.Servers["github"]["type"])
			assert.Equal(t, map[string]any{"Authorization": "Bearer test-only"}, config.Servers["github"]["headers"])
			assert.Equal(t, []any{"read"}, config.Servers["github"]["tools"])
			assert.Equal(t, "stdio", config.Servers["local"]["type"])
			assert.NotContains(t, config.Servers, "safeoutputs")
			stat, err := os.Stat(configPath)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o600), stat.Mode().Perm())
		})
	}
}

func TestGooseHarnessCLIContract(t *testing.T) {
	def := loadGooseSample(t)
	dir := t.TempDir()
	harness := filepath.Join(dir, "goose_harness.cjs")
	require.NoError(t, os.WriteFile(harness, []byte(def.Behaviors.HarnessScript), 0o600))
	reflect := `exports.fetchAWFReflect = async () => ({ok: true, reflectData: {}});
exports.resolveOpenAICompatibleEndpointFromReflect = () => ({provider: "github", host: "http://test-proxy:10002", basePath: "chat/completions"});`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "awf_reflect.cjs"), []byte(reflect), 0o600))
	prompt := filepath.Join(dir, "prompt.txt")
	require.NoError(t, os.WriteFile(prompt, []byte("test prompt"), 0o600))
	config := filepath.Join(dir, "mcp.json")
	require.NoError(t, os.WriteFile(config, []byte(`{"mcpServers":{
		"github":{"url":"http://awmg-mcpg:8080/mcp/github","headers":{"Authorization":"Bearer test-only"},"tools":["pull_request_read"]},
		"local":{"command":"node","args":["a b","quote'","$(not-a-command)"],"env":{"TEST":"a b"}}
	}}`), 0o600))
	binary := filepath.Join(dir, "test-goose")
	fixture := `#!/usr/bin/env node
const fs = require("fs");
if (process.argv.includes("--version")) { console.log("1.53.0"); process.exit(0); }
const configPath = process.env.GOOSE_ADDITIONAL_CONFIG_FILES;
fs.writeFileSync(process.env.TEST_OUTPUT, JSON.stringify({
	args: process.argv.slice(2),
	model: process.env.GOOSE_MODEL,
	provider: process.env.GOOSE_PROVIDER,
	host: process.env.OPENAI_HOST,
	basePath: process.env.OPENAI_BASE_PATH,
	root: process.env.GOOSE_PATH_ROOT,
	keyring: process.env.GOOSE_DISABLE_KEYRING,
	config: JSON.parse(fs.readFileSync(configPath, "utf8")),
	mode: fs.statSync(configPath).mode & 0o777
}));
process.exit(Number(process.env.TEST_EXIT || 0));
`
	require.NoError(t, os.WriteFile(binary, []byte(fixture), 0o700))
	outputPath := filepath.Join(dir, "output.json")
	run := func(extra ...string) ([]byte, error) {
		cmd := exec.Command("node", harness, binary)
		cmd.Env = append(os.Environ(), "GH_AW_ENGINE_VERSION="+def.Version,
			"GH_AW_MCP_CONFIG="+config, "GH_AW_PROMPT="+prompt,
			"GOOSE_MODEL=copilot/org/model", "GH_AW_MAX_TURNS=30",
			"AWF_REFLECT_ENABLED=1", "GH_AW_LLM_PROVIDER=github",
			"TEST_OUTPUT="+outputPath)
		cmd.Env = append(cmd.Env, extra...)
		return cmd.CombinedOutput()
	}
	t.Run("headless invocation", func(t *testing.T) {
		output, err := run()
		require.NoError(t, err, "%s", output)
		assert.NotContains(t, string(output), "Bearer test-only")
		content, err := os.ReadFile(outputPath)
		require.NoError(t, err)
		var result struct {
			Args     []string       `json:"args"`
			Model    string         `json:"model"`
			Provider string         `json:"provider"`
			Host     string         `json:"host"`
			BasePath string         `json:"basePath"`
			Root     string         `json:"root"`
			Keyring  string         `json:"keyring"`
			Config   map[string]any `json:"config"`
			Mode     int            `json:"mode"`
		}
		require.NoError(t, json.Unmarshal(content, &result))
		assert.Equal(t, []string{"run", "--no-session", "--with-builtin", "developer", "--output-format", "stream-json", "--max-turns", "30", "--instructions", prompt}, result.Args)
		assert.Equal(t, "org/model", result.Model)
		assert.Equal(t, "openai", result.Provider)
		assert.Equal(t, "http://test-proxy:10002", result.Host)
		assert.Equal(t, "chat/completions", result.BasePath)
		assert.Equal(t, "1", result.Keyring)
		assert.Equal(t, 0o600, result.Mode)
		extensions := result.Config["extensions"].(map[string]any)
		http := extensions["github"].(map[string]any)
		assert.Equal(t, "http://awmg-mcpg:8080/mcp/github", http["uri"])
		assert.Equal(t, map[string]any{"Authorization": "Bearer test-only"}, http["headers"])
		assert.Equal(t, []any{"pull_request_read"}, http["available_tools"])
		local := extensions["local"].(map[string]any)
		assert.Equal(t, "stdio", local["type"])
		assert.Equal(t, []any{"a b", "quote'", "$(not-a-command)"}, local["args"])
		assert.Equal(t, map[string]any{"TEST": "a b"}, local["envs"])
		_, err = os.Stat(filepath.Dir(result.Root))
		assert.True(t, os.IsNotExist(err), "temporary config must be removed")
	})
	t.Run("exit status", func(t *testing.T) {
		output, err := run("TEST_EXIT=7")
		var exit *exec.ExitError
		require.ErrorAs(t, err, &exit)
		assert.Equal(t, 7, exit.ExitCode())
		assert.Contains(t, string(output), "Goose execution failed with exit code 7")
	})
	for _, turns := range []string{"0", "-1", "NaN", "4294967296"} {
		t.Run("invalid turns "+turns, func(t *testing.T) {
			output, err := run("GH_AW_MAX_TURNS=" + turns)
			require.Error(t, err)
			assert.Contains(t, string(output), "GH_AW_MAX_TURNS must be a positive 32-bit integer")
		})
	}
	t.Run("missing model", func(t *testing.T) {
		output, err := run("GOOSE_MODEL=")
		require.Error(t, err)
		assert.Contains(t, string(output), "GOOSE_MODEL is required")
	})
	t.Run("version mismatch", func(t *testing.T) {
		output, err := run("GH_AW_ENGINE_VERSION=9.9.9")
		require.Error(t, err)
		assert.Contains(t, string(output), "Goose binary version does not match")
	})
}

func TestGooseStructuredLogParser(t *testing.T) {
	def := loadGooseSample(t)
	script := def.Behaviors.LogParser + `
const fs = require("fs");
console.log(JSON.stringify(parseLog(fs.readFileSync(0, "utf8"))));
`
	log := `[INFO] infrastructure
Warning: Failed to start extension 'broken' (connection error)
{"type":"message","message":{"id":"a","role":"assistant","content":[{"type":"text","text":"Checking"},{"type":"toolRequest","id":"t","toolCall":{"status":"success","value":{"name":"github__pull_request_read","arguments":{"method":"get","pullNumber":42}}}}]}}
{"type":"message","message":{"id":"b","role":"user","content":[{"type":"toolResponse","id":"t","toolResult":{"status":"success","value":{"content":[{"type":"text","text":"PR 42"}]}}}]}}
{"type":"message","message":{"id":"c","role":"assistant","content":[{"type":"text","text":"Done"}]}}
{"type":"complete","input_tokens":123,"output_tokens":45,"cache_read_input_tokens":67}
`
	cmd := exec.Command("node", "-e", script)
	cmd.Dir = "../../actions/setup/js"
	cmd.Stdin = strings.NewReader(log)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	var result struct {
		LogEntries  []map[string]any `json:"logEntries"`
		MCPFailures []string         `json:"mcpFailures"`
		MaxTurnsHit bool             `json:"maxTurnsHit"`
	}
	require.NoError(t, json.Unmarshal(output, &result))
	assert.Equal(t, []string{"broken"}, result.MCPFailures)
	assert.False(t, result.MaxTurnsHit)
	require.Len(t, result.LogEntries, 7)
	assert.Contains(t, string(output), `"toolName":"github__pull_request_read"`)
	assert.Contains(t, string(output), `"toolCallId":"t"`)
	data := result.LogEntries[6]["data"].(map[string]any)
	assert.Equal(t, map[string]any{"input_tokens": float64(123), "output_tokens": float64(45), "cache_read_input_tokens": float64(67), "input_tokens_include_cache": true}, data["usage"])
	for _, event := range result.LogEntries {
		assert.Contains(t, event["type"], ".")
		assert.Contains(t, event, "data")
	}
}

func TestGooseDetectionDoesNotInheritCLIVersion(t *testing.T) {
	compiler := NewCompiler()
	compiler.engineCatalog.Register(loadGooseSample(t))
	data := &WorkflowData{
		AI: "goose", Model: "copilot/auto",
		EngineConfig: &EngineConfig{ID: "goose", Version: "1.53.0"},
		SafeOutputs:  &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{}},
	}
	for name, steps := range map[string][]string{
		"inline":   compiler.buildDetectionEngineExecutionStep(data),
		"external": compiler.buildInstallDetectionEngineForExternalDetectorStep(data),
	} {
		t.Run(name, func(t *testing.T) {
			content := strings.Join(steps, "")
			assert.Contains(t, content, "Install GitHub Copilot CLI")
			assert.NotContains(t, content, "1.53.0")
		})
	}
	assert.Equal(t, "1.53.0", data.EngineConfig.Version)
}
