//go:build !integration && !windows

package workflow

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func agyHarnessCommand(t *testing.T, body string, reflectJSON string) (*exec.Cmd, string) {
	t.Helper()
	dir := t.TempDir()
	harness := filepath.Join(dir, "agy_harness.cjs")
	require.NoError(t, os.WriteFile(harness, agyRuntimeScript(t, "agy_harness.cjs"), 0o600))
	actionsDir, err := filepath.Abs("../../actions/setup/js")
	require.NoError(t, err)
	module, err := json.Marshal(filepath.Join(actionsDir, "awf_reflect.cjs"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "awf_reflect.cjs"), []byte("module.exports = { ...require("+string(module)+"), fetchAWFReflect: async () => JSON.parse(process.env.AGY_TEST_REFLECT) };"), 0o600))
	binary := filepath.Join(dir, "native-agy")
	fixture := "#!/usr/bin/env node\nif (process.argv.includes('--version')) { console.log('1.3.1'); process.exit(0); }\n" + body
	require.NoError(t, os.WriteFile(binary, []byte(fixture), 0o700))
	prompt := filepath.Join(dir, "prompt with spaces.txt")
	require.NoError(t, os.WriteFile(prompt, []byte("Literal $(touch SHOULD_NOT_EXIST)\n\"quoted\"; --model not-an-argument"), 0o600))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, "node", harness, binary)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "GITHUB_WORKSPACE=" + dir,
		"GH_AW_ENGINE_VERSION=1.3.1", "GH_AW_AGY_MODEL=gemini-3.8-flash-medium",
		"GH_AW_PROMPT=" + prompt, "GEMINI_API_KEY=host-test-key",
		"GOOGLE_APPLICATION_CREDENTIALS=/private/adc.json", "GOOGLE_API_KEY=other-test-key",
		"AWF_REFLECT_ENABLED=1", "AGY_TEST_REFLECT=" + reflectJSON,
	}
	return cmd, dir
}

func agyRuntimeScript(t *testing.T, name string) []byte {
	t.Helper()
	script, err := os.ReadFile(filepath.Join("../../actions/setup/js", name))
	require.NoError(t, err)
	return script
}

func TestAgyHarnessPrivateConfigurationAndLiteralInput(t *testing.T) {
	body := `const fs = require("node:fs");
let input = ""; process.stdin.setEncoding("utf8");
process.stdin.on("data", chunk => input += chunk);
process.stdin.on("end", () => {
  const settings = JSON.parse(fs.readFileSync(process.env.HOME + "/.gemini/antigravity-cli/settings.json", "utf8"));
  console.log(JSON.stringify({ event: "init", init: { model: "gemini-3.8-flash-medium", input: JSON.parse(input), args: process.argv.slice(2), settings, home: process.env.HOME, apiKey: process.env.GEMINI_API_KEY, endpoint: process.env.GOOGLE_GEMINI_BASE_URL, adc: process.env.GOOGLE_APPLICATION_CREDENTIALS, googleKey: process.env.GOOGLE_API_KEY } }));
  console.log(JSON.stringify({ event: "result", result: { status: "SUCCESS", num_turns: 1, usage: { input_tokens: 10, output_tokens: 2 } } }));
});`
	cmd, dir := agyHarnessCommand(t, body, `{"ok":true,"reflectData":{"endpoints":[{"configured":true,"provider":"gemini","models_url":"http://api-proxy:10004/v1beta/models"}]}}`)
	out, err := cmd.Output()
	require.NoError(t, err)
	var observation struct {
		Init struct {
			Input struct {
				Message struct{ Content string }
			}
			Args                   []string
			Settings               map[string]string
			Home, APIKey, Endpoint string
			ADC, GoogleKey         *string
		}
	}
	require.NoError(t, json.Unmarshal([]byte(strings.Split(string(out), "\n")[0]), &observation))
	assert.Equal(t, "Literal $(touch SHOULD_NOT_EXIST)\n\"quoted\"; --model not-an-argument", observation.Init.Input.Message.Content)
	assert.Equal(t, map[string]string{"modelProvider": "gemini"}, observation.Init.Settings)
	assert.Equal(t, "awf-proxy", observation.Init.APIKey)
	assert.Equal(t, "http://api-proxy:10004", observation.Init.Endpoint)
	assert.Nil(t, observation.Init.ADC)
	assert.Nil(t, observation.Init.GoogleKey)
	assert.NotContains(t, strings.Join(observation.Init.Args, " "), "SHOULD_NOT_EXIST")
	assert.Contains(t, observation.Init.Args, "--print-timeout")
	_, err = os.Stat(observation.Init.Home)
	assert.True(t, os.IsNotExist(err), "private native settings must be cleaned up")
	config, err := os.ReadFile(filepath.Join(dir, ".agents", "mcp_config.json"))
	require.NoError(t, err)
	assert.JSONEq(t, `{"mcpServers":{}}`, string(config))
}

func TestAgyHarnessFailsClosed(t *testing.T) {
	validReflect := `{"ok":true,"reflectData":{"endpoints":[{"configured":true,"provider":"gemini","models_url":"http://api-proxy:10004/v1beta/models"}]}}`
	for _, tt := range []struct{ name, body, reflect string }{
		{"missing endpoint", `console.log("must not execute");`, `{"ok":false}`},
		{"unconfigured endpoint", `console.log("must not execute");`, `{"ok":true,"reflectData":{"endpoints":[{"configured":false,"provider":"gemini","models_url":"http://api-proxy/models"}]}}`},
		{"native failure", `console.log(JSON.stringify({event:"result",result:{status:"ERROR"}}));process.exitCode=3;`, validReflect},
		{"missing result", `process.stdin.resume();`, validReflect},
		{"malformed stream", `console.log("{invalid");`, validReflect},
		{"waiting", `console.log(JSON.stringify({event:"result",result:{status:"WAITING"}}));`, validReflect},
		{"missing inference", `console.log(JSON.stringify({event:"result",result:{status:"SUCCESS"}}));`, validReflect},
		{"zero usage", `console.log(JSON.stringify({event:"result",result:{status:"SUCCESS",num_turns:1,usage:{input_tokens:0,output_tokens:0}}}));`, validReflect},
		{"invalid usage", `console.log(JSON.stringify({event:"result",result:{status:"SUCCESS",num_turns:1,usage:{input_tokens:"10",output_tokens:2}}}));`, validReflect},
		{"pending tool", `console.log(JSON.stringify({event:"step_update",step_update:{step_type:"tool",step_index:1,state:"ACTIVE"}}));console.log(JSON.stringify({event:"result",result:{status:"SUCCESS",num_turns:1,usage:{input_tokens:10,output_tokens:2}}}));`, validReflect},
		{"soft denial", `process.stderr.write("soft-denied: approval required\n");console.log(JSON.stringify({event:"result",result:{status:"SUCCESS",num_turns:1,usage:{input_tokens:10,output_tokens:2}}}));`, validReflect},
		{"signal", `process.kill(process.pid,"SIGTERM");`, validReflect},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd, _ := agyHarnessCommand(t, tt.body, tt.reflect)
			out, err := cmd.CombinedOutput()
			require.Error(t, err)
			assert.NotContains(t, string(out), "must not execute")
			assert.NotContains(t, string(out), "host-test-key")
			assert.Contains(t, string(out), "session.error")
		})
	}
}

func TestAgyHarnessWatchdogAndParentInterruption(t *testing.T) {
	validReflect := `{"ok":true,"reflectData":{"endpoints":[{"configured":true,"provider":"gemini","models_url":"http://api-proxy:10004/v1beta/models"}]}}`
	t.Run("watchdog escalates an unresponsive native process", func(t *testing.T) {
		cmd, dir := agyHarnessCommand(t, `process.on("SIGTERM", () => {}); setInterval(() => {}, 1000);`, validReflect)
		shim := filepath.Join(dir, "bounded-watchdog.cjs")
		require.NoError(t, os.WriteFile(shim, []byte(`const original = global.setTimeout;
global.setTimeout = (callback, delay, ...args) => original(callback, delay === 305000 ? 150 : delay === 1000 ? 50 : delay, ...args);
`), 0o600))
		cmd.Args = slices.Insert(cmd.Args, 1, "--require", shim)
		out, err := cmd.CombinedOutput()
		require.Error(t, err)
		require.Contains(t, string(out), "session.error")
	})
	t.Run("parent interruption cannot preserve a success result", func(t *testing.T) {
		body := `process.on("SIGINT", () => {
  console.log(JSON.stringify({event:"result",result:{status:"SUCCESS",num_turns:1,usage:{input_tokens:10,output_tokens:2}}}));
  process.exit(0);
});
setInterval(() => {}, 1000);
process.kill(process.ppid, "SIGINT");`
		cmd, _ := agyHarnessCommand(t, body, validReflect)
		out, err := cmd.CombinedOutput()
		require.Error(t, err)
		require.Contains(t, string(out), `"status":"ERROR"`)
		require.Contains(t, string(out), `"input_tokens":10`)
		require.Contains(t, string(out), "session.error")
	})
}

func TestAgyHarnessBoundsDescendantPipes(t *testing.T) {
	validReflect := `{"ok":true,"reflectData":{"endpoints":[{"configured":true,"provider":"gemini","models_url":"http://api-proxy:10004/v1beta/models"}]}}`
	for _, detached := range []bool{false, true} {
		for _, earlyExit := range []bool{false, true} {
			t.Run("detached="+strconv.FormatBool(detached)+"/early-exit="+strconv.FormatBool(earlyExit), func(t *testing.T) {
				exit := ""
				if earlyExit {
					exit = `console.log(JSON.stringify({event:"result",result:{status:"SUCCESS",num_turns:1,usage:{input_tokens:10,output_tokens:2}}}));
process.exit(0);`
				}
				body := `const { spawn } = require("node:child_process");
const fs = require("node:fs");
const child = spawn(process.execPath, ["-e", "process.on('SIGTERM', () => {}); setInterval(() => {}, 1000);"],
  { detached: ` + strconv.FormatBool(detached) + `, stdio: ["ignore", "inherit", "inherit"] });
fs.writeFileSync(process.env.GITHUB_WORKSPACE + "/descendant.json", JSON.stringify({ pid: child.pid, home: process.env.HOME }));
process.on("SIGTERM", () => {});
` + exit + `
setInterval(() => {}, 1000);`
				cmd, dir := agyHarnessCommand(t, body, validReflect)
				shim := filepath.Join(dir, "bounded-watchdog.cjs")
				require.NoError(t, os.WriteFile(shim, []byte(`const original = global.setTimeout;
global.setTimeout = (callback, delay, ...args) => original(callback, delay === 305000 ? 250 : delay === 1000 ? 50 : delay === 1500 ? 150 : delay, ...args);
`), 0o600))
				cmd.Args = slices.Insert(cmd.Args, 1, "--require", shim)
				start := time.Now()
				out, err := cmd.CombinedOutput()
				if earlyExit && !detached {
					require.NoError(t, err, "%s", out)
				} else {
					require.Error(t, err)
					assert.Contains(t, string(out), "session.error")
				}
				var receipt struct {
					PID  int    `json:"pid"`
					Home string `json:"home"`
				}
				data, readErr := os.ReadFile(filepath.Join(dir, "descendant.json"))
				require.NoError(t, readErr)
				require.NoError(t, json.Unmarshal(data, &receipt))
				t.Cleanup(func() { _ = syscall.Kill(receipt.PID, syscall.SIGKILL) })
				assert.Less(t, time.Since(start), 3*time.Second)
				_, err = os.Stat(receipt.Home)
				assert.True(t, os.IsNotExist(err), "bounded completion must clean the private settings home")
				if !detached {
					state, _ := exec.Command("ps", "-p", strconv.Itoa(receipt.PID), "-o", "stat=").Output()
					assert.True(t, len(strings.TrimSpace(string(state))) == 0 || strings.HasPrefix(strings.TrimSpace(string(state)), "Z"), "descendant must be terminated")
				}
			})
		}
	}
}

func TestAgyHarnessRejectsWrongCustomExecutableVersion(t *testing.T) {
	cmd, _ := agyHarnessCommand(t, `console.log("must not execute");`, `{"ok":false}`)
	require.NoError(t, os.WriteFile(cmd.Args[2], []byte("#!/usr/bin/env node\nconsole.log('0.0.0');\n"), 0o700))
	out, err := cmd.CombinedOutput()
	require.Error(t, err)
	assert.Contains(t, string(out), "Agy executable does not match engine.version")
	assert.NotContains(t, string(out), "must not execute")
}

func TestAgyRuntimeScriptsHaveValidSyntax(t *testing.T) {
	for name, script := range map[string][]byte{
		"harness": agyRuntimeScript(t, "agy_harness.cjs"),
		"adapter": agyRuntimeScript(t, "convert_gateway_config_agy.cjs"),
	} {
		t.Run(name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), name+".cjs")
			require.NoError(t, os.WriteFile(file, script, 0o600))
			out, err := exec.Command("node", "--check", file).CombinedOutput()
			require.NoError(t, err, "%s", out)
		})
	}
}

func TestAgyMCPConfigurationAdapter(t *testing.T) {
	for _, tt := range []struct {
		name, config   string
		valid, symlink bool
	}{
		{"gateway", `{"mcpServers":{"native":{"url":"http://gateway:8080/mcp/native","headers":{"Authorization":"test-gateway-token"}},"safeoutputs":{"command":"ignored"}}}`, true, false},
		{"invalid root", `{"mcpServers":[]}`, false, false},
		{"stdio", `{"mcpServers":{"native":{"command":"node"}}}`, false, false},
		{"nonstring header", `{"mcpServers":{"native":{"url":"http://gateway:8080/mcp/native","headers":{"Authorization":123}}}}`, false, false},
		{"external endpoint", `{"mcpServers":{"native":{"url":"https://outside.example/mcp/native"}}}`, false, false},
		{"dangling symlink", `{"mcpServers":{}}`, false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			adapter := filepath.Join(dir, "adapter.cjs")
			require.NoError(t, os.WriteFile(adapter, agyRuntimeScript(t, "convert_gateway_config_agy.cjs"), 0o600))
			shared, err := filepath.Abs("../../actions/setup/js/convert_gateway_config_shared.cjs")
			require.NoError(t, err)
			require.NoError(t, os.Symlink(shared, filepath.Join(dir, "convert_gateway_config_shared.cjs")))
			gateway := filepath.Join(dir, "gateway.json")
			require.NoError(t, os.WriteFile(gateway, []byte(tt.config), 0o600))
			if tt.symlink {
				require.NoError(t, os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, ".agents")))
			}
			cmd := exec.Command("node", adapter)
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GITHUB_WORKSPACE=" + dir,
				"MCP_GATEWAY_OUTPUT=" + gateway, "MCP_GATEWAY_DOMAIN=host.docker.internal",
				"MCP_GATEWAY_PORT=80", `GH_AW_MCP_CLI_SERVERS=["safeoutputs"]`}
			out, err := cmd.CombinedOutput()
			if !tt.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err, "%s", out)
			file := filepath.Join(dir, ".agents", "mcp_config.json")
			data, err := os.ReadFile(file)
			require.NoError(t, err)
			assert.JSONEq(t, `{"mcpServers":{"native":{"serverUrl":"http://host.docker.internal:80/mcp/native","headers":{"Authorization":"test-gateway-token"}}}}`, string(data))
			stat, err := os.Stat(file)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o600), stat.Mode().Perm())
		})
	}
}
