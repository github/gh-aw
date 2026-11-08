//go:build !integration

package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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

func TestConformanceTransportsWithoutGitHubMCP(t *testing.T) {
	var source struct {
		Tools map[string]any `yaml:"tools"`
	}
	readConformanceFrontmatter(t, "../../.github/workflows/shared/engine-conformance.md", &source)
	require.Equal(t, false, source.Tools["github"])
	source.Tools["work-queue"] = map[string]any{"worker": true, "require-assignment": true}
	source.Tools["cli-proxy"] = true
	data := &WorkflowData{
		Tools:       source.Tools,
		ParsedTools: NewTools(source.Tools),
		SafeOutputs: &SafeOutputsConfig{NoOp: &NoOpConfig{}},
		MCPScripts: &MCPScriptsConfig{Tools: map[string]*MCPScriptToolConfig{
			"conformance-challenge": {Script: "return 'nonce';"},
		}},
	}
	require.ElementsMatch(t, []string{"mcpscripts", "safeoutputs", "work-queue"}, getMCPCLIServerNames(data))
	require.False(t, primaryGitHubMCPEnabled(data))
}

func TestMiniSweAgentConformanceProxyContract(t *testing.T) {
	var worker struct {
		Tools map[string]any `yaml:"tools"`
	}
	readConformanceFrontmatter(t, "../../.github/workflows/engine-conformance-mini-swe-agent.md", &worker)
	var source struct {
		Engine EngineDefinition `yaml:"engine"`
	}
	readConformanceFrontmatter(t, "../../.github/workflows/shared/mini-swe-agent.md", &source)
	engine, err := NewBehaviorDefinedEngine(&source.Engine)
	require.NoError(t, err)
	tools, err := enforceMCPProxyTools(engine, worker.Tools)
	require.NoError(t, err, "non-MCP workers must explicitly retain the required gh-proxy transport")
	github := tools["github"].(map[string]any)
	require.Equal(t, "gh-proxy", github["mode"])
	require.Equal(t, []any{"repos"}, github["toolsets"])
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

func conformanceQueueEnv(t *testing.T, engine string) []string {
	t.Helper()
	assignment, err := json.Marshal(map[string]any{
		"version": 3, "dispatch_id": "dispatch", "request_id": "request",
		"commit_id": "commit", "policy_epoch": "policy", "pool": "engine-conformance",
		"worker_profile": "engine-conformance-" + engine,
		"claims": []map[string]any{{
			"handle": "original-" + engine, "claim_id": "claim", "work_id": "work",
			"work":        map[string]any{"engine": engine, "effect_contract": map[string]string{"kind": "none"}},
			"result_refs": []any{},
		}},
	})
	require.NoError(t, err)
	actionsDir, err := filepath.Abs("../../actions/setup/js")
	require.NoError(t, err)
	return []string{
		"CONFORMANCE_ASSIGNMENT=" + string(assignment), "GH_AW_ACTIONS_DIR=" + actionsDir,
		"GITHUB_EVENT_PATH=" + conformanceEventPath(t, string(assignment)),
		"GH_AW_WORK_QUEUE_ENABLED=true", "GH_AW_WORK_QUEUE_ROLE=worker",
	}
}

func conformanceEventPath(t *testing.T, assignment string) string {
	t.Helper()
	event, err := json.Marshal(map[string]any{"inputs": map[string]string{"work_queue_assignment": assignment}})
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "event.json")
	require.NoError(t, os.WriteFile(path, event, 0o600))
	return path
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
		engine     string
		mutate     string
		execution  string
		wantFailed string
		standalone bool
	}{
		{name: "success", execution: "success"},
		{name: "codex success", engine: "codex", execution: "success"},
		{name: "agy success", engine: "agy", execution: "success"},
		{name: "standalone agy success", engine: "agy", execution: "success", standalone: true},
		{name: "pi success", engine: "pi", execution: "success"},
		{name: "gemini success", engine: "gemini", execution: "success"},
		{name: "pi interrupted before noop", engine: "pi", execution: "success", mutate: `fs.unlinkSync(process.env.CONFORMANCE_SAFE_OUTPUTS);`, wantFailed: "staged-safe-output"},
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
		{name: "malformed safe output", execution: "success", mutate: `fs.writeFileSync(process.env.CONFORMANCE_SAFE_OUTPUTS, "{");`, wantFailed: "staged-safe-output"},
		{name: "oversized safe output", execution: "success", mutate: `fs.appendFileSync(process.env.CONFORMANCE_SAFE_OUTPUTS, " ".repeat(16384));`, wantFailed: "staged-safe-output"},
		{name: "missing claim handle", execution: "success", mutate: `delete noop.claim_handle; saveNoop();`, wantFailed: "staged-safe-output"},
		{name: "foreign claim handle", execution: "success", mutate: `noop.claim_handle = "foreign"; saveNoop();`, wantFailed: "staged-safe-output"},
		{name: "hardcoded claim handle", execution: "success", mutate: `noop.claim_handle = "h1"; saveNoop();`, wantFailed: "staged-safe-output"},
		{name: "null claim handle", execution: "success", mutate: `noop.claim_handle = null; saveNoop();`, wantFailed: "staged-safe-output"},
		{name: "conflicting claim selector", execution: "success", mutate: `noop.claim_id = "foreign"; saveNoop();`, wantFailed: "staged-safe-output"},
		{name: "conflicting work selector", execution: "success", mutate: `noop.work_id = "foreign"; saveNoop();`, wantFailed: "staged-safe-output"},
		{name: "extra field in noop", execution: "success", mutate: `noop.passed = true; saveNoop();`, wantFailed: "staged-safe-output"},
		{name: "extra scope", execution: "success", mutate: `noop.assignment = {}; saveNoop();`, wantFailed: "staged-safe-output"},
		{name: "wrong safe-output type", execution: "success", mutate: `noop.type = "report_missing_tool"; saveNoop();`, wantFailed: "staged-safe-output"},
		{name: "wrong noop message", execution: "success", mutate: `noop.message = "guessed"; saveNoop();`, wantFailed: "staged-safe-output"},
		{name: "duplicate noop", execution: "success", mutate: `fs.appendFileSync(process.env.CONFORMANCE_SAFE_OUTPUTS, fs.readFileSync(process.env.CONFORMANCE_SAFE_OUTPUTS));`, wantFailed: "staged-safe-output"},
		{name: "symlink safe output", execution: "success", mutate: `fs.renameSync(process.env.CONFORMANCE_SAFE_OUTPUTS, process.env.CONFORMANCE_SAFE_OUTPUTS + ".real"); fs.symlinkSync(process.env.CONFORMANCE_SAFE_OUTPUTS + ".real", process.env.CONFORMANCE_SAFE_OUTPUTS);`, wantFailed: "staged-safe-output"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine := test.engine
			if engine == "" {
				engine = "copilot"
			}
			directory := t.TempDir()
			root := filepath.Join(directory, "agent")
			host := filepath.Join(directory, "host")
			env := []string{
				"CONFORMANCE_ENGINE=" + engine, "CONFORMANCE_ROOT=" + root,
				"CONFORMANCE_STATE=" + host, "CONFORMANCE_EXECUTION=" + test.execution,
				"ENGINE_CONFORMANCE_SENTINEL=conformance-" + engine,
				"GITHUB_STEP_SUMMARY=" + filepath.Join(directory, "summary.md"),
				"CONFORMANCE_SAFE_OUTPUTS=" + filepath.Join(host, "safeoutputs.jsonl"),
			}
			env = append(env, conformanceQueueEnv(t, engine)...)
			if test.standalone {
				env = append(env, "CONFORMANCE_ASSIGNMENT=", "GH_AW_WORK_QUEUE_ENABLED=false", "GH_AW_WORK_QUEUE_ROLE=",
					"GITHUB_EVENT_PATH="+conformanceEventPath(t, ""))
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
  const { withClaimExecution } = require("../../actions/setup/js/work_queue_claim_scope.cjs");
  const assignment = process.env.CONFORMANCE_ASSIGNMENT ? JSON.parse(process.env.CONFORMANCE_ASSIGNMENT) : null;
  const emit = () => {
    const handler = createHandlers({ debug() {} }, createAppendFunction(process.env.CONFORMANCE_SAFE_OUTPUTS)).defaultHandler("noop");
    if (assignment) {
      for (const selector of [{ claim_handle: "foreign" }, { claim_id: "foreign" }, { work_id: "foreign" }, { assignment: {} }]) {
        require("node:assert/strict").throws(() => handler({ message: "Conformance probes completed", ...selector }));
      }
    }
    handler({ message: "Conformance probes completed" });
  };
  if (assignment) withClaimExecution({ assignment, claim_handle: assignment.claims[0].handle }, emit);
  else emit();
  const { output_id: outputId, ...noop } = JSON.parse(fs.readFileSync(process.env.CONFORMANCE_SAFE_OUTPUTS, "utf8"));
  require("node:assert/strict").match(outputId, /^so_[A-Za-z0-9_-]{12}$/);
  require("node:assert/strict").deepEqual(noop, {
    type: "noop", message: "Conformance probes completed",
    ...(assignment && { claim_handle: assignment.claims[0].handle }),
  });
  const saveNoop = () => fs.writeFileSync(process.env.CONFORMANCE_SAFE_OUTPUTS, JSON.stringify(noop));
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

func TestEngineConformanceRequiresOriginalSingletonAssignment(t *testing.T) {
	var suite conformanceSuite
	readConformanceFrontmatter(t, "../../.github/workflows/shared/engine-conformance.md", &suite)
	prepare := conformanceNodeScript(t, suite.PreAgentSteps[0].Run)
	for _, test := range []struct{ name, mutate string }{
		{"missing assignment", `delete process.env.CONFORMANCE_ASSIGNMENT;`},
		{"malformed assignment", `process.env.CONFORMANCE_ASSIGNMENT = "{";`},
		{"legacy scalar assignment", `supplied = { claim_id: "claim", work_id: "work" };`},
		{"unsupported version", `supplied.version = 2;`},
		{"empty assignment", `supplied.claims = [];`},
		{"multiple original claims", `supplied.claims.push({ ...supplied.claims[0], handle: "other", claim_id: "other", work_id: "other" });`},
		{"duplicate original claims", `supplied.claims.push(supplied.claims[0]);`},
		{"invalid handle", `supplied.claims[0].handle = null;`},
		{"unknown authority", `supplied.authorized = true;`},
		{"wrong compiler role", `process.env.GH_AW_WORK_QUEUE_ROLE = "dispatcher";`},
		{"disabled worker", `process.env.GH_AW_WORK_QUEUE_ENABLED = "false";`},
		{"standalone assignment", `process.env.GH_AW_WORK_QUEUE_ENABLED = "false"; delete process.env.GH_AW_WORK_QUEUE_ROLE;`},
		{"wrong worker profile", `supplied.worker_profile = "engine-conformance-codex";`},
		{"wrong work engine", `supplied.claims[0].work.engine = "codex";`},
		{"write effect contract", `supplied.claims[0].work.effect_contract = { kind: "write" };`},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			host := filepath.Join(directory, "host")
			env := append(conformanceQueueEnv(t, "copilot"),
				"CONFORMANCE_ENGINE=copilot", "CONFORMANCE_ROOT="+filepath.Join(directory, "agent"),
				"CONFORMANCE_STATE="+host)
			script := `
let supplied = JSON.parse(process.env.CONFORMANCE_ASSIGNMENT);
` + test.mutate + `
if (process.env.CONFORMANCE_ASSIGNMENT && process.env.CONFORMANCE_ASSIGNMENT !== "{") {
  process.env.CONFORMANCE_ASSIGNMENT = JSON.stringify(supplied);
}
require("node:fs").writeFileSync(process.env.GITHUB_EVENT_PATH, JSON.stringify({
  inputs: { work_queue_assignment: process.env.CONFORMANCE_ASSIGNMENT },
}));
` + prepare
			output, err := runConformanceNode(t, env, "-e", script)
			require.Error(t, err, "%s", output)
			_, statErr := os.Stat(filepath.Join(host, "expected.json"))
			require.True(t, os.IsNotExist(statErr), "invalid assignments must not prepare conformance evidence")
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
		standalone   bool
	}{
		{name: "exact scoped noop", output: `{"type":"noop","message":"Conformance probes completed","claim_handle":"original-agy"}` + "\n", passed: true},
		{name: "standalone exact noop", output: `{"type":"noop","message":"Conformance probes completed"}`, passed: true, standalone: true},
		{name: "standalone unexpected scope", output: `{"type":"noop","message":"Conformance probes completed","claim_handle":"original-agy"}`, standalone: true},
		{name: "unscoped noop", output: `{"type":"noop","message":"Conformance probes completed"}`},
		{name: "foreign scope", output: `{"type":"noop","message":"Conformance probes completed","claim_handle":"foreign"}`},
		{name: "hardcoded scope", output: `{"type":"noop","message":"Conformance probes completed","claim_handle":"h1"}`},
		{name: "null scope", output: `{"type":"noop","message":"Conformance probes completed","claim_handle":null}`},
		{name: "extra scope", output: `{"type":"noop","message":"Conformance probes completed","claim_handle":"original-agy","work_id":"foreign"}`},
		{name: "empty placeholder"},
		{name: "wrong type", output: `{"type":"report_missing_tool","message":"Conformance probes completed","claim_handle":"original-agy"}`},
		{name: "wrong message", output: `{"type":"noop","message":"guessed","claim_handle":"original-agy"}`},
		{name: "duplicate noop", output: `{"type":"noop","message":"Conformance probes completed","claim_handle":"original-agy"}` + "\n" + `{"type":"noop","message":"Conformance probes completed","claim_handle":"original-agy"}`},
	} {
		t.Run(item.name, func(t *testing.T) {
			dir := t.TempDir()
			logPath := filepath.Join(dir, "native.jsonl")
			outputPath := filepath.Join(dir, "outputs.jsonl")
			expected := map[string]string{"fileNonce": "fixture"}
			if !item.standalone {
				expected["claimHandle"] = "original-agy"
			}
			expectedJSON, err := json.Marshal(expected)
			require.NoError(t, err)
			for name, content := range map[string]string{
				"expected.json":                      string(expectedJSON),
				"native-receipt.json":                `{"fileNonce":"fixture","toolNonce":"` + strings.Repeat("a", 48) + `"}`,
				"native-infrastructure-receipt.json": `{"fileNonce":"fixture","toolNonce":"` + strings.Repeat("b", 48) + `"}`,
				"outputs.jsonl":                      item.output,
				"native.jsonl": `{"event":"init","init":{"model":"gemini-3.8-flash-medium"}}
	{"event":"step_update","step_update":{"step_type":"tool","state":"DONE","tool_info":{"name":"call_mcp_tool","parameters":{"ServerName":"agy-native","ToolName":"native_challenge","Arguments":{"file_nonce":"fixture"}},"output":"{\"toolNonce\":\"` + strings.Repeat("a", 48) + `\"}"}}}
	{"event":"step_update","step_update":{"step_type":"tool","state":"DONE","tool_info":{"name":"call_mcp_tool","parameters":{"ServerName":"mcpscripts","ToolName":"native_infrastructure_challenge","Arguments":{"file_nonce":"fixture"}},"output":"{\"toolNonce\":\"` + strings.Repeat("b", 48) + `\"}"}}}
	{"event":"step_update","step_update":{"step_type":"tool","state":"DONE","tool_info":{"name":"call_mcp_tool","parameters":{"ServerName":"safeoutputs","ToolName":"noop","Arguments":{"message":"Conformance probes completed"}},"output":"{}"}}}
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
		{name: "active infrastructure call", mutate: `infrastructure.state = "ACTIVE";`},
		{name: "wrong infrastructure server", mutate: `infrastructure.tool_info.parameters.ServerName = "agy-native";`},
		{name: "wrong infrastructure nonce", mutate: `infrastructure.tool_info.parameters.Arguments.file_nonce = "guessed";`},
		{name: "wrong infrastructure receipt", mutate: `infrastructure.tool_info.output = JSON.stringify({ toolNonce: "guessed" });`},
		{name: "failed infrastructure call", mutate: `infrastructure.tool_info.error = { message: "failed" };`},
		{name: "CLI safe output", mutate: `noop.tool_info.name = "run_command";`},
		{name: "active safe output", mutate: `noop.state = "ACTIVE";`},
		{name: "wrong safe-output message", mutate: `noop.tool_info.parameters.Arguments.message = "guessed";`},
		{name: "failed safe output", mutate: `noop.tool_info.error = { message: "failed" };`},
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
const infrastructureNonce = "b".repeat(48);
const infrastructure = { step_type: "tool", state: "DONE", tool_info: {
  name: "call_mcp_tool",
  parameters: { ServerName: "mcpscripts", ToolName: "native_infrastructure_challenge", Arguments: { file_nonce: "fixture" } },
  output: JSON.stringify({ toolNonce: infrastructureNonce }),
} };
const noop = { step_type: "tool", state: "DONE", tool_info: {
  name: "call_mcp_tool",
  parameters: { ServerName: "safeoutputs", ToolName: "noop", Arguments: { message: "Conformance probes completed" } },
  output: "{}",
} };
` + item.mutate + `
const entries = [
  { event: "init", init: { model: "gemini-3.8-flash-medium" } },
  { event: "step_update", step_update: step },
  { event: "step_update", step_update: infrastructure },
  { event: "step_update", step_update: noop },
  { event: "result", result: { status: "SUCCESS", num_turns: 1, usage: { input_tokens: 10, output_tokens: 2 } } },
];
fs.writeFileSync(path.join(host, "native.jsonl"), entries.map(entry => JSON.stringify(entry)).join("\n"));
fs.writeFileSync(path.join(host, "expected.json"), JSON.stringify({ fileNonce: "fixture", claimHandle: "original-agy" }));
fs.writeFileSync(path.join(host, "native-receipt.json"), JSON.stringify({ fileNonce: "fixture", toolNonce }));
fs.writeFileSync(path.join(host, "native-infrastructure-receipt.json"), JSON.stringify({ fileNonce: "fixture", toolNonce: infrastructureNonce }));
fs.writeFileSync(process.env.CONFORMANCE_SAFE_OUTPUTS, JSON.stringify({ type: "noop", message: "Conformance probes completed", claim_handle: "original-agy" }) + "\n");
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

func TestAgyNativeConformanceChallengeScripts(t *testing.T) {
	var suite conformanceSuite
	readConformanceFrontmatter(t, "../../.github/workflows/shared/agy-conformance.md", &suite)
	for name, receipt := range map[string]string{
		"native-challenge":                "native-receipt.json",
		"native-infrastructure-challenge": "native-infrastructure-receipt.json",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "expected.json"), []byte(`{"fileNonce":"fixture"}`), 0o600))
			tool := suite.MCPScripts[name]
			require.NotEmpty(t, tool.Script)
			encodedReceipt, err := json.Marshal(receipt)
			require.NoError(t, err)
			script := `
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
async function execute(file_nonce) {
` + tool.Script + `
}
(async () => {
  await assert.rejects(execute("wrong"), /nonce does not match/);
  const proof = await execute("fixture");
  assert.match(proof.toolNonce, /^[a-f0-9]{48}$/);
  const receipt = JSON.parse(fs.readFileSync(path.join(process.env.CONFORMANCE_STATE, ` + string(encodedReceipt) + `), "utf8"));
  assert.deepEqual(receipt, { fileNonce: "fixture", toolNonce: proof.toolNonce });
})().catch(error => { console.error(error); process.exitCode = 1; });
`
			out, err := runConformanceNode(t, []string{"CONFORMANCE_STATE=" + dir}, "-e", script)
			require.NoError(t, err, "%s", out)
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
	files = slices.DeleteFunc(files, func(file string) bool {
		return strings.HasSuffix(file, "engine-conformance-dispatcher.md")
	})
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
			budget := 5
			switch id {
			case "claude", "codex", "copilot", "deepseek-harness", "pi":
				budget = 50
			}
			require.EqualValues(t, budget, source["max-ai-credits"])
			if id == "copilot" || id == "pi" {
				require.EqualValues(t, 30, source["max-turns"], "credit headroom must retain the bounded turn limit")
			}
			if id == "codex" {
				require.Equal(t, "openai/gpt-6.1-sol", source["model"], "Codex must select a model whose pricing can be compiled into the firewall configuration")
			}
			on, ok := source["on"].(map[string]any)
			require.True(t, ok)
			require.Contains(t, on, "workflow_dispatch")
			require.Len(t, on, 1, "conformance workers must be dispatch-only")
			lock, err := os.ReadFile(strings.TrimSuffix(file, ".md") + ".lock.yml")
			require.NoError(t, err)
			compiled := string(lock)
			require.Contains(t, compiled, fmt.Sprintf(`GH_AW_MAX_AI_CREDITS: "%d"`, budget), "the firewall must receive the authored credit limit")
			if id == "codex" {
				require.Contains(t, compiled, `"providers":{"openai":{"models":{"gpt-6.1-sol":{"cost":`, "the firewall must receive model-specific Codex pricing")
				require.NotContains(t, compiled, `"defaultAiCreditsPricing"`, "conformance must not guess pricing for unknown models")
			}
			require.Contains(t, compiled, "work_queue_assignment:")
			require.Contains(t, compiled, `GH_AW_WORK_QUEUE_ROLE: "worker"`)
			for _, expected := range []string{
				"Prepare engine conformance fixtures", "Assert engine conformance",
				"Upload engine conformance evidence", "conformance-challenge",
				"ENGINE_CONFORMANCE_SENTINEL: conformance-" + id,
				"CONFORMANCE_EXECUTION: ${{ steps.agentic_execution.outcome }}",
			} {
				require.Contains(t, compiled, expected, "compiled %s is missing %s", id, expected)
			}
			require.NotContains(t, compiled, "github.aw.import-inputs", "import inputs must be resolved")
			require.NotContains(t, compiled, `GH_AW_SAFE_OUTPUTS_STAGED: "true"`, "queue workers must keep Claim settlement live")
			require.Contains(t, compiled, `\"noop\":{\"max\":2,\"report-as-issue\":\"false\"}`, "the conformance noop must not publish test issues")
		})
	}
}
