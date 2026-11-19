//go:build !integration && !windows

package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const langGraphSamplePath = "../../.github/workflows/shared/langgraph.md"

func loadLangGraphSample(t *testing.T) *EngineDefinition {
	t.Helper()
	var frontmatter struct {
		Engine EngineDefinition `yaml:"engine"`
	}
	readConformanceFrontmatter(t, langGraphSamplePath, &frontmatter)
	return &frontmatter.Engine
}

func langGraphBootstrapScript(t *testing.T) (string, map[string]string) {
	t.Helper()
	var frontmatter struct {
		Steps []struct {
			Run string            `yaml:"run"`
			Env map[string]string `yaml:"env"`
		} `yaml:"pre-agent-steps"`
	}
	readConformanceFrontmatter(t, langGraphSamplePath, &frontmatter)
	require.Len(t, frontmatter.Steps, 1)
	run := frontmatter.Steps[0].Run
	require.True(t, strings.HasPrefix(run, "python3 -I - <<'PY'\n"))
	require.True(t, strings.HasSuffix(run, "\nPY\n"))
	return strings.TrimSuffix(strings.TrimPrefix(run, "python3 -I - <<'PY'\n"), "\nPY\n"), frontmatter.Steps[0].Env
}

func TestLangGraphSampleLock(t *testing.T) {
	def := loadLangGraphSample(t)
	script, env := langGraphBootstrapScript(t)
	hash := def.Behaviors.Execution.Env["GH_AW_LANGGRAPH_DEPS_LOCK_SHA256"]
	require.Len(t, hash, 64)
	assert.Equal(t, hash, env["GH_AW_LANGGRAPH_DEPS_LOCK_SHA256"])
	assert.Equal(t, def.Behaviors.Execution.Env["GH_AW_LANGGRAPH_DEPS"], env["GH_AW_LANGGRAPH_DEPS"])

	match := regexp.MustCompile(`(?s)requirements = """(.*?)"""`).FindStringSubmatch(script)
	require.Len(t, match, 2)
	sum := sha256.Sum256([]byte(match[1]))
	assert.Equal(t, hash, hex.EncodeToString(sum[:]), "embedded lock must match its trusted hash")
	for line := range strings.SplitSeq(strings.TrimSpace(match[1]), "\n") {
		assert.Regexp(t, `^[A-Za-z0-9_.-]+==\S+ --hash=sha256:[0-9a-f]{64}$`, line)
	}
	for _, pkg := range []string{"langgraph==" + def.Version, "langchain_openai==", "langchain_anthropic==", "langchain_core=="} {
		assert.Contains(t, match[1], "\n"+pkg, "lock must pin %s", pkg)
	}
}

func TestLangGraphSampleExecution(t *testing.T) {
	def := loadLangGraphSample(t)
	engine, err := NewBehaviorDefinedEngine(def)
	require.NoError(t, err)
	assert.True(t, def.Experimental)
	assert.False(t, engine.GetCapabilities().MCP)
	assert.True(t, engine.GetCapabilities().MaxTurns)
	for _, step := range engine.GetInstallationSteps(&WorkflowData{}) {
		assert.NotContains(t, strings.Join(step, "\n"), "pip install")
	}
	for _, test := range []struct{ model, provider string }{
		{"copilot/claude-haiku-5.5", "github"},
		{"openai/gpt-4o", "openai"},
		{"anthropic/claude-sonnet-4-5", "anthropic"},
	} {
		t.Run(test.model, func(t *testing.T) {
			data := &WorkflowData{Name: "LangGraph", Model: test.model, EngineConfig: &EngineConfig{Version: def.Version}}
			steps := engine.GetExecutionSteps(data, "/tmp/gh-aw/agent-stdio.log")
			require.NotEmpty(t, steps)
			execution := strings.Join(steps[len(steps)-1], "\n")
			assert.Contains(t, execution, "GH_AW_LANGGRAPH_SELECTED_MODEL: "+test.model)
			assert.Contains(t, execution, "GH_AW_LLM_PROVIDER: "+test.provider)
			assert.Contains(t, execution, "AWF_REFLECT_ENABLED: 1")
			assert.Contains(t, execution, "langgraph_harness.cjs")
			assert.Contains(t, execution, "StateGraph(MessagesState)")
		})
	}
}

func langGraphHarnessCommand(t *testing.T, dir string, args ...string) *exec.Cmd {
	t.Helper()
	harness := filepath.Join(dir, "langgraph_harness.cjs")
	require.NoError(t, os.WriteFile(harness, []byte(loadLangGraphSample(t).Behaviors.HarnessScript), 0o600))
	reflectPath, err := filepath.Abs("../../actions/setup/js/awf_reflect.cjs")
	require.NoError(t, err)
	encodedPath, err := json.Marshal(reflectPath)
	require.NoError(t, err)
	source := "module.exports = { ...require(" + string(encodedPath) + `),
fetchAWFReflect: async () => JSON.parse(process.env.LANGGRAPH_TEST_REFLECT) };`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "awf_reflect.cjs"), []byte(source), 0o600))
	cmd := exec.Command("node", append([]string{harness}, args...)...)
	cmd.Env = append(os.Environ(),
		"GH_AW_LANGGRAPH_SELECTED_MODEL=copilot/test-model", "GH_AW_LLM_PROVIDER=github",
		"GH_AW_MAX_TURNS=20", "AWF_REFLECT_ENABLED=1",
		"GH_AW_PROMPT="+filepath.Join(dir, "prompt with spaces.md"), "GH_AW_ENGINE_CWD=",
		"GITHUB_WORKSPACE="+dir,
		`LANGGRAPH_TEST_REFLECT={"ok":true,"reflectData":{"endpoints":[{"provider":"github","configured":true,"models_url":"http://localhost:1234/v1/models"}]}}`,
	)
	return cmd
}

func TestLangGraphHarnessRouting(t *testing.T) {
	for _, test := range []struct{ model, provider, client, mapped, base string }{
		{"copilot/claude-haiku-5.5", "github", "openai", "claude-haiku-5.5", "http://localhost:1234/v1"},
		{"openai/gpt-4o", "openai", "openai", "gpt-4o", "http://localhost:1234/v1"},
		{"codex/gpt-5", "openai", "openai", "gpt-5", "http://localhost:1234/v1"},
		{"anthropic/claude-sonnet-4-5", "anthropic", "anthropic", "claude-sonnet-4-5", "http://localhost:1234"},
	} {
		t.Run(test.model, func(t *testing.T) {
			cmd := langGraphHarnessCommand(t, t.TempDir(), "node", "-e",
				`const e=process.env;console.log(JSON.stringify({client:e.GH_AW_LANGGRAPH_CLIENT,model:e.GH_AW_LANGGRAPH_MODEL,base:e.GH_AW_LANGGRAPH_API_BASE,limit:e.GH_AW_LANGGRAPH_STEP_LIMIT,tracing:e.LANGSMITH_TRACING,args:process.argv.slice(1)}))`,
				"literal argument; not a shell command")
			reflection, err := json.Marshal(map[string]any{
				"ok": true, "reflectData": map[string]any{"endpoints": []map[string]any{
					{"provider": test.provider, "configured": true, "models_url": "http://localhost:1234/v1/models"},
				}},
			})
			require.NoError(t, err)
			cmd.Env = append(cmd.Env, "GH_AW_LANGGRAPH_SELECTED_MODEL="+test.model, "GH_AW_LLM_PROVIDER="+test.provider,
				"LANGGRAPH_TEST_REFLECT="+string(reflection))
			output, err := cmd.Output()
			require.NoError(t, err)
			var result struct {
				Client, Model, Base, Limit, Tracing string
				Args                                []string
			}
			require.NoError(t, json.Unmarshal(output, &result))
			assert.Equal(t, test.client, result.Client)
			assert.Equal(t, test.mapped, result.Model)
			assert.Equal(t, test.base, result.Base)
			assert.Equal(t, "20", result.Limit)
			assert.Equal(t, "false", result.Tracing)
			assert.Equal(t, []string{"literal argument; not a shell command"}, result.Args)
		})
	}
}

func TestLangGraphHarnessFailures(t *testing.T) {
	for _, test := range []struct{ name, env, message string }{
		{"no AWF", "AWF_REFLECT_ENABLED=0", "requires the AWF sandbox"},
		{"invalid model", "GH_AW_LANGGRAPH_SELECTED_MODEL=unqualified", "provider/model"},
		{"invalid limit", "GH_AW_MAX_TURNS=0", "positive safe integer"},
		{"unsupported provider", "GH_AW_LLM_PROVIDER=google", "supports copilot"},
		{"reflection failure", `LANGGRAPH_TEST_REFLECT={"ok":false,"reason":"unavailable"}`, "unavailable"},
		{"unconfigured provider", `LANGGRAPH_TEST_REFLECT={"ok":true,"reflectData":{"endpoints":[{"provider":"github","configured":false,"models_url":"http://localhost:1234/v1/models"}]}}`, "No configured"},
		{"authenticated proxy", "HTTP_PROXY=http://user:do-not-inherit@proxy:3128", "credential-free"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cmd := langGraphHarnessCommand(t, t.TempDir(), "node", "-e", "process.exit(0)")
			cmd.Env = append(cmd.Env, test.env)
			output, err := cmd.CombinedOutput()
			require.Error(t, err)
			assert.Contains(t, string(output), test.message)
			assert.NotContains(t, string(output), "do-not-inherit")
		})
	}

	t.Run("child exit preserved", func(t *testing.T) {
		cmd := langGraphHarnessCommand(t, t.TempDir(), "node", "-e", "process.exit(7)")
		output, err := cmd.CombinedOutput()
		var exitError *exec.ExitError
		require.ErrorAs(t, err, &exitError)
		assert.Equal(t, 7, exitError.ExitCode())
		assert.Contains(t, string(output), "exit code 7")
	})
}

func TestLangGraphHarnessEnvironment(t *testing.T) {
	cmd := langGraphHarnessCommand(t, t.TempDir(), "node", "-e", `console.log(JSON.stringify(process.env))`)
	cmd.Env = append(cmd.Env,
		"AWF_CLI_PROXY_URL=http://cli-proxy:11000",
		"GH_TOKEN=do-not-inherit", "OPENAI_API_KEY=do-not-inherit", "LANGSMITH_API_KEY=do-not-inherit",
		"FUTURE_SECRET=do-not-inherit", "PYTHONPATH=do-not-inherit")
	output, err := cmd.Output()
	require.NoError(t, err)
	var env map[string]string
	require.NoError(t, json.Unmarshal(output, &env))
	assert.Equal(t, "http://cli-proxy:11000", env["AWF_CLI_PROXY_URL"])
	for _, name := range []string{"GH_TOKEN", "OPENAI_API_KEY", "LANGSMITH_API_KEY", "FUTURE_SECRET", "PYTHONPATH"} {
		assert.NotContains(t, env, name)
	}
}

// TestLangGraphPinnedPackageSmoke installs the real hash-locked wheels and runs
// the agent graph end-to-end against a local OpenAI-compatible mock.
func TestLangGraphPinnedPackageSmoke(t *testing.T) {
	if os.Getenv("GH_AW_LANGGRAPH_REAL_SMOKE") != "1" {
		t.Skip("set GH_AW_LANGGRAPH_REAL_SMOKE=1 to install the hash-locked wheels and exercise a local mock LLM")
	}
	def := loadLangGraphSample(t)
	dir := t.TempDir()
	deps := filepath.Join(dir, "dependencies")
	hash := def.Behaviors.Execution.Env["GH_AW_LANGGRAPH_DEPS_LOCK_SHA256"]
	script, _ := langGraphBootstrapScript(t)
	bootstrap := exec.Command("python3", "-I", "-c", script)
	bootstrap.Env = append(os.Environ(), "GH_AW_ENGINE_VERSION="+def.Version,
		"GH_AW_LANGGRAPH_DEPS="+deps, "GH_AW_LANGGRAPH_DEPS_LOCK_SHA256="+hash)
	bootstrapOutput, err := bootstrap.CombinedOutput()
	require.NoError(t, err, "%s", bootstrapOutput)

	prompt := "Local LangGraph smoke: print LANGGRAPH_LOCAL_SMOKE and finish."
	require.NoError(t, os.WriteFile(filepath.Join(dir, "prompt with spaces.md"), []byte(prompt), 0o600))
	var mu sync.Mutex
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		encoded, _ := json.Marshal(request)
		mu.Lock()
		bodies = append(bodies, string(encoded))
		call := len(bodies)
		mu.Unlock()
		message := map[string]any{"role": "assistant", "content": "Done: LANGGRAPH_LOCAL_SMOKE printed."}
		finish := "stop"
		if call == 1 {
			message = map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{
				"id": "call_1", "type": "function",
				"function": map[string]string{"name": "run_shell", "arguments": `{"command":"printf 'LANGGRAPH_LOCAL_SMOKE\\n'"}`},
			}}}
			finish = "tool_calls"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": fmt.Sprintf("langgraph-%d", call), "object": "chat.completion", "created": 1, "model": "test-model",
			"choices": []any{map[string]any{"index": 0, "finish_reason": finish, "message": message}},
			"usage":   map[string]int{"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20},
		})
	}))
	defer server.Close()

	trajectory := filepath.Join(dir, "agent", "trajectory.json")
	cmd := langGraphHarnessCommand(t, dir, append([]string{"python3"}, def.Behaviors.Execution.Args...)...)
	cmd.Env = append(cmd.Env,
		"GH_AW_ENGINE_VERSION="+def.Version,
		"GH_AW_LANGGRAPH_DEPS="+deps, "GH_AW_LANGGRAPH_DEPS_LOCK_SHA256="+hash,
		"GH_AW_LANGGRAPH_TRAJECTORY="+trajectory,
		fmt.Sprintf(`LANGGRAPH_TEST_REFLECT={"ok":true,"reflectData":{"endpoints":[{"provider":"github","configured":true,"models_url":"%s/v1/models"}]}}`, server.URL))
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	assert.Contains(t, string(output), "LangGraph exit status: Completed")

	mu.Lock()
	require.Len(t, bodies, 2)
	assert.Contains(t, bodies[0], prompt)
	assert.Contains(t, bodies[0], `"run_shell"`)
	assert.Contains(t, bodies[1], "LANGGRAPH_LOCAL_SMOKE")
	mu.Unlock()

	info, err := os.Stat(trajectory)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	contents, err := os.ReadFile(trajectory)
	require.NoError(t, err)
	assert.Contains(t, string(contents), `"exit_status": "Completed"`)
	assert.Contains(t, string(contents), "exit code: 0")

	t.Run("max turns enforced", func(t *testing.T) {
		mu.Lock()
		bodies = nil
		mu.Unlock()
		cmd := langGraphHarnessCommand(t, dir, append([]string{"python3"}, def.Behaviors.Execution.Args...)...)
		cmd.Env = append(cmd.Env,
			"GH_AW_MAX_TURNS=1",
			"GH_AW_ENGINE_VERSION="+def.Version,
			"GH_AW_LANGGRAPH_DEPS="+deps, "GH_AW_LANGGRAPH_DEPS_LOCK_SHA256="+hash,
			"GH_AW_LANGGRAPH_TRAJECTORY="+trajectory,
			fmt.Sprintf(`LANGGRAPH_TEST_REFLECT={"ok":true,"reflectData":{"endpoints":[{"provider":"github","configured":true,"models_url":"%s/v1/models"}]}}`, server.URL))
		output, err := cmd.CombinedOutput()
		require.Error(t, err, "%s", output)
		assert.Contains(t, string(output), "LangGraph exit status: LimitsExceeded")
		mu.Lock()
		assert.Len(t, bodies, 1)
		mu.Unlock()
	})
}
