//go:build !integration

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

func atomicEnvironmentFixture(t *testing.T, command string, args ...string) (*exec.Cmd, string) {
	t.Helper()
	dir, err := os.MkdirTemp(".", ".atomic-environment-")
	require.NoError(t, err)
	dir, err = filepath.Abs(dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(dir)) })
	cmd := atomicHarnessCommand(t, dir, command, args...)
	// Keep the harness's fixed runtime scratch root inside the local fixture.
	script := strings.ReplaceAll(loadAtomicSample(t).Behaviors.HarnessScript, "/tmp/gh-aw/agent", filepath.Join(dir, "agent"))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "atomic_harness.cjs"), []byte(script), 0o600))
	return cmd, dir
}

func TestAtomicHarnessEnvironmentAllowlist(t *testing.T) {
	cmd, dir := atomicEnvironmentFixture(t, "node")
	probe := filepath.Join(dir, "probe.cjs")
	require.NoError(t, os.WriteFile(probe, []byte(`
const fs = require("fs"), path = require("path"), { spawnSync } = require("child_process");
const prompt = fs.readFileSync(0, "utf8");
const file = path.join(process.cwd(), "readback.txt");
fs.writeFileSync(file, "readback verified");
const shell = spawnSync("bash", ["-c", 'test "$ENGINE_CONFORMANCE_SENTINEL" = conformance-atomic && test -z "$ARBITRARY_REPOSITORY_SECRET" && printf "build verified"'], { encoding: "utf8" });
if (shell.error) throw shell.error;
if (shell.status !== 0) process.exit(shell.status || 1);
console.log(JSON.stringify({
  env: process.env, prompt, args: process.argv.slice(2),
  readback: fs.readFileSync(file, "utf8"), build: shell.stdout,
  mcp: JSON.parse(fs.readFileSync(path.join(process.env.ATOMIC_CODING_AGENT_DIR, "mcp.json"), "utf8"))
}));`), 0o600))
	cmd.Args = append(cmd.Args, probe, "--print", "--tools", "read,bash,edit,write,mcp__*")
	retained := map[string]string{
		"PATH": os.Getenv("PATH"), "HOME": dir, "TMPDIR": dir, "TMP": dir, "TEMP": dir,
		"LANG": "C", "LC_ALL": "C", "LC_CTYPE": "C", "TZ": "UTC", "TERM": "dumb",
		"NO_COLOR": "1", "CI": "true", "HOSTALIASES": filepath.Join(dir, "host-aliases"),
		"GITHUB_WORKSPACE": dir, "GH_AW_ENGINE_CWD": dir, "GH_AW_ENGINE_VERSION": loadAtomicSample(t).Version,
		"GH_AW_MCP_CONFIG": filepath.Join(dir, "mcp.json"), "GH_AW_TOOL_TIMEOUT": "30",
		"GH_AW_SAFE_OUTPUTS":             filepath.Join(dir, "safe-outputs.json"),
		"GH_AW_SAFE_OUTPUTS_CONFIG_PATH": filepath.Join(dir, "safe-outputs-config.json"),
		"GH_AW_SAFE_OUTPUTS_TOOLS_PATH":  filepath.Join(dir, "safe-outputs-tools.json"),
		"HTTP_PROXY":                     "http://squid:3128", "HTTPS_PROXY": "http://squid:3128",
		"ALL_PROXY": "http://squid:3128", "http_proxy": "http://squid:3128",
		"https_proxy": "http://squid:3128", "all_proxy": "http://squid:3128",
		"NO_PROXY": "localhost,api-proxy", "no_proxy": "localhost,api-proxy",
		"ENGINE_CONFORMANCE_SENTINEL": "conformance-atomic",
	}
	for name, value := range retained {
		cmd.Env = append(cmd.Env, name+"="+value)
	}
	omitted := []string{
		"ARBITRARY_REPOSITORY_SECRET", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY",
		"AZURE_CLIENT_SECRET", "GOOGLE_APPLICATION_CREDENTIALS", "GH_AW_FUTURE_CREDENTIAL",
		"GH_AW_GITHUB_MCP_SERVER_TOKEN", "GITHUB_MCP_SERVER_TOKEN", "GH_AW_MCP_SCRIPTS_API_KEY",
		"GH_AW_DEFAULT_OTLP_HEADERS", "OTEL_EXPORTER_OTLP_HEADERS", "CUSTOM_WORKFLOW_VARIABLE",
		"COPILOT_GITHUB_TOKEN", "OPENAI_API_KEY", "CODEX_API_KEY", "ANTHROPIC_API_KEY",
		"GITHUB_TOKEN", "GH_TOKEN", "GH_AW_GITHUB_TOKEN", "GH_AW_AGENT_TOKEN",
		"NODE_PATH", "BASH_ENV", "ENV", "LD_PRELOAD",
	}
	for _, name := range omitted {
		cmd.Env = append(cmd.Env, name+"=sentinel-not-for-agent")
	}
	cmd.Env = append(cmd.Env, "NODE_OPTIONS=--no-warnings")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(`{
  "autoEnableCodemode": false,
  "mcpServers": {"fixture": {"type":"http","url":"http://localhost:8080/mcp","exposure":"direct","headers":{"Authorization":"gateway-session"}}}
}`), 0o600))
	var stderr strings.Builder
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	require.NoError(t, err, "%s", stderr.String())
	var captured struct {
		Env      map[string]string `json:"env"`
		Prompt   string            `json:"prompt"`
		Args     []string          `json:"args"`
		Readback string            `json:"readback"`
		Build    string            `json:"build"`
		MCP      struct {
			Servers map[string]struct {
				Type     string            `json:"type"`
				Exposure string            `json:"exposure"`
				Headers  map[string]string `json:"headers"`
			} `json:"mcpServers"`
		} `json:"mcp"`
	}
	require.NoError(t, json.Unmarshal(output, &captured))
	for name, value := range retained {
		assert.Equal(t, value, captured.Env[name], name)
	}
	for _, name := range append(omitted, "NODE_OPTIONS", "ATOMIC_TEST_REFLECT", "AWF_REFLECT_ENABLED", "GH_AW_PROMPT", "GH_AW_LLM_PROVIDER", "ATOMIC_MODEL", "PWD") {
		assert.NotContains(t, captured.Env, name)
	}
	assert.NotContains(t, string(output), "sentinel-not-for-agent")
	assert.Equal(t, "--help $(not-a-command)\nAtomic fixture prompt", captured.Prompt)
	assert.Equal(t, []string{"--print", "--tools", "read,bash,edit,write,mcp__*", "--provider", "awf-proxy", "--model", "fixture-model"}, captured.Args)
	assert.Equal(t, "readback verified", captured.Readback)
	assert.Equal(t, "build verified", captured.Build)
	assert.Equal(t, "http", captured.MCP.Servers["fixture"].Type)
	assert.Equal(t, "direct", captured.MCP.Servers["fixture"].Exposure)
	assert.Equal(t, "[REDACTED]", captured.MCP.Servers["fixture"].Headers["Authorization"])
	assert.Equal(t, "1", captured.Env["ATOMIC_OFFLINE"])
	assert.Equal(t, "0", captured.Env["ATOMIC_TELEMETRY"])
	_, err = os.Stat(captured.Env["ATOMIC_CODING_AGENT_DIR"])
	assert.True(t, os.IsNotExist(err), "private configuration must be removed")
}

func TestAtomicHarnessEnvironmentConformanceSentinel(t *testing.T) {
	for _, tt := range []struct {
		name, value string
		set, reject bool
	}{
		{name: "absent"},
		{name: "empty", set: true},
		{name: "accepted", value: "conformance-atomic", set: true},
		{name: "other engine", value: "conformance-copilot", set: true, reject: true},
		{name: "arbitrary value", value: "sentinel-secret", set: true, reject: true},
		{name: "whitespace", value: " conformance-atomic", set: true, reject: true},
		{name: "newline", value: "conformance-atomic\n", set: true, reject: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd, dir := atomicEnvironmentFixture(t, "node", "-e", `
const fs = require("fs"), { spawnSync } = require("child_process");
fs.writeFileSync("spawned", "yes");
const shell = spawnSync("bash", ["-c", 'printf "%s" "$ENGINE_CONFORMANCE_SENTINEL"'], { encoding: "utf8" });
if (shell.error) throw shell.error;
if (shell.status !== 0) process.exit(shell.status || 1);
console.log(JSON.stringify({ child: process.env.ENGINE_CONFORMANCE_SENTINEL, shell: shell.stdout }));
`, "--")
			cmd.Env = append(cmd.Env, "HOME="+dir, "TMPDIR="+dir)
			if tt.set {
				cmd.Env = append(cmd.Env, "ENGINE_CONFORMANCE_SENTINEL="+tt.value)
			}
			var stderr strings.Builder
			cmd.Stderr = &stderr
			output, err := cmd.Output()
			if tt.reject {
				require.Error(t, err)
				assert.Contains(t, stderr.String(), "ENGINE_CONFORMANCE_SENTINEL must be conformance-atomic when set")
				assert.NotContains(t, stderr.String(), "sentinel-secret", "rejected values must not be logged")
				_, err = os.Stat(filepath.Join(dir, "spawned"))
				assert.True(t, os.IsNotExist(err), "invalid sentinel must fail before spawning")
				entries, err := os.ReadDir(filepath.Join(dir, "agent"))
				require.NoError(t, err)
				assert.Empty(t, entries, "private configuration must be removed on rejection")
				return
			}
			require.NoError(t, err, "%s", stderr.String())
			var captured map[string]string
			require.NoError(t, json.Unmarshal(output, &captured))
			assert.Equal(t, tt.value, captured["shell"])
			if tt.value == "" {
				assert.NotContains(t, captured, "child")
			} else {
				assert.Equal(t, tt.value, captured["child"])
			}
		})
	}
}

func TestAtomicHarnessEnvironmentRejectsUnsafeProxy(t *testing.T) {
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		for _, value := range []string{
			"http://user:sentinel-secret@squid:3128", "http://squid:3128/?key=sentinel-secret",
			"http://squid:3128/sentinel-secret", "http://squid:3128/#sentinel-secret",
			"socks5://squid:3128", "not-a-url",
		} {
			t.Run(name+"/"+value, func(t *testing.T) {
				cmd, dir := atomicEnvironmentFixture(t, "node", "-e", `require("fs").writeFileSync("spawned", "bad")`)
				cmd.Env = append(cmd.Env, name+"="+value)
				output, err := cmd.CombinedOutput()
				require.Error(t, err)
				assert.Contains(t, string(output), name+" must be a credential-free HTTP proxy URL")
				assert.NotContains(t, string(output), "sentinel-secret")
				_, err = os.Stat(filepath.Join(dir, "spawned"))
				assert.True(t, os.IsNotExist(err), "invalid proxy must fail before spawning")
				entries, err := os.ReadDir(filepath.Join(dir, "agent"))
				require.NoError(t, err)
				assert.Empty(t, entries, "private configuration must be removed on rejection")
			})
		}
	}
}

func TestAtomicHarnessEnvironmentPublishedCLI(t *testing.T) {
	command := os.Getenv("GH_AW_ATOMIC_TEST_CLI")
	if command == "" {
		t.Skip("set GH_AW_ATOMIC_TEST_CLI to validate the pinned CLI with the allowlisted environment")
	}
	cmd, dir := atomicEnvironmentFixture(t, command, "--version")
	cmd.Env = append(cmd.Env, "HOME="+dir, "TMPDIR="+dir, "ARBITRARY_REPOSITORY_SECRET=sentinel-not-for-agent")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	assert.Contains(t, string(output), loadAtomicSample(t).Version)
	assert.NotContains(t, string(output), "sentinel-not-for-agent")
}
