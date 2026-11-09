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
	"strings"
	"sync"
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

func miniSweAgentLockHash(t *testing.T) string {
	t.Helper()
	hash := loadMiniSweAgentSample(t).Behaviors.Execution.Env["GH_AW_MINI_DEPS_LOCK_SHA256"]
	require.Len(t, hash, 64)
	return hash
}

func miniSweAgentLockHashFor(contents []byte) string {
	hash := sha256.Sum256(contents)
	return hex.EncodeToString(hash[:])
}

func miniSweAgentBootstrapScript(t *testing.T) string {
	t.Helper()
	var frontmatter struct {
		Steps []struct {
			Run string            `yaml:"run"`
			Env map[string]string `yaml:"env"`
		} `yaml:"pre-agent-steps"`
	}
	readConformanceFrontmatter(t, "../../.github/workflows/shared/mini-swe-agent.md", &frontmatter)
	require.Len(t, frontmatter.Steps, 1)
	require.Equal(t, miniSweAgentLockHash(t), frontmatter.Steps[0].Env["GH_AW_MINI_DEPS_LOCK_SHA256"])
	run := frontmatter.Steps[0].Run
	require.True(t, strings.HasPrefix(run, "python3 -I - <<'PY'\n"))
	require.True(t, strings.HasSuffix(run, "\nPY\n"))
	return strings.TrimSuffix(strings.TrimPrefix(run, "python3 -I - <<'PY'\n"), "\nPY\n")
}

func miniSweAgentBootstrapCommand(t *testing.T, dir string) *exec.Cmd {
	t.Helper()
	script := filepath.Join(dir, "bootstrap.py")
	require.NoError(t, os.WriteFile(script, []byte(miniSweAgentBootstrapScript(t)), 0o600))
	cmd := exec.Command("python3", "-I", "-c", miniSweAgentBootstrapStubs, script)
	cmd.Env = append(os.Environ(), "GH_AW_ENGINE_VERSION="+loadMiniSweAgentSample(t).Version,
		"GH_AW_MINI_DEPS="+filepath.Join(dir, "dependencies"),
		"GH_AW_MINI_DEPS_LOCK_SHA256="+miniSweAgentLockHash(t))
	return cmd
}

func stageMiniSweAgentTestLock(t *testing.T, dir string) {
	t.Helper()
	output, err := miniSweAgentBootstrapCommand(t, dir).CombinedOutput()
	require.NoError(t, err, "%s", output)
	assert.Contains(t, string(output), "Hash-locked wheel staging verified")
}

func TestMiniSweAgentBootstrap(t *testing.T) {
	stageMiniSweAgentTestLock(t, t.TempDir())
	for _, test := range []struct{ name, env, message string }{
		{"unlocked version", "GH_AW_ENGINE_VERSION=0.0.0", "version must match the dependency lock"},
		{"wrong Python", "MINI_TEST_PYTHON=3.11", "requires Linux x86-64 and Python 3.12"},
		{"wrong platform", "MINI_TEST_PLATFORM=darwin", "requires Linux x86-64 and Python 3.12"},
		{"wrong architecture", "MINI_TEST_ARCH=aarch64", "requires Linux x86-64 and Python 3.12"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cmd := miniSweAgentBootstrapCommand(t, t.TempDir())
			cmd.Env = append(cmd.Env, test.env)
			output, err := cmd.CombinedOutput()
			require.Error(t, err)
			assert.Contains(t, string(output), test.message)
		})
	}
}

const miniSweAgentBootstrapStubs = `
import os
import sys
from pathlib import Path
from unittest.mock import patch

with (
    patch("subprocess.run") as download,
    patch("sys.version_info", tuple(map(int, os.environ.get("MINI_TEST_PYTHON", "3.12").split(".")))),
    patch("sys.platform", os.environ.get("MINI_TEST_PLATFORM", "linux")),
    patch("platform.machine", return_value=os.environ.get("MINI_TEST_ARCH", "x86_64")),
):
    exec(compile(Path(sys.argv[1]).read_text(), sys.argv[1], "exec"))
    dependencies = Path(os.environ["GH_AW_MINI_DEPS"])
    lock = dependencies / "requirements.lock"
    download.assert_called_once_with([
        sys.executable, "-I", "-m", "pip", "download", "--quiet",
        "--isolated", "--disable-pip-version-check", "--only-binary=:all:",
        "--require-hashes", "--dest", str(dependencies / "wheels"),
        "--requirement", str(lock),
    ], check=True)
    requirements = lock.read_text().strip().splitlines()
    assert len(requirements) > 80
    assert all("==" in line and " --hash=sha256:" in line for line in requirements)
    assert not list(dependencies.glob("*.dist-info"))
    assert sys.flags.isolated
    print("Hash-locked wheel staging verified")
`

func TestMiniSweAgentSampleExecution(t *testing.T) {
	def := loadMiniSweAgentSample(t)
	engine, err := NewBehaviorDefinedEngine(def)
	require.NoError(t, err)
	assert.True(t, def.Experimental)
	assert.False(t, engine.GetCapabilities().MCP)
	assert.True(t, engine.GetCapabilities().MaxTurns)
	for _, step := range engine.GetInstallationSteps(&WorkflowData{}) {
		assert.NotContains(t, strings.Join(step, "\n"), "pip install")
		assert.NotContains(t, strings.Join(step, "\n"), "minisweagent")
	}
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
			stageMiniSweAgentTestLock(t, dir)
			prompt := filepath.Join(dir, "prompt with spaces.md")
			require.NoError(t, os.WriteFile(prompt, []byte("literal task --- with file-based input"), 0o600))
			script := filepath.Join(dir, "mini.py")
			require.NoError(t, os.WriteFile(script, []byte(args[2]), 0o600))
			cmd := miniSweAgentHarnessCommand(t, dir, "python3", "-I", "-c", miniSweAgentPythonStubs, script)
			cmd.Args = append(cmd.Args, status)
			cmd.Env = append(cmd.Env, "GH_AW_ENGINE_VERSION="+def.Version,
				"GH_AW_MINI_DEPS="+filepath.Join(dir, "dependencies"),
				"GH_AW_MINI_DEPS_LOCK_SHA256="+miniSweAgentLockHash(t),
				"GH_AW_MINI_TRAJECTORY="+filepath.Join(dir, "agent", "trajectory.json"))
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
agent.run.return_value = {"exit_status": sys.argv[2]}
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
with (
    patch("subprocess.run") as install,
    patch("importlib.metadata.version") as version,
    patch("sys.version_info", (3, 12)),
    patch("sys.platform", "linux"),
    patch("platform.machine", return_value="x86_64"),
):
    version.return_value = os.environ["GH_AW_ENGINE_VERSION"]
    try:
        exec(compile(Path(sys.argv[1]).read_text(), sys.argv[1], "exec"))
    except SystemExit as error:
        exit_code = error.code
    install.assert_called_once_with([
        sys.executable, "-I", "-m", "pip", "install", "--quiet",
        "--isolated", "--disable-pip-version-check", "--only-binary=:all:",
        "--require-hashes", "--no-index", "--find-links", str(Path(os.environ["GH_AW_MINI_DEPS"]) / "wheels"),
        "--target", os.environ["GH_AW_MINI_DEPS"],
        "--requirement", str(Path(os.environ["GH_AW_MINI_DEPS"]) / "requirements.lock"),
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

func TestMiniSweAgentHarnessEnvironment(t *testing.T) {
	dir := t.TempDir()
	cmd := miniSweAgentHarnessCommand(t, dir, "node", "-e", `console.log(JSON.stringify(process.env))`)
	cmd.Env = append(cmd.Env,
		"PATH="+os.Getenv("PATH"),
		"GH_AW_MCP_DEFERRED_SERVERS=work-queue",
		"GH_AW_SAFE_OUTPUTS_TOOLS_PATH="+filepath.Join(dir, "tools.json"),
		"ENGINE_CONFORMANCE_SENTINEL=conformance-mini-swe-agent",
		"AWF_CLI_PROXY_URL=http://cli-proxy:11000",
		"HTTP_PROXY=http://squid-proxy:3128", "HTTPS_PROXY=http://squid-proxy:3128",
		"NO_PROXY=api-proxy,cli-proxy", "no_proxy=api-proxy,cli-proxy",
		"GH_AW_GITHUB_MCP_SERVER_TOKEN=do-not-inherit",
		"GITHUB_MCP_SERVER_TOKEN=do-not-inherit",
		"GH_TOKEN=do-not-inherit", "OPENAI_API_KEY=do-not-inherit",
		"FUTURE_SECRET=do-not-inherit", "PYTHONPATH=do-not-inherit",
		"NODE_OPTIONS=--no-warnings", "BASH_ENV=do-not-inherit")
	output, err := cmd.Output()
	require.NoError(t, err)
	var env map[string]string
	require.NoError(t, json.Unmarshal(output, &env))
	assert.Equal(t, os.Getenv("PATH"), env["PATH"])
	assert.Equal(t, dir, env["GITHUB_WORKSPACE"])
	assert.Equal(t, "work-queue", env["GH_AW_MCP_DEFERRED_SERVERS"])
	assert.Equal(t, filepath.Join(dir, "tools.json"), env["GH_AW_SAFE_OUTPUTS_TOOLS_PATH"])
	assert.Equal(t, "conformance-mini-swe-agent", env["ENGINE_CONFORMANCE_SENTINEL"])
	assert.Equal(t, "http://cli-proxy:11000", env["AWF_CLI_PROXY_URL"])
	assert.Equal(t, "http://squid-proxy:3128", env["HTTP_PROXY"])
	assert.Equal(t, "http://squid-proxy:3128", env["HTTPS_PROXY"])
	assert.Equal(t, "api-proxy,cli-proxy", env["NO_PROXY"])
	assert.Equal(t, "api-proxy,cli-proxy", env["no_proxy"])
	for _, name := range []string{
		"GH_AW_GITHUB_MCP_SERVER_TOKEN", "GITHUB_MCP_SERVER_TOKEN", "GH_TOKEN",
		"OPENAI_API_KEY", "FUTURE_SECRET", "PYTHONPATH", "NODE_OPTIONS", "BASH_ENV",
	} {
		assert.NotContains(t, env, name)
	}
	t.Run("authenticated proxy rejected", func(t *testing.T) {
		cmd := miniSweAgentHarnessCommand(t, t.TempDir(), "node", "-e", "process.exit(0)")
		cmd.Env = append(cmd.Env, "HTTP_PROXY=http://user:do-not-inherit@proxy:3128")
		output, err := cmd.CombinedOutput()
		require.Error(t, err)
		assert.Contains(t, string(output), "HTTP_PROXY must be a credential-free HTTP(S) URL")
		assert.NotContains(t, string(output), "do-not-inherit")
	})
}

func TestMiniSweAgentPythonRejectsUnlockedVersion(t *testing.T) {
	def := loadMiniSweAgentSample(t)
	dir := t.TempDir()
	stageMiniSweAgentTestLock(t, dir)
	cmd := miniSweAgentHarnessCommand(t, dir, append([]string{"python3"}, def.Behaviors.Execution.Args...)...)
	cmd.Env = append(cmd.Env,
		"GH_AW_ENGINE_VERSION=0.0.0",
		"GH_AW_MINI_DEPS="+filepath.Join(dir, "dependencies"),
		"GH_AW_MINI_DEPS_LOCK_SHA256="+miniSweAgentLockHash(t))
	output, err := cmd.CombinedOutput()
	require.Error(t, err)
	assert.Contains(t, string(output), "version must match the dependency lock")
}

func TestMiniSweAgentPythonRequiresStagedLock(t *testing.T) {
	def := loadMiniSweAgentSample(t)
	dir := t.TempDir()
	cmd := miniSweAgentHarnessCommand(t, dir, append([]string{"python3"}, def.Behaviors.Execution.Args...)...)
	cmd.Env = append(cmd.Env,
		"GH_AW_ENGINE_VERSION="+def.Version,
		"GH_AW_MINI_DEPS="+filepath.Join(dir, "dependencies"),
		"GH_AW_MINI_DEPS_LOCK_SHA256="+miniSweAgentLockHash(t))
	output, err := cmd.CombinedOutput()
	require.Error(t, err)
	assert.Contains(t, string(output), "requirements.lock")
	assert.NotContains(t, string(output), "mini-SWE-agent exit status")
}

func TestMiniSweAgentPythonRejectsTamperedLock(t *testing.T) {
	def := loadMiniSweAgentSample(t)
	dir := t.TempDir()
	stageMiniSweAgentTestLock(t, dir)
	dependencies := filepath.Join(dir, "dependencies")
	lock := filepath.Join(dependencies, "requirements.lock")
	requirements, err := os.ReadFile(lock)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(lock, append(requirements, []byte("# modified\n")...), 0o600))
	cmd := miniSweAgentHarnessCommand(t, dir, append([]string{"python3"}, def.Behaviors.Execution.Args...)...)
	cmd.Env = append(cmd.Env,
		"GH_AW_ENGINE_VERSION="+def.Version,
		"GH_AW_MINI_DEPS="+dependencies,
		"GH_AW_MINI_DEPS_LOCK_SHA256="+miniSweAgentLockHash(t))
	output, err := cmd.CombinedOutput()
	require.Error(t, err)
	assert.Contains(t, string(output), "does not match the trusted bootstrap lock")
	assert.NotContains(t, string(output), "mini-SWE-agent exit status")
}

func TestMiniSweAgentPythonRejectsTamperedWheel(t *testing.T) {
	if err := exec.Command("python3", "-I", "-c",
		`import platform, sys; sys.exit(0 if sys.version_info[:2] == (3, 12) and sys.platform == "linux" and platform.machine() == "x86_64" else 1)`).Run(); err != nil {
		if os.Getenv("GH_AW_REQUIRE_MINI_PY312") == "1" {
			t.Fatalf("Linux x86-64 and Python 3.12 are required for the offline hash-verification test: %v", err)
		}
		t.Skip("Linux x86-64 and Python 3.12 are required for the offline hash-verification test")
	}
	if err := exec.Command("python3", "-I", "-m", "pip", "--version").Run(); err != nil {
		if os.Getenv("GH_AW_REQUIRE_MINI_PY312") == "1" {
			t.Fatalf("isolated pip is required for the offline hash-verification test: %v", err)
		}
		t.Skip("isolated pip is required for the offline hash-verification test")
	}
	def := loadMiniSweAgentSample(t)
	dir := t.TempDir()
	stageMiniSweAgentTestLock(t, dir)
	dependencies := filepath.Join(dir, "dependencies")
	lock := filepath.Join(dependencies, "requirements.lock")
	requirements, err := os.ReadFile(lock)
	require.NoError(t, err)
	var miniRequirement string
	for line := range strings.SplitSeq(string(requirements), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "mini-swe-agent==") {
			miniRequirement = strings.TrimSpace(line)
		}
	}
	require.NotEmpty(t, miniRequirement)
	requirements = []byte(miniRequirement + "\n")
	require.NoError(t, os.WriteFile(lock, requirements, 0o600))
	wheels := filepath.Join(dependencies, "wheels")
	require.NoError(t, os.MkdirAll(wheels, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(wheels, "mini_swe_agent-"+def.Version+"-py3-none-any.whl"),
		[]byte("tampered wheel"), 0o600))
	cmd := miniSweAgentHarnessCommand(t, dir, append([]string{"python3"}, def.Behaviors.Execution.Args...)...)
	cmd.Env = append(cmd.Env,
		"GH_AW_ENGINE_VERSION="+def.Version,
		"GH_AW_MINI_DEPS="+dependencies,
		"GH_AW_MINI_DEPS_LOCK_SHA256="+miniSweAgentLockHashFor(requirements))
	output, err := cmd.CombinedOutput()
	require.Error(t, err)
	assert.Contains(t, string(output), "DO NOT MATCH THE HASHES")
	assert.NotContains(t, string(output), "mini-SWE-agent exit status")
}

func TestMiniSweAgentPinnedPackageSmoke(t *testing.T) {
	if os.Getenv("GH_AW_MINI_REAL_SMOKE") != "1" {
		t.Skip("set GH_AW_MINI_REAL_SMOKE=1 to install the hash-locked wheels and exercise a local mock LLM")
	}
	dir := t.TempDir()
	def := loadMiniSweAgentSample(t)
	bootstrap := exec.Command("python3", "-I", "-c", miniSweAgentBootstrapScript(t))
	bootstrap.Env = append(os.Environ(), "GH_AW_ENGINE_VERSION="+def.Version,
		"GH_AW_MINI_DEPS="+filepath.Join(dir, "dependencies"),
		"GH_AW_MINI_DEPS_LOCK_SHA256="+miniSweAgentLockHash(t))
	bootstrapOutput, bootstrapErr := bootstrap.CombinedOutput()
	require.NoError(t, bootstrapErr, "%s", bootstrapOutput)
	prompt := "Local mini-SWE-agent smoke: print MINI_LOCAL_SMOKE and submit."
	require.NoError(t, os.WriteFile(filepath.Join(dir, "prompt with spaces.md"), []byte(prompt), 0o600))
	var mu sync.Mutex
	var requests []string
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var request struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		for _, message := range request.Messages {
			requests = append(requests, message.Content)
		}
		calls++
		content := "THOUGHT: Execute the local smoke marker.\n```mswea_bash_command\nprintf 'MINI_LOCAL_SMOKE\\n'\n```"
		if calls > 1 {
			content = "THOUGHT: Submit the completed local smoke.\n```mswea_bash_command\necho COMPLETE_TASK_AND_SUBMIT_FINAL_OUTPUT\n```"
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "mini-local-smoke", "object": "chat.completion", "created": 1, "model": "test-model",
			"choices": []any{map[string]any{
				"index": 0, "finish_reason": "stop",
				"message": map[string]string{"role": "assistant", "content": content},
			}},
			"usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20},
		})
	}))
	defer server.Close()
	cmd := miniSweAgentHarnessCommand(t, dir, append([]string{"python3"}, def.Behaviors.Execution.Args...)...)
	cmd.Env = append(cmd.Env,
		"GH_AW_ENGINE_VERSION="+def.Version,
		"GH_AW_MINI_DEPS="+filepath.Join(dir, "dependencies"),
		"GH_AW_MINI_DEPS_LOCK_SHA256="+miniSweAgentLockHash(t),
		"GH_AW_MINI_TRAJECTORY="+filepath.Join(dir, "trajectory.json"),
		fmt.Sprintf(`MINI_TEST_REFLECT={"ok":true,"reflectData":{"endpoints":[{"provider":"github","configured":true,"models_url":"%s/v1/models"}]}}`, server.URL))
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	assert.Contains(t, string(output), "mini-SWE-agent exit status: Submitted")
	mu.Lock()
	assert.Contains(t, strings.Join(requests, "\n"), prompt)
	assert.Equal(t, 2, calls)
	mu.Unlock()
	trajectory, err := os.ReadFile(filepath.Join(dir, "trajectory.json"))
	require.NoError(t, err)
	assert.Contains(t, string(trajectory), "MINI_LOCAL_SMOKE")
}
