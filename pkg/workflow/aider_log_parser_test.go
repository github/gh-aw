//go:build !integration && !windows

package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAiderLogParserAndUnifiedSession(t *testing.T) {
	def := loadAiderSample(t)
	require.NotEmpty(t, def.Behaviors.LogParser)
	dir := t.TempDir()
	logPath := filepath.Join(dir, "agent-stdio.log")
	log := `[INFO] Firewall started
{"type":"session.init","timestamp":1000,"data":{"sourceEngine":"aider","sessionId":"test","model":"openai/auto"}}
{"type":"user.message","timestamp":1050,"data":{"sourceEngine":"aider","sessionId":"test","content":"Run the smoke test"}}
{"type":"assistant.message","timestamp":1100,"data":{"sourceEngine":"aider","sessionId":"test","content":"Model reply with smoke results"}}
{"type":"tool.execution_start","timestamp":1200,"data":{"sourceEngine":"aider","sessionId":"test","toolCallId":"success","toolName":"bash","input":{"command":"safeoutputs create_issue --title test --body PASS"}}}
{"type":"tool.execution_complete","timestamp":1300,"data":{"sourceEngine":"aider","sessionId":"test","toolCallId":"success","toolName":"bash","success":true,"exitCode":0,"output":"issue queued","durationMs":100}}
{"type":"tool.execution_start","timestamp":1400,"data":{"sourceEngine":"aider","sessionId":"test","toolCallId":"failure","toolName":"bash","input":{"command":"test missing-file"}}}
{"type":"tool.execution_complete","timestamp":1500,"data":{"sourceEngine":"aider","sessionId":"test","toolCallId":"failure","toolName":"bash","success":false,"exitCode":1,"output":"file missing","durationMs":100}}
{"type":"session.result","timestamp":1600,"data":{"sourceEngine":"aider","sessionId":"test","numTurns":1,"durationMs":600,"errors":[]}}
[aider-harness] process completed
`
	require.NoError(t, os.WriteFile(logPath, []byte(log), 0o600))
	source := def.Behaviors.LogParser + `
const fs = require("fs");
const path = require("path");
const { collectUnifiedSession } = require("./unified_session.cjs");
const parsed = parseLog(fs.readFileSync(process.argv[1], "utf8"));
const unified = collectUnifiedSession({ rootDir: path.dirname(process.argv[1]), engine: "aider" });
process.stdout.write(JSON.stringify({
  parsed,
  events: unified.events.filter(event => event.provenance.component === "agent"),
  unrecognized: unified.events.some(event => event.type === "session.source_warning" && event.data.reason === "unrecognized_engine_log"),
}));
`
	actionsDir, err := filepath.Abs("../../actions/setup/js")
	require.NoError(t, err)
	cmd := exec.Command("node", "-e", source, logPath)
	cmd.Dir = actionsDir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	var result struct {
		Parsed struct {
			Markdown   string
			LogEntries []struct {
				Type string
			}
		}
		Events []struct {
			Type string
			Data struct {
				Success *bool
			}
		}
		Unrecognized bool
	}
	require.NoError(t, json.Unmarshal(output, &result))
	require.Len(t, result.Parsed.LogEntries, 8)
	require.Len(t, result.Events, 8)
	assert.Contains(t, result.Parsed.Markdown, "Model reply with smoke results")
	assert.False(t, result.Unrecognized)
	require.NotNil(t, result.Events[4].Data.Success)
	assert.True(t, *result.Events[4].Data.Success)
	require.NotNil(t, result.Events[6].Data.Success)
	assert.False(t, *result.Events[6].Data.Success)
}

func TestAiderRunBackedSessionPublication(t *testing.T) {
	for _, fixture := range []string{"smoke-success", "downstream-failure", "rate-limit-failure"} {
		t.Run(fixture, func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join("testdata", "aider_session", fixture+".log"))
			require.NoError(t, err)
			checkAiderSessionPublication(t, string(content), fixture != "rate-limit-failure")
		})
	}
}

func TestAiderCanonicalExtensionsAndPartialPublication(t *testing.T) {
	// Synthetic protocol coverage, not claims about fields in sampled Actions runs.
	content := `{"type":"assistant.reasoning","id":"native","parentId":null,"timestamp":0,"data":{"sourceEngine":"aider","content":"  reason\n","partial":true}}
{"type":"assistant.message","timestamp":0,"data":{"sourceEngine":"aider","content":"","partial":false}}
{"type":"assistant.refusal","data":{"sourceEngine":"aider","reason":"refusal","content":"  denied\n","partial":false}}
{"type":"tool.execution_start","data":{"sourceEngine":"aider","toolCallId":"dangling","input":{}}}
{"type":"tool.execution_complete","data":{"sourceEngine":"aider","toolCallId":"orphan","success":false,"output":0,"durationMs":0,"exitCode":1}}
{"type":"vendor.progress","id":"extension","parentId":"","timestamp":0,"native":false,"data":{"content":null,"zero":0,"false":false,"empty":[]}}
{"type":"session.result","data":{"sourceEngine":"aider","numTurns":0,"durationMs":0,"totalCostUsd":0,"usage":{"input_tokens":0,"output_tokens":0,"input_tokens_include_cache":false},"errors":[]}}
`
	checkAiderSessionPublication(t, content, true)
}

func checkAiderSessionPublication(t *testing.T, content string, hasConversation bool) {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "agent-stdio.log")
	require.NoError(t, os.WriteFile(logPath, []byte(content), 0o600))
	source := loadAiderSample(t).Behaviors.LogParser + `
const assert = require("node:assert/strict");
const fs = require("fs");
const path = require("path");
const { runLogParser } = require("./log_parser_bootstrap.cjs");
const { serializeSessionArtifact } = require("./session_artifact.cjs");
const { collectUnifiedSession, mergeSessionSources } = require("./unified_session.cjs");
const { sessionCLI } = require("./session_cli.cjs");
const { isSessionEvent } = require("./agent_session.cjs");
const { parseLogEntries } = require("./log_parser_shared.cjs");
const root = path.dirname(process.argv[1]);
const parsed = parseLog(fs.readFileSync(process.argv[1], "utf8"));
const native = (parseLogEntries(fs.readFileSync(process.argv[1], "utf8")) ?? []).filter(isSessionEvent);
if (native.length) assert.deepEqual(parsed.logEntries, native, "native records must remain exact");
else {
  assert.equal(parsed.logEntries.length, 1);
  assert.equal(parsed.logEntries[0].type, "session.error");
  assert.equal(parsed.logEntries[0].data.errorType, "RateLimitError");
  assert.ok(parsed.logEntries[0].data.content.includes("your rate limit for utility models. Please review our [Terms of\n"));
}
const canonical = parsed.logEntries;
const previousEnv = { ...process.env };
process.env.GH_AW_AGENT_OUTPUT = process.argv[1];
global.core = {
  debug() {}, info() {}, notice() {}, warning() {}, error() {},
  setOutput() {}, exportVariable() {},
  setFailed(message) { throw new Error(message); },
  summary: { addRaw() { return this; }, async write() {} },
};
(async () => {
  try {
    await runLogParser({ parseLog, parserName: "Aider", rootDir: root });
    const persisted = fs.readFileSync(path.join(root, "agent-session.jsonl"), "utf8").trim().split("\n").filter(Boolean).map(JSON.parse);
    assert.deepEqual(persisted.filter(event => event.type !== "agent.execution"), canonical, "Actions persistence must not alter canonical payloads");
    const collected = collectUnifiedSession({ rootDir: root, engine: "aider", warn() {} });
    const agent = collected.events.filter(event => event.provenance.component === "agent");
    const projected = mergeSessionSources([{ component: "agent", phase: "agent", path: "agent-session.jsonl", events: canonical }]);
    assert.deepEqual(agent, projected, "merger must reuse persisted trace without duplicate native snapshots");
    const reconstructed = sessionCLI(["reconstruct", root, "aider"]).trim().split("\n").map(JSON.parse);
    assert.deepEqual(reconstructed.filter(event => event.provenance.component === "agent"), projected, "CLI reconstruction must match Actions merging");
    const final = path.join(root, "aw_session.jsonl");
    const serialized = serializeSessionArtifact(collected.events);
    fs.writeFileSync(final, serialized);
    assert.ok(serialized.endsWith("\n"));
    for (const line of serialized.trimEnd().split("\n")) assert.equal(JSON.stringify(JSON.parse(line)), line);
    if (process.argv[2] === "true") {
      assert.ok(sessionCLI(["markdown", final]).length > 0);
      assert.ok(sessionCLI(["agent-markdown", path.join(root, "agent-session.jsonl"), "aider"]).length > 0);
    } else {
      assert.equal(native.length, 0, "historical prose cannot invent session evidence");
      assert.ok(agent.every(event => event.type === "session.error"), "diagnostics cannot reconstruct conversation, usage or terminal status");
    }
  } finally {
    process.env = previousEnv;
    delete global.core;
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
`
	actionsDir, err := filepath.Abs("../../actions/setup/js")
	require.NoError(t, err)
	cmd := exec.Command("node", "-e", source, logPath, strconv.FormatBool(hasConversation))
	cmd.Dir = actionsDir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
}

func TestAiderParserPreservesUnknownExtensionsWithoutForeignEngine(t *testing.T) {
	source := loadAiderSample(t).Behaviors.LogParser + `
const assert = require("node:assert/strict");
const entries = [
  {type:"vendor.progress", id:"native", data:{value:false}},
  {type:"assistant.message", data:{sourceEngine:"other", content:"foreign"}},
  {type:"session.result", data:{sourceEngine:"aider", numTurns:0}},
  {type:"result", num_turns:99},
];
assert.deepEqual(parseLog(entries.map(JSON.stringify).join("\n")).logEntries, [entries[0], entries[2]]);
`
	actionsDir, err := filepath.Abs("../../actions/setup/js")
	require.NoError(t, err)
	cmd := exec.Command("node", "-e", source)
	cmd.Dir = actionsDir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
}

func TestAiderStartupDiagnosticsRequireNativeAttribution(t *testing.T) {
	source := loadAiderSample(t).Behaviors.LogParser + `
const assert = require("node:assert/strict");
const header = "Aider v0.86.2\nModel: openai/auto with diff edit format\nGit repo: none\nRepo-map: disabled\n\n";
const error = "litellm.RateLimitError: provider unavailable\n  exact wrapped detail \n";
const fence = String.fromCharCode(96).repeat(3);
const supported = parseLog(header + error).logEntries;
assert.deepEqual(supported, [{
  type:"session.error",
  data:{sourceEngine:"aider", errorType:"RateLimitError", content:error},
}]);
for (const content of [
  "", error, header + fence + "text\n" + error + fence + "\n",
  header + "~~~text\n" + error + "~~~\n",
  header + "> " + error, header + '"' + error,
  header + "assistant:\n" + error,
  header + "user:\n" + error,
  header + "tool:\n" + error,
  header + "exec:\n" + error,
  header + "A reply about errors:\n" + error,
  header + "litellm.warning: not a provider failure\n",
]) assert.deepEqual(parseLog(content).logEntries, [], content);
const tool = {type:"tool.execution_complete", data:{sourceEngine:"aider", success:false, output:error}};
assert.deepEqual(parseLog(header + JSON.stringify(tool) + "\n" + error).logEntries, [tool]);
const message = {type:"assistant.message", data:{sourceEngine:"aider", content:error}};
assert.deepEqual(parseLog(header + JSON.stringify(message)).logEntries, [message]);
`
	actionsDir, err := filepath.Abs("../../actions/setup/js")
	require.NoError(t, err)
	cmd := exec.Command("node", "-e", source)
	cmd.Dir = actionsDir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
}
