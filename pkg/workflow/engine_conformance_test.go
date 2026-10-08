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

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

type conformanceStep struct {
	Name string `yaml:"name"`
	Run  string `yaml:"run"`
	If   string `yaml:"if"`
}

type conformanceSuite struct {
	PreAgentSteps []conformanceStep `yaml:"pre-agent-steps"`
	PostSteps     []conformanceStep `yaml:"post-steps"`
	MCPScripts    map[string]struct {
		Description string `yaml:"description"`
		Script      string `yaml:"script"`
	} `yaml:"mcp-scripts"`
}

func readConformanceFrontmatter(t *testing.T, path string, target any) {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	frontmatter, err := extractMarkdownFrontmatterYAML(content)
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(frontmatter, target))
}

func conformanceNodeScript(t *testing.T, run string) string {
	t.Helper()
	_, script, ok := strings.Cut(run, "node <<'JS'\n")
	require.True(t, ok, "expected embedded JavaScript heredoc")
	script, ok = strings.CutSuffix(script, "\nJS\n")
	require.True(t, ok, "expected closing JavaScript heredoc")
	return script
}

func runConformanceNode(t *testing.T, env []string, args ...string) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "node", args...)
	command.Env = append(os.Environ(), env...)
	return command.CombinedOutput()
}

func TestEngineConformanceJavaScript(t *testing.T) {
	var suite conformanceSuite
	readConformanceFrontmatter(t, "../../.github/workflows/shared/engine-conformance.md", &suite)
	require.Len(t, suite.PreAgentSteps, 1)
	require.Len(t, suite.PostSteps, 2)
	require.Equal(t, "always()", suite.PostSteps[0].If)
	prepare := conformanceNodeScript(t, suite.PreAgentSteps[0].Run)
	verify := conformanceNodeScript(t, suite.PostSteps[0].Run)
	tool := suite.MCPScripts["conformance-challenge"]
	require.NotEmpty(t, tool.Script)

	tests := []struct {
		name       string
		mutate     string
		execution  string
		wantFailed string
	}{
		{name: "success", execution: "success"},
		{name: "failed agent", execution: "failure", wantFailed: "agent-execution"},
		{name: "timed out agent", execution: "cancelled", wantFailed: "agent-execution"},
		{name: "missing result", execution: "success", mutate: `fs.unlinkSync(resultPath);`, wantFailed: "result-schema"},
		{name: "malformed result", execution: "success", mutate: `fs.writeFileSync(resultPath, "{");`, wantFailed: "result-schema"},
		{name: "extra field", execution: "success", mutate: `result.passed = true; save();`, wantFailed: "result-schema"},
		{name: "wrong number type", execution: "success", mutate: `result.sum = String(result.sum); save();`, wantFailed: "result-schema"},
		{name: "wrong arithmetic", execution: "success", mutate: `result.sum++; save();`, wantFailed: "inference"},
		{name: "wrong file nonce", execution: "success", mutate: `result.fileNonce = "guessed"; save();`, wantFailed: "file-read"},
		{name: "wrong tool nonce", execution: "success", mutate: `result.toolNonce = "guessed"; save();`, wantFailed: "tool-round-trip"},
		{name: "missing receipt", execution: "success", mutate: `fs.unlinkSync(path.join(host, "receipt.json"));`, wantFailed: "tool-round-trip"},
		{name: "missing shell", execution: "success", mutate: `fs.unlinkSync(path.join(root, "shell.json"));`, wantFailed: "shell-and-environment"},
		{name: "wrong environment", execution: "success", mutate: `result.engineEnv = "other"; save();`, wantFailed: "shell-and-environment"},
		{name: "modified fixture", execution: "success", mutate: `fs.writeFileSync(path.join(root, "input.json"), "{}");`, wantFailed: "fixtures"},
		{name: "oversized result", execution: "success", mutate: `fs.writeFileSync(resultPath, " ".repeat(16385));`, wantFailed: "result-schema"},
		{name: "symlink result", execution: "success", mutate: `fs.renameSync(resultPath, resultPath + ".real"); fs.symlinkSync(resultPath + ".real", resultPath);`, wantFailed: "result-schema"},
		{name: "missing safe output", execution: "success", mutate: `fs.unlinkSync(process.env.CONFORMANCE_SAFE_OUTPUTS);`, wantFailed: "staged-safe-output"},
		{name: "empty safe-output placeholder", execution: "success", mutate: `fs.writeFileSync(process.env.CONFORMANCE_SAFE_OUTPUTS, "");`, wantFailed: "staged-safe-output"},
		{name: "wrong safe-output type", execution: "success", mutate: `fs.writeFileSync(process.env.CONFORMANCE_SAFE_OUTPUTS, JSON.stringify({ type: "missing_tool", message: "Conformance probes completed" }));`, wantFailed: "staged-safe-output"},
		{name: "wrong noop message", execution: "success", mutate: `fs.writeFileSync(process.env.CONFORMANCE_SAFE_OUTPUTS, JSON.stringify({ type: "noop", message: "guessed" }));`, wantFailed: "staged-safe-output"},
		{name: "duplicate noop", execution: "success", mutate: `fs.appendFileSync(process.env.CONFORMANCE_SAFE_OUTPUTS, fs.readFileSync(process.env.CONFORMANCE_SAFE_OUTPUTS));`, wantFailed: "staged-safe-output"},
		{name: "symlink safe output", execution: "success", mutate: `fs.renameSync(process.env.CONFORMANCE_SAFE_OUTPUTS, process.env.CONFORMANCE_SAFE_OUTPUTS + ".real"); fs.symlinkSync(process.env.CONFORMANCE_SAFE_OUTPUTS + ".real", process.env.CONFORMANCE_SAFE_OUTPUTS);`, wantFailed: "staged-safe-output"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			root := filepath.Join(directory, "agent")
			host := filepath.Join(directory, "host")
			env := []string{
				"CONFORMANCE_ENGINE=copilot", "CONFORMANCE_ROOT=" + root,
				"CONFORMANCE_STATE=" + host, "CONFORMANCE_EXECUTION=" + test.execution,
				"ENGINE_CONFORMANCE_SENTINEL=conformance-copilot",
				"GITHUB_STEP_SUMMARY=" + filepath.Join(directory, "summary.md"),
				"CONFORMANCE_SAFE_OUTPUTS=" + filepath.Join(host, "safeoutputs.jsonl"),
			}
			output, err := runConformanceNode(t, env, "-e", prepare)
			require.NoError(t, err, "%s", output)
			output, err = runConformanceNode(t, env, filepath.Join(root, "shell-probe.cjs"))
			require.NoError(t, err, "%s", output)

			toolScript := `
const fs = require("node:fs");
const path = require("node:path");
const root = process.env.CONFORMANCE_ROOT;
const host = process.env.CONFORMANCE_STATE;
const input = JSON.parse(fs.readFileSync(path.join(root, "input.json"), "utf8"));
async function execute(file_nonce) {
` + tool.Script + `
}
(async () => {
  await require("node:assert/strict").rejects(execute("wrong"), /nonce does not match/);
  const proof = await execute(input.fileNonce);
  const shell = JSON.parse(fs.readFileSync(path.join(root, "shell.json"), "utf8"));
  const result = { fileNonce: input.fileNonce, sum: input.left + input.right, ...shell, toolNonce: proof.toolNonce };
  const resultPath = path.join(root, "result.json");
  const save = () => fs.writeFileSync(resultPath, JSON.stringify(result));
  save();
  const { createHandlers } = require("../../actions/setup/js/safe_outputs_handlers.cjs");
  const { createAppendFunction } = require("../../actions/setup/js/safe_outputs_append.cjs");
  createHandlers({ debug() {} }, createAppendFunction(process.env.CONFORMANCE_SAFE_OUTPUTS))
    .defaultHandler("noop")({ message: "Conformance probes completed" });
  ` + test.mutate + `
})().catch(error => { console.error(error); process.exitCode = 1; });
`
			output, err = runConformanceNode(t, env, "-e", toolScript)
			require.NoError(t, err, "%s", output)
			output, err = runConformanceNode(t, env, "-e", verify)
			reportContent, readErr := os.ReadFile(filepath.Join(host, "report.json"))
			require.NoError(t, readErr)
			var report struct {
				Status string `json:"status"`
				Checks []struct {
					Name   string `json:"name"`
					Status string `json:"status"`
				} `json:"checks"`
			}
			require.NoError(t, json.Unmarshal(reportContent, &report))
			require.Len(t, report.Checks, 8)
			if test.wantFailed == "" {
				require.NoError(t, err, "%s", output)
				require.Equal(t, "passed", report.Status)
			} else {
				require.Error(t, err, "%s", output)
				require.Equal(t, "failed", report.Status)
				found := false
				for _, check := range report.Checks {
					if check.Name == test.wantFailed && check.Status == "failed" {
						found = true
					}
				}
				require.True(t, found, "%s should fail: %s", test.wantFailed, reportContent)
			}
		})
	}
}

func TestAgyNativeConformanceRequiresStagedNoop(t *testing.T) {
	var suite conformanceSuite
	readConformanceFrontmatter(t, "../../.github/workflows/shared/agy-conformance.md", &suite)
	verify := conformanceNodeScript(t, suite.PostSteps[0].Run)
	for _, item := range []struct {
		name, output string
		passed       bool
	}{
		{"exact noop", `{"type":"noop","message":"Conformance probes completed"}` + "\n", true},
		{"empty placeholder", "", false},
		{"wrong type", `{"type":"missing_tool","message":"Conformance probes completed"}`, false},
		{"wrong message", `{"type":"noop","message":"guessed"}`, false},
		{"duplicate noop", `{"type":"noop","message":"Conformance probes completed"}` + "\n" + `{"type":"noop","message":"Conformance probes completed"}`, false},
	} {
		t.Run(item.name, func(t *testing.T) {
			dir := t.TempDir()
			logPath := filepath.Join(dir, "native.jsonl")
			outputPath := filepath.Join(dir, "outputs.jsonl")
			for name, content := range map[string]string{
				"expected.json":       `{"fileNonce":"fixture"}`,
				"native-receipt.json": `{"fileNonce":"fixture","toolNonce":"` + strings.Repeat("a", 48) + `"}`,
				"outputs.jsonl":       item.output,
				"native.jsonl": `{"event":"init","init":{"model":"gemini-3.8-flash-medium"}}
	{"event":"step_update","step_update":{"step_type":"tool","state":"DONE","tool_info":{"name":"call_mcp_tool","parameters":{"ServerName":"agy-native","ToolName":"native_challenge","Arguments":{"file_nonce":"fixture"}},"output":"{\"toolNonce\":\"` + strings.Repeat("a", 48) + `\"}"}}}
	{"event":"result","result":{"status":"SUCCESS","num_turns":1,"usage":{"input_tokens":10,"output_tokens":2}}}`,
			} {
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
			}
			encodedPath, err := json.Marshal(logPath)
			require.NoError(t, err)
			script := strings.ReplaceAll(verify, `"/tmp/gh-aw/agent-stdio.log"`, string(encodedPath))
			out, err := runConformanceNode(t, []string{"CONFORMANCE_STATE=" + dir, "CONFORMANCE_SAFE_OUTPUTS=" + outputPath}, "-e", script)
			reportPath := filepath.Join(dir, "native-report.json")
			if item.passed {
				require.NoError(t, err, "%s", out)
				report, readErr := os.ReadFile(reportPath)
				require.NoError(t, readErr)
				require.Contains(t, string(report), `"stagedSafeOutputs": true`)
			} else {
				require.Error(t, err, "%s", out)
				_, statErr := os.Stat(reportPath)
				require.True(t, os.IsNotExist(statErr), "invalid noop evidence must not produce a passing native report")
			}
		})
	}
}

func TestAgyNativeConformanceRequiresCompletedNativeCall(t *testing.T) {
	var suite conformanceSuite
	readConformanceFrontmatter(t, "../../.github/workflows/shared/agy-conformance.md", &suite)
	verify := conformanceNodeScript(t, suite.PostSteps[0].Run)
	for _, item := range []struct {
		name, mutate string
		passed       bool
	}{
		{name: "actual native event", passed: true},
		{name: "active call", mutate: `step.state = "ACTIVE";`},
		{name: "missing state", mutate: `delete step.state;`},
		{name: "CLI wrapper", mutate: `info.name = "run_command";`},
		{name: "wrong server", mutate: `info.parameters.ServerName = "mcpscripts";`},
		{name: "wrong tool", mutate: `info.parameters.ToolName = "conformance_challenge";`},
		{name: "wrong nonce", mutate: `info.parameters.Arguments.file_nonce = "guessed";`},
		{name: "missing parameters", mutate: `delete info.parameters;`},
		{name: "failed tool", mutate: `info.error = { type: "ERROR", message: "failed" };`},
		{name: "wrong receipt", mutate: `info.output = JSON.stringify({ toolNonce: "guessed" });`},
		{name: "invalid response", mutate: `info.output = "not JSON";`},
	} {
		t.Run(item.name, func(t *testing.T) {
			dir := t.TempDir()
			logPath := filepath.Join(dir, "native.jsonl")
			outputPath := filepath.Join(dir, "outputs.jsonl")
			prepare := `
const fs = require("node:fs");
const path = require("node:path");
const host = process.env.CONFORMANCE_STATE;
const toolNonce = "a".repeat(48);
const info = {
  name: "call_mcp_tool",
  parameters: { ServerName: "agy-native", ToolName: "native_challenge", Arguments: { file_nonce: "fixture" } },
  output: JSON.stringify({ toolNonce }),
};
const step = { step_type: "tool", state: "DONE", tool_info: info };
` + item.mutate + `
const entries = [
  { event: "init", init: { model: "gemini-3.8-flash-medium" } },
  { event: "step_update", step_update: step },
  { event: "result", result: { status: "SUCCESS", num_turns: 1, usage: { input_tokens: 10, output_tokens: 2 } } },
];
fs.writeFileSync(path.join(host, "native.jsonl"), entries.map(entry => JSON.stringify(entry)).join("\n"));
fs.writeFileSync(path.join(host, "expected.json"), JSON.stringify({ fileNonce: "fixture" }));
fs.writeFileSync(path.join(host, "native-receipt.json"), JSON.stringify({ fileNonce: "fixture", toolNonce }));
fs.writeFileSync(process.env.CONFORMANCE_SAFE_OUTPUTS, JSON.stringify({ type: "noop", message: "Conformance probes completed" }) + "\n");
`
			env := []string{"CONFORMANCE_STATE=" + dir, "CONFORMANCE_SAFE_OUTPUTS=" + outputPath}
			out, err := runConformanceNode(t, env, "-e", prepare)
			require.NoError(t, err, "%s", out)
			encodedPath, err := json.Marshal(logPath)
			require.NoError(t, err)
			script := strings.ReplaceAll(verify, `"/tmp/gh-aw/agent-stdio.log"`, string(encodedPath))
			out, err = runConformanceNode(t, env, "-e", script)
			if item.passed {
				require.NoError(t, err, "%s", out)
			} else {
				require.Error(t, err, "%s", out)
				_, statErr := os.Stat(filepath.Join(dir, "native-report.json"))
				require.True(t, os.IsNotExist(statErr), "invalid native calls must not produce a passing report")
			}
		})
	}
}

func TestAgyNativeConformanceAllowlistMatchesServerTools(t *testing.T) {
	var suite conformanceSuite
	var source struct {
		MCPServers map[string]struct {
			Allowed []string `yaml:"allowed"`
		} `yaml:"mcp-servers"`
	}
	file := "../../.github/workflows/shared/agy-conformance.md"
	readConformanceFrontmatter(t, file, &suite)
	readConformanceFrontmatter(t, file, &source)
	allowed := source.MCPServers["agy-native"].Allowed
	require.Equal(t, []string{"native_challenge"}, allowed)
	require.Contains(t, suite.MCPScripts, "native-challenge")

	names := make([]string, 0, len(suite.MCPScripts))
	for name := range suite.MCPScripts {
		names = append(names, name)
	}
	encodedNames, err := json.Marshal(names)
	require.NoError(t, err)
	encodedAllowed, err := json.Marshal(allowed)
	require.NoError(t, err)
	script := `
const assert = require("node:assert/strict");
const { registerTool, handleRequest } = require("../../actions/setup/js/mcp_server_core.cjs");
const server = { tools: {}, debug() {} };
for (const name of ` + string(encodedNames) + `) {
  registerTool(server, { name, description: "Conformance probe", inputSchema: { type: "object" } });
}
handleRequest(server, { jsonrpc: "2.0", id: 1, method: "tools/list" })
  .then(response => {
    assert.equal(response.error, undefined);
    const exposed = response.result.tools.map(tool => tool.name);
    for (const name of ` + string(encodedAllowed) + `) {
      assert.ok(exposed.includes(name), "Native gateway allowlist must match tools/list: " + name);
    }
  })
  .catch(error => { console.error(error); process.exitCode = 1; });
`
	output, err := runConformanceNode(t, nil, "-e", script)
	require.NoError(t, err, "%s", output)
}

func TestEngineConformanceCatalogCoverage(t *testing.T) {
	registry := NewEngineRegistry()
	ids := registry.GetSupportedEngines()
	var catalog knownEngineImportsFile
	content, err := os.ReadFile("../../.github/aw/engines.json")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(content, &catalog))
	for _, entry := range catalog.Engines {
		ids = append(ids, entry.ID)
	}
	files, err := filepath.Glob("../../.github/workflows/engine-conformance-*.md")
	require.NoError(t, err)
	require.Len(t, files, len(ids), "each runnable engine must have exactly one conformance workflow")
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			file := "../../.github/workflows/engine-conformance-" + id + ".md"
			var source map[string]any
			readConformanceFrontmatter(t, file, &source)
			engine, ok := source["engine"].(map[string]any)
			require.True(t, ok)
			require.Equal(t, id, engine["id"])
			require.Equal(t, true, source["strict"])
			require.Equal(t, true, source["inlined-imports"])
			require.EqualValues(t, 10, source["timeout-minutes"])
			require.EqualValues(t, 5, source["max-ai-credits"])
			on, ok := source["on"].(map[string]any)
			require.True(t, ok)
			require.Contains(t, on, "workflow_dispatch")
			require.Len(t, on, 1, "live inference should be manual-only")
			lock, err := os.ReadFile(strings.TrimSuffix(file, ".md") + ".lock.yml")
			require.NoError(t, err)
			compiled := string(lock)
			for _, expected := range []string{
				"Prepare engine conformance fixtures", "Assert engine conformance",
				"Upload engine conformance evidence", "conformance-challenge",
				"ENGINE_CONFORMANCE_SENTINEL: conformance-" + id,
				"CONFORMANCE_EXECUTION: ${{ steps.agentic_execution.outcome }}",
			} {
				require.Contains(t, compiled, expected, "compiled %s is missing %s", id, expected)
			}
			require.NotContains(t, compiled, "github.aw.import-inputs", "import inputs must be resolved")
			require.Contains(t, compiled, `GH_AW_SAFE_OUTPUTS_STAGED: "true"`, "safe outputs must not publish test issues")
		})
	}
}
