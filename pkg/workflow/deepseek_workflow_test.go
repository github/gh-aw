//go:build !integration && !windows

package workflow

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func loadDeepSeekSample(t *testing.T) *EngineDefinition {
	t.Helper()
	content, err := os.ReadFile("../../.github/workflows/shared/deepseek-harness.md")
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

func deepSeekHarnessCommand(t *testing.T, dir, command string, args ...string) *exec.Cmd {
	t.Helper()
	harnessPath := filepath.Join(dir, "deepseek_harness.cjs")
	require.NoError(t, os.WriteFile(harnessPath, []byte(loadDeepSeekSample(t).Behaviors.HarnessScript), 0o600))
	actionsDir, err := filepath.Abs("../../actions/setup/js")
	require.NoError(t, err)
	reflectPath, err := json.Marshal(filepath.Join(actionsDir, "awf_reflect.cjs"))
	require.NoError(t, err)
	reflectSource := "module.exports = { ...require(" + string(reflectPath) + `),
fetchAWFReflect: async () => JSON.parse(process.env.GH_AW_DEEPSEEK_TEST_REFLECT || "{}") };`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "awf_reflect.cjs"), []byte(reflectSource), 0o600))
	promptPath := filepath.Join(dir, "prompt with spaces.md")
	require.NoError(t, os.WriteFile(promptPath, []byte("- prompt 'with quotes'\n$(not-a-command)\n"), 0o600))
	cmd := exec.Command("node", append([]string{harnessPath, command}, args...)...)
	cmd.Env = append(os.Environ(),
		"GITHUB_WORKSPACE="+dir, "GH_AW_ENGINE_CWD="+dir, "GH_AW_PROMPT="+promptPath,
		"DSH_MODEL=openai/test-model", "GH_AW_LLM_PROVIDER=openai", "AWF_REFLECT_ENABLED=0",
		"OPENAI_API_KEY=test-key", "OPENAI_BASE_URL=http://localhost:1234/v1",
		"ANTHROPIC_API_KEY=test-anthropic-key", "ANTHROPIC_BASE_URL=http://localhost:1235",
		"CODEX_API_KEY=", "DSH_TELEMETRY_DISABLED=1", "DSH_TOOLS_MODE=native",
		"DSH_PERMISSION_MODE=danger-full-access",
	)
	return cmd
}

func deepSeekProbe(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "probe.cjs")
	source := `const fs = require("fs");
const args = process.argv.slice(2);
const patchPath = args[args.indexOf("--patch") + 1];
console.log(JSON.stringify({
  args, prompt: fs.readFileSync(0, "utf8"), cwd: process.cwd(), home: process.env.DSH_HOME,
  key: process.env.OPENAI_API_KEY, anthropicKey: process.env.ANTHROPIC_API_KEY,
  patchMode: fs.statSync(patchPath).mode & 0o777,
  homeMode: fs.statSync(process.env.DSH_HOME).mode & 0o777,
  patch: JSON.parse(fs.readFileSync(patchPath, "utf8"))
}));`
	require.NoError(t, os.WriteFile(path, []byte(source), 0o600))
	return path
}

type deepSeekProbeResult struct {
	Args         []string `json:"args"`
	Prompt       string   `json:"prompt"`
	Cwd          string   `json:"cwd"`
	Home         string   `json:"home"`
	Key          string   `json:"key"`
	AnthropicKey string   `json:"anthropicKey"`
	PatchMode    int      `json:"patchMode"`
	HomeMode     int      `json:"homeMode"`
	Patch        []struct {
		ID     string `json:"id"`
		Config struct {
			Provider  string `json:"provider"`
			Model     string `json:"model"`
			Providers map[string]struct {
				APIKeyEnv string `json:"apiKeyEnv"`
				API       string `json:"api"`
				BaseURL   string `json:"baseURL"`
				Models    []struct {
					ID string `json:"id"`
				} `json:"models"`
			} `json:"providers"`
		} `json:"config"`
	} `json:"patch"`
}

func runDeepSeekProbe(t *testing.T, cmd *exec.Cmd) deepSeekProbeResult {
	t.Helper()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	require.NoError(t, err, "%s", stderr.String())
	var result deepSeekProbeResult
	require.NoError(t, json.Unmarshal(output, &result))
	assert.NotContains(t, stderr.String(), "test-key")
	assert.NotContains(t, stderr.String(), "$(not-a-command)")
	require.Len(t, result.Patch, 2)
	assert.Equal(t, "agent-default-model", result.Patch[0].ID)
	assert.Equal(t, "awf-proxy", result.Patch[0].Config.Provider)
	assert.Equal(t, "llm-pi-ai", result.Patch[1].ID)
	assert.Equal(t, 0o600, result.PatchMode)
	assert.Equal(t, 0o700, result.HomeMode)
	assert.Equal(t, "-", result.Args[len(result.Args)-1])
	return result
}

func TestDeepSeekSampleProviderExecution(t *testing.T) {
	def := loadDeepSeekSample(t)
	require.Equal(t, "0.2.0-rc.2", def.Version)
	engine, err := NewBehaviorDefinedEngine(def)
	require.NoError(t, err)
	assert.False(t, engine.GetCapabilities().MCP)
	assert.Equal(t, "@deepseek-ai/dsh", def.Behaviors.Installation.PackageName)
	assert.True(t, def.Behaviors.Installation.PostInstallScripts)
	for _, test := range []struct{ model, provider string }{
		{"copilot/auto", "github"}, {"openai/gpt-5", "openai"},
		{"codex/gpt-5", "openai"}, {"anthropic/claude-sonnet-4-5", "anthropic"},
	} {
		t.Run(test.model, func(t *testing.T) {
			data := &WorkflowData{Name: "DeepSeek", Model: test.model, EngineConfig: &EngineConfig{Version: def.Version}}
			steps := engine.GetExecutionSteps(data, "/tmp/gh-aw/agent-stdio.log")
			require.NotEmpty(t, steps)
			execution := strings.Join(steps[len(steps)-1], "\n")
			assert.Contains(t, execution, "DSH_MODEL: "+test.model)
			assert.Contains(t, execution, "GH_AW_LLM_PROVIDER: "+test.provider)
			assert.Contains(t, execution, "AWF_REFLECT_ENABLED: 1")
			assert.Contains(t, execution, "DSH_TELEMETRY_DISABLED: 1")
			assert.Contains(t, execution, "deepseek-harness_harness.cjs")
			installation := strings.Join(flattenSteps(engine.GetInstallationSteps(data)), "\n")
			assert.Contains(t, installation, "@deepseek-ai/dsh@0.2.0-rc.2")
			assert.Contains(t, installation, "dsh --version")
		})
	}
}

func TestDeepSeekHarnessProviderRouting(t *testing.T) {
	for _, test := range []struct{ provider, reflectedProvider, baseURL, protocol string }{
		{"github", "copilot", "http://api-proxy:4000", "openai-completions"},
		{"openai", "openai", "http://api-proxy:4001/custom/v1", "openai-completions"},
		{"anthropic", "anthropic", "http://api-proxy:4002", "anthropic-messages"},
	} {
		t.Run(test.provider, func(t *testing.T) {
			dir := t.TempDir()
			cmd := deepSeekHarnessCommand(t, dir, "node", deepSeekProbe(t, dir), "--profile", "headless")
			modelsURL := test.baseURL + "/models"
			if test.provider == "anthropic" {
				modelsURL = test.baseURL + "/v1/models"
			}
			reflect, err := json.Marshal(map[string]any{"ok": true, "reflectData": map[string]any{"endpoints": []any{
				map[string]any{"provider": "unrelated", "configured": true, "models_url": "http://wrong/v1/models"},
				map[string]any{"provider": test.reflectedProvider, "configured": true, "models_url": modelsURL},
			}}})
			require.NoError(t, err)
			cmd.Env = append(cmd.Env, "AWF_REFLECT_ENABLED=1", "GH_AW_LLM_PROVIDER="+test.provider,
				"DSH_MODEL="+test.reflectedProvider+"/test/model", "HOSTALIASES=", "GH_AW_DEEPSEEK_TEST_REFLECT="+string(reflect))
			result := runDeepSeekProbe(t, cmd)
			route := result.Patch[1].Config.Providers["awf-proxy"]
			assert.Equal(t, test.baseURL, route.BaseURL)
			assert.Equal(t, test.protocol, route.API)
			assert.Equal(t, "test/model", result.Patch[0].Config.Model)
			require.Len(t, route.Models, 1)
			assert.Equal(t, "test/model", route.Models[0].ID)
			key := result.Key
			if test.provider == "anthropic" {
				assert.Equal(t, "ANTHROPIC_API_KEY", route.APIKeyEnv)
				key = result.AnthropicKey
			}
			assert.Equal(t, "awf-proxy", key)
		})
	}
}

func TestDeepSeekHarnessDirectProviders(t *testing.T) {
	for _, test := range []struct {
		name, provider, baseURL, key string
		env                          []string
	}{
		{"OpenAI", "openai", "http://localhost:1234/v1", "test-key", nil},
		{"OpenAI default", "openai", "https://api.openai.com/v1", "test-key", []string{"OPENAI_BASE_URL="}},
		{"Codex key", "openai", "http://localhost:1234/v1", "test-codex-key", []string{"OPENAI_API_KEY=", "CODEX_API_KEY=test-codex-key"}},
		{"Anthropic", "anthropic", "http://localhost:1235", "test-anthropic-key", nil},
		{"Anthropic v1", "anthropic", "http://localhost:1235", "test-anthropic-key", []string{"ANTHROPIC_BASE_URL=http://localhost:1235/v1/"}},
		{"Anthropic default", "anthropic", "https://api.anthropic.com", "test-anthropic-key", []string{"ANTHROPIC_BASE_URL="}},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			cmd := deepSeekHarnessCommand(t, dir, "node", deepSeekProbe(t, dir), "--profile", "headless")
			cmd.Env = append(cmd.Env, "GH_AW_LLM_PROVIDER="+test.provider)
			cmd.Env = append(cmd.Env, test.env...)
			result := runDeepSeekProbe(t, cmd)
			route := result.Patch[1].Config.Providers["awf-proxy"]
			assert.Equal(t, test.baseURL, route.BaseURL)
			key := result.Key
			if test.provider == "anthropic" {
				assert.Equal(t, "anthropic-messages", route.API)
				assert.Equal(t, "ANTHROPIC_API_KEY", route.APIKeyEnv)
				key = result.AnthropicKey
			} else {
				assert.Equal(t, "openai-completions", route.API)
				assert.Equal(t, "OPENAI_API_KEY", route.APIKeyEnv)
			}
			assert.Equal(t, test.key, key)
		})
	}
}

func TestDeepSeekHarnessPreservesPromptAndRepositorySettings(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, ".dsh")
	require.NoError(t, os.Mkdir(home, 0o700))
	settings := filepath.Join(home, "settings.yaml")
	require.NoError(t, os.WriteFile(settings, []byte("existing: settings\n"), 0o600))
	cwd := filepath.Join(dir, "directory with spaces")
	require.NoError(t, os.Mkdir(cwd, 0o700))
	prompt := "- prompt 'with quotes'\n$(not-a-command)\n" + strings.Repeat("large prompt\n", 20000)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "prompt with spaces.md"), []byte(prompt), 0o600))
	probe := deepSeekProbe(t, dir)
	var previousHome string
	for range 2 {
		cmd := deepSeekHarnessCommand(t, dir, "node", probe, "--profile", "headless")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "prompt with spaces.md"), []byte(prompt), 0o600))
		cmd.Env = append(cmd.Env, "GH_AW_ENGINE_CWD="+cwd)
		result := runDeepSeekProbe(t, cmd)
		assert.Equal(t, prompt, result.Prompt)
		assert.NotContains(t, strings.Join(result.Args, " "), prompt)
		assert.NotEqual(t, previousHome, result.Home)
		previousHome = result.Home
		actualCwd, err := filepath.EvalSymlinks(cwd)
		require.NoError(t, err)
		assert.Equal(t, actualCwd, result.Cwd)
		assert.Equal(t, "test-key", result.Key)
		patch, err := os.ReadFile(filepath.Join(result.Home, "cordis.patch.yml"))
		require.NoError(t, err)
		assert.NotContains(t, string(patch), "test-key")
	}
	content, err := os.ReadFile(settings)
	require.NoError(t, err)
	assert.Equal(t, "existing: settings\n", string(content))
}

func TestDeepSeekHarnessRejectsInvalidConfiguration(t *testing.T) {
	for _, test := range []struct {
		name, env, message string
	}{
		{"missing command", "", "DeepSeek Harness command is required"},
		{"empty provider", "DSH_MODEL=/model", "DSH_MODEL must use provider/model format"},
		{"empty model", "DSH_MODEL=openai/", "DSH_MODEL must use provider/model format"},
		{"unknown provider", "GH_AW_LLM_PROVIDER=unrelated", "GH_AW_LLM_PROVIDER must be"},
		{"missing key", "OPENAI_API_KEY=", "OPENAI_API_KEY is required without AWF"},
		{"Copilot without AWF", "GH_AW_LLM_PROVIDER=github", "Copilot routing requires the AWF sandbox"},
		{"missing prompt", "GH_AW_PROMPT=", "GH_AW_PROMPT is required"},
		{"reflect failure", "AWF_REFLECT_ENABLED=1", "Unable to discover"},
	} {
		t.Run(test.name, func(t *testing.T) {
			command := "node"
			if test.name == "missing command" {
				command = ""
			}
			cmd := deepSeekHarnessCommand(t, t.TempDir(), command)
			cmd.Env = append(cmd.Env, test.env)
			output, err := cmd.CombinedOutput()
			require.Error(t, err)
			assert.Contains(t, string(output), test.message)
		})
	}
}

func TestDeepSeekHarnessRejectsWrongReflectEndpoint(t *testing.T) {
	for _, endpoints := range []string{
		`[{"provider":"anthropic","configured":true,"models_url":"http://wrong/v1/models"}]`,
		`[{"provider":"openai","configured":false,"models_url":"http://wrong/v1/models"}]`,
		`[{"provider":"openai","configured":true,"port":4000}]`,
	} {
		cmd := deepSeekHarnessCommand(t, t.TempDir(), "node")
		cmd.Env = append(cmd.Env, "AWF_REFLECT_ENABLED=1",
			`GH_AW_DEEPSEEK_TEST_REFLECT={"ok":true,"reflectData":{"endpoints":`+endpoints+`}}`)
		output, err := cmd.CombinedOutput()
		require.Error(t, err)
		assert.Contains(t, string(output), "No configured /reflect models endpoint found for provider openai")
	}
}

func TestDeepSeekHarnessPreservesProcessFailures(t *testing.T) {
	for _, test := range []struct {
		name, source string
		code         int
	}{
		{"exit code", "process.exit(7)", 7},
		{"signal", `process.kill(process.pid, "SIGTERM")`, 143},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			fixture := filepath.Join(dir, "failure.cjs")
			require.NoError(t, os.WriteFile(fixture, []byte(test.source), 0o600))
			output, err := deepSeekHarnessCommand(t, dir, "node", fixture).CombinedOutput()
			var exitError *exec.ExitError
			require.ErrorAs(t, err, &exitError)
			assert.Equal(t, test.code, exitError.ExitCode())
			assert.Contains(t, string(output), "DeepSeek Harness execution failed")
		})
	}
	dir := t.TempDir()
	output, err := deepSeekHarnessCommand(t, dir, filepath.Join(dir, "missing-dsh")).CombinedOutput()
	require.Error(t, err)
	assert.Contains(t, string(output), "ENOENT")
}

func deepSeekMockProvider(t *testing.T, anthropic bool) (*httptest.Server, chan map[string]any) {
	t.Helper()
	requests := make(chan map[string]any, 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := "/v1/chat/completions"
		if anthropic {
			path = "/v1/messages"
		}
		assert.Equal(t, path, r.URL.Path)
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("invalid provider request: %v", err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		requests <- request
		w.Header().Set("Content-Type", "text/event-stream")
		response := "data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"DeepSeek integration verified\"},\"finish_reason\":null}]}\n\n" +
			"data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n" +
			"data: [DONE]\n\n"
		if anthropic {
			response = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"test\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"test-model\",\"content\":[],\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n" +
				"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
				"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"DeepSeek integration verified\"}}\n\n" +
				"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
				"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":5}}\n\n" +
				"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
		}
		messages, err := json.Marshal(request["messages"])
		if err != nil {
			t.Errorf("invalid request messages: %v", err)
			http.Error(w, "invalid messages", http.StatusBadRequest)
			return
		}
		if tools, ok := request["tools"].([]any); ok && len(tools) > 0 &&
			!bytes.Contains(messages, []byte(`"role":"tool"`)) && !bytes.Contains(messages, []byte(`"type":"tool_result"`)) {
			response = deepSeekMockToolResponse(t, anthropic)
		}
		_, err = w.Write([]byte(response))
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	return server, requests
}

func deepSeekMockToolResponse(t *testing.T, anthropic bool) string {
	t.Helper()
	args := map[string]string{
		"command":     "printf 'native shell verified' > deepseek-tool-probe.txt && cat deepseek-tool-probe.txt",
		"description": "Write and read the integration test fixture",
	}
	arguments, err := json.Marshal(args)
	require.NoError(t, err)
	if anthropic {
		return "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"test\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"test-model\",\"content\":[],\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n" +
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"test-tool\",\"name\":\"bash\",\"input\":{}}}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":" + string(mustMarshalDeepSeekJSON(t, string(arguments))) + "}}\n\n" +
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":5}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	}
	chunk := map[string]any{
		"id": "test", "object": "chat.completion.chunk", "choices": []any{map[string]any{
			"index": 0, "finish_reason": "tool_calls", "delta": map[string]any{
				"role": "assistant", "tool_calls": []any{map[string]any{
					"index": 0, "id": "test-tool", "type": "function",
					"function": map[string]any{"name": "bash", "arguments": string(arguments)},
				}},
			},
		}},
	}
	return "data: " + string(mustMarshalDeepSeekJSON(t, chunk)) + "\n\ndata: [DONE]\n\n"
}

func mustMarshalDeepSeekJSON(t *testing.T, value any) []byte {
	t.Helper()
	result, err := json.Marshal(value)
	require.NoError(t, err)
	return result
}

func TestDeepSeekHarnessPublishedCLI(t *testing.T) {
	command := os.Getenv("GH_AW_DEEPSEEK_TEST_CLI")
	if command == "" {
		t.Skip("set GH_AW_DEEPSEEK_TEST_CLI to a pinned dsh binary for local mock-provider validation")
	}
	for _, provider := range []string{"openai", "anthropic", "github"} {
		t.Run(provider, func(t *testing.T) {
			server, requests := deepSeekMockProvider(t, provider == "anthropic")
			dir := t.TempDir()
			cmd := deepSeekHarnessCommand(t, dir, command, "--profile", "headless")
			modelsURL := server.URL + "/v1/models"
			reflect, err := json.Marshal(map[string]any{"ok": true, "reflectData": map[string]any{"endpoints": []any{
				map[string]any{"provider": provider, "configured": true, "models_url": modelsURL},
			}}})
			require.NoError(t, err)
			cmd.Env = append(cmd.Env, "GH_AW_LLM_PROVIDER="+provider, "AWF_REFLECT_ENABLED=1",
				"DSH_MODEL="+provider+"/test-model", "GH_AW_DEEPSEEK_TEST_REFLECT="+string(reflect))
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", output)
			assert.Contains(t, string(output), "DeepSeek integration verified")
			require.NotEmpty(t, requests)
			fixture, err := os.ReadFile(filepath.Join(dir, "deepseek-tool-probe.txt"))
			require.NoError(t, err)
			assert.Equal(t, "native shell verified", string(fixture))
			var transcript strings.Builder
			for len(requests) > 0 {
				request := <-requests
				assert.Equal(t, "test-model", request["model"])
				messages, err := json.Marshal(request["messages"])
				require.NoError(t, err)
				transcript.Write(messages)
			}
			assert.Contains(t, transcript.String(), "$(not-a-command)")
			assert.Contains(t, transcript.String(), "native shell verified")
		})
	}
}
