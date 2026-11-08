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
)

func loadMiniSweAgentSample(t *testing.T) *EngineDefinition {
	t.Helper()
	var frontmatter struct {
		Engine EngineDefinition `yaml:"engine"`
	}
	readConformanceFrontmatter(t, "../../.github/workflows/shared/mini-swe-agent.md", &frontmatter)
	return &frontmatter.Engine
}

func TestMiniSweAgentSampleExecution(t *testing.T) {
	def := loadMiniSweAgentSample(t)
	engine, err := NewBehaviorDefinedEngine(def)
	require.NoError(t, err)
	assert.True(t, def.Experimental)
	assert.False(t, engine.GetCapabilities().MCP)
	assert.True(t, engine.GetCapabilities().MaxTurns)
	for _, test := range []struct{ model, provider string }{
		{"copilot/claude-haiku-5.5", "github"},
		{"openai/gpt-4o", "openai"},
		{"codex/gpt-5", "openai"},
		{"anthropic/claude-sonnet-4-5", "anthropic"},
	} {
		t.Run(test.model, func(t *testing.T) {
			data := &WorkflowData{Name: "mini-SWE-agent", Model: test.model, EngineConfig: &EngineConfig{Version: def.Version}}
			steps := engine.GetExecutionSteps(data, "/tmp/gh-aw/agent-stdio.log")
			require.NotEmpty(t, steps)
			execution := strings.Join(steps[len(steps)-1], "\n")
			assert.Contains(t, execution, "GH_AW_MINI_MODEL: "+test.model)
			assert.Contains(t, execution, "GH_AW_LLM_PROVIDER: "+test.provider)
			assert.Contains(t, execution, "AWF_REFLECT_ENABLED: 1")
			assert.Contains(t, execution, "mini-swe-agent_harness.cjs")
		})
	}
}

func miniSweAgentHarnessCommand(t *testing.T, dir string, args ...string) *exec.Cmd {
	t.Helper()
	harness := filepath.Join(dir, "mini-swe-agent_harness.cjs")
	require.NoError(t, os.WriteFile(harness, []byte(loadMiniSweAgentSample(t).Behaviors.HarnessScript), 0o600))
	reflectPath, err := filepath.Abs("../../actions/setup/js/awf_reflect.cjs")
	require.NoError(t, err)
	encodedPath, err := json.Marshal(reflectPath)
	require.NoError(t, err)
	source := "module.exports = { ...require(" + string(encodedPath) + `),
fetchAWFReflect: async () => JSON.parse(process.env.MINI_TEST_REFLECT) };`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "awf_reflect.cjs"), []byte(source), 0o600))
	cmd := exec.Command("node", append([]string{harness}, args...)...)
	cmd.Env = append(os.Environ(),
		"GH_AW_MINI_MODEL=copilot/test-model", "GH_AW_LLM_PROVIDER=github",
		"GH_AW_MAX_TURNS=20", "AWF_REFLECT_ENABLED=1",
		"GH_AW_PROMPT="+filepath.Join(dir, "prompt with spaces.md"), "GH_AW_ENGINE_CWD="+dir,
		"GITHUB_WORKSPACE="+dir,
		`MINI_TEST_REFLECT={"ok":true,"reflectData":{"endpoints":[{"provider":"github","configured":true,"models_url":"http://localhost:1234/v1/models"}]}}`,
	)
	return cmd
}

func TestMiniSweAgentHarnessRouting(t *testing.T) {
	for _, test := range []struct{ model, provider, mapped, base string }{
		{"copilot/claude-haiku-5.5", "github", "openai/claude-haiku-5.5", "http://localhost:1234/v1"},
		{"openai/gpt-4o", "openai", "openai/gpt-4o", "http://localhost:1234/v1"},
		{"codex/gpt-5", "openai", "openai/gpt-5", "http://localhost:1234/v1"},
		{"anthropic/claude-sonnet-4-5", "anthropic", "anthropic/claude-sonnet-4-5", "http://localhost:1234"},
	} {
		t.Run(test.model, func(t *testing.T) {
			dir := t.TempDir()
			cmd := miniSweAgentHarnessCommand(t, dir, "node", "-e",
				`console.log(JSON.stringify({model:process.env.GH_AW_MINI_LITELLM_MODEL,base:process.env.GH_AW_MINI_API_BASE,limit:process.env.GH_AW_MINI_STEP_LIMIT,args:process.argv.slice(1)}))`,
				"literal argument; not a shell command")
			reflection, err := json.Marshal(map[string]any{
				"ok": true, "reflectData": map[string]any{"endpoints": []map[string]any{
					{"provider": test.provider, "configured": true, "models_url": "http://localhost:1234/v1/models"},
				}},
			})
			require.NoError(t, err)
			cmd.Env = append(cmd.Env, "GH_AW_MINI_MODEL="+test.model, "GH_AW_LLM_PROVIDER="+test.provider,
				"MINI_TEST_REFLECT="+string(reflection))
			output, err := cmd.Output()
			require.NoError(t, err)
			var result struct {
				Model string
				Base  string
				Limit string
				Args  []string
			}
			require.NoError(t, json.Unmarshal(output, &result))
			assert.Equal(t, test.mapped, result.Model)
			assert.Equal(t, test.base, result.Base)
			assert.Equal(t, "20", result.Limit)
			assert.Equal(t, []string{"literal argument; not a shell command"}, result.Args)
		})
	}
}

func TestMiniSweAgentHarnessFailures(t *testing.T) {
	for _, test := range []struct{ name, env, message string }{
		{"no AWF", "AWF_REFLECT_ENABLED=0", "requires the AWF sandbox"},
		{"invalid model", "GH_AW_MINI_MODEL=unqualified", "provider/model"},
		{"invalid limit", "GH_AW_MAX_TURNS=0", "positive safe integer"},
		{"unsupported provider", "GH_AW_LLM_PROVIDER=google", "supports copilot"},
		{"reflection failure", `MINI_TEST_REFLECT={"ok":false,"reason":"unavailable"}`, "unavailable"},
		{"wrong provider", `MINI_TEST_REFLECT={"ok":true,"reflectData":{"endpoints":[{"provider":"openai","configured":true,"models_url":"http://localhost:1234/v1/models"}]}}`, "No configured"},
		{"unconfigured provider", `MINI_TEST_REFLECT={"ok":true,"reflectData":{"endpoints":[{"provider":"github","configured":false,"models_url":"http://localhost:1234/v1/models"}]}}`, "No configured"},
		{"missing models URL", `MINI_TEST_REFLECT={"ok":true,"reflectData":{"endpoints":[{"provider":"github","configured":true}]}}`, "No configured"},
		{"invalid models URL", `MINI_TEST_REFLECT={"ok":true,"reflectData":{"endpoints":[{"provider":"github","configured":true,"models_url":"not a URL"}]}}`, "Invalid models URL"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cmd := miniSweAgentHarnessCommand(t, t.TempDir(), "node", "-e", "process.exit(0)")
			cmd.Env = append(cmd.Env, test.env)
			output, err := cmd.CombinedOutput()
			require.Error(t, err)
			assert.Contains(t, string(output), test.message)
		})
	}

	t.Run("child exit preserved", func(t *testing.T) {
		cmd := miniSweAgentHarnessCommand(t, t.TempDir(), "node", "-e", "process.exit(7)")
		output, err := cmd.CombinedOutput()
		var exitError *exec.ExitError
		require.ErrorAs(t, err, &exitError)
		assert.Equal(t, 7, exitError.ExitCode())
		assert.Contains(t, string(output), "exit code 7")
	})
}

func TestMiniSweAgentHarnessWorkingDirectory(t *testing.T) {
	for _, cwd := range []string{"", "subdirectory"} {
		t.Run(cwd, func(t *testing.T) {
			dir := t.TempDir()
			expected := filepath.Join(dir, cwd)
			require.NoError(t, os.MkdirAll(expected, 0o700))
			cmd := miniSweAgentHarnessCommand(t, dir, "node", "-e",
				`console.log(process.cwd()); console.log(process.env.GH_AW_ENGINE_CWD)`)
			cmd.Env = append(cmd.Env, "GH_AW_ENGINE_CWD="+cwd)
			output, err := cmd.Output()
			require.NoError(t, err)
			assert.Equal(t, expected+"\n"+expected+"\n", string(output))
		})
	}
}

func TestMiniSweAgentHarnessDefaultTurnLimit(t *testing.T) {
	cmd := miniSweAgentHarnessCommand(t, t.TempDir(), "node", "-e", `console.log(process.env.GH_AW_MINI_STEP_LIMIT)`)
	cmd.Env = append(cmd.Env, "GH_AW_MAX_TURNS=")
	output, err := cmd.Output()
	require.NoError(t, err)
	assert.Equal(t, "20\n", string(output))
}

func TestMiniSweAgentPythonExecution(t *testing.T) {
	def := loadMiniSweAgentSample(t)
	args := def.Behaviors.Execution.Args
	require.Len(t, args, 3)
	require.Equal(t, []string{"-I", "-c"}, args[:2])
	for _, status := range []string{"Submitted", "LimitsExceeded", "unknown"} {
		t.Run(status, func(t *testing.T) {
			dir := t.TempDir()
			prompt := filepath.Join(dir, "prompt with spaces.md")
			require.NoError(t, os.WriteFile(prompt, []byte("literal task --- with file-based input"), 0o600))
			script := filepath.Join(dir, "mini.py")
			require.NoError(t, os.WriteFile(script, []byte(args[2]), 0o600))
			cmd := miniSweAgentHarnessCommand(t, dir, "python3", "-I", "-c", miniSweAgentPythonStubs, script)
			cmd.Env = append(cmd.Env, "GH_AW_ENGINE_VERSION="+def.Version,
				"GH_AW_MINI_DEPS="+filepath.Join(dir, "dependencies"),
				"GH_AW_MINI_TRAJECTORY="+filepath.Join(dir, "agent", "trajectory.json"),
				"MINI_TEST_STATUS="+status)
			output, err := cmd.CombinedOutput()
			if status == "Submitted" {
				require.NoError(t, err, "%s", output)
			} else {
				var exitError *exec.ExitError
				require.ErrorAs(t, err, &exitError, "%s", output)
				assert.Equal(t, 1, exitError.ExitCode())
			}
			assert.Contains(t, string(output), "Python wiring verified")
			assert.Contains(t, string(output), "mini-SWE-agent exit status: "+status)
			info, err := os.Stat(filepath.Join(dir, "agent", "trajectory.json"))
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		})
	}
}

const miniSweAgentPythonStubs = `
import os
import sys
from pathlib import Path
from types import ModuleType, SimpleNamespace
from unittest.mock import Mock, patch

config = {"agent": {}, "model": {}, "environment": {}}
builtin = Path(os.environ["GITHUB_WORKSPACE"]) / "builtin"
builtin.mkdir()
(builtin / "default.yaml").write_text("fixture configuration")
model = SimpleNamespace(config=SimpleNamespace(model_kwargs={}))
model_class = Mock(return_value=model)
environment = object()
environment_class = Mock(return_value=environment)
agent = Mock()
agent.run.return_value = {"exit_status": os.environ["MINI_TEST_STATUS"]}
agent_class = Mock(return_value=agent)
yaml_load = Mock(return_value=config)

for name, attributes in {
    "yaml": {"safe_load": yaml_load},
    "minisweagent.agents.default": {"DefaultAgent": agent_class},
    "minisweagent.config": {"builtin_config_dir": builtin},
    "minisweagent.environments.local": {"LocalEnvironment": environment_class},
    "minisweagent.models.litellm_textbased_model": {"LitellmTextbasedModel": model_class},
}.items():
    module = ModuleType(name)
    module.__dict__.update(attributes)
    sys.modules[name] = module

exit_code = 0
with patch("subprocess.run") as install, patch("importlib.metadata.version") as version:
    version.return_value = os.environ["GH_AW_ENGINE_VERSION"]
    try:
        exec(compile(Path(sys.argv[1]).read_text(), sys.argv[1], "exec"))
    except SystemExit as error:
        exit_code = error.code
    install.assert_called_once_with([
        sys.executable, "-I", "-m", "pip", "install", "--quiet",
        "--disable-pip-version-check", "--only-binary=:all:",
        "--target", os.environ["GH_AW_MINI_DEPS"],
        "mini-swe-agent==" + os.environ["GH_AW_ENGINE_VERSION"],
    ], check=True)
    version.assert_called_once_with("mini-swe-agent")

assert sys.flags.isolated
assert sys.path[0] == os.environ["GH_AW_MINI_DEPS"]
yaml_load.assert_called_once_with("fixture configuration")
assert config["agent"]["step_limit"] == 20
assert config["agent"]["cost_limit"] == 0
assert config["agent"]["output_path"] == Path(os.environ["GH_AW_MINI_TRAJECTORY"])
model_class.assert_called_once_with(
    model_name="openai/test-model", cost_tracking="ignore_errors")
assert model.config.model_kwargs == {
    "api_base": "http://localhost:1234/v1", "api_key": "awf-proxy"}
environment_class.assert_called_once_with(cwd=os.environ["GITHUB_WORKSPACE"])
agent_class.assert_called_once_with(model, environment, **config["agent"])
agent.run.assert_called_once_with("literal task --- with file-based input")
print("Python wiring verified", flush=True)
raise SystemExit(exit_code)
`
