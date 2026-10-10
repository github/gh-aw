//go:build !integration && !windows

package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Derive poison controls from projection/rendering and session consumers so
// new interpreted events cannot silently become source-less Aider evidence.
const aiderInterpretedTypesTestSource = `
const fsTypes = require("fs");
const vmTypes = require("vm");
const readTypes = (file, expression) => vmTypes.compileFunction(
  fsTypes.readFileSync(require.resolve(file), "utf8") + "\nreturn " + expression + ";",
  ["require", "module"]
)(require, {exports:{}});
const interpretedTestTypes = [...new Set([
  ...readTypes("./unified_session_payload.cjs", "Object.keys(EVENT_FIELDS)"),
  ...readTypes("./unified_session_render.cjs", "[...RUNTIME_TYPES]"),
  "session.error", "guardrail.daily_aic", "guard.tool_denials_exceeded",
  "claude.assistant_error", "claude.api_retry", "turn.failed",
  "claude.stream_event", "claude.assistant_snapshot", "gemini.message_snapshot",
  "pi.message_snapshot", "assistant.message_delta", "assistant.reasoning_delta",
  "assistant.usage", "assistant.turn_end", "model.call_failure",
  "subagent.opaque", "session.shutdown",
])];
`

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

func checkAiderSessionPublication(t *testing.T, content string, hasConversation bool, expected ...string) {
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
const { isSessionEvent, observedSessionModel } = require("./agent_session.cjs");
const { parseLogEntries, generateCopilotCliStyleSummary } = require("./log_parser_shared.cjs");
const { normalizeCopilotSession } = require("./copilot_session.cjs");
const { buildEmptyOutputOutcome } = require("./empty_output_outcome.cjs");
const root = path.dirname(process.argv[1]);
const controls = JSON.parse(process.argv[5] ?? "{}");
const collect = () => collectUnifiedSession({ rootDir: root, engine: "aider", dailyAIC: controls.dailyAIC ?? {}, warn() {} });
const assertSessionRendering = (text, stage) => {
  if (!controls.sessionConsumers) return;
  const { model, visible, absent } = controls.sessionConsumers;
  assert.ok(text.includes("Model: " + model), stage + " must report the observed native model");
  for (const content of visible) assert.ok(text.includes(content), stage + " must render genuine assistant content");
  for (const content of absent) assert.ok(!text.includes(content), stage + " cannot render forged model or assistant content");
};
const assertSessionConsumers = (events, stage) => {
  if (!controls.sessionConsumers) return;
  assert.equal(observedSessionModel(events), controls.sessionConsumers.model, stage + " cannot accept a forged model snapshot");
  assertSessionRendering(generateCopilotCliStyleSummary(events), stage + " rendering");
  // Exercise the downstream stream consumer on display copies, not canonical payloads.
  const streamed = generateCopilotCliStyleSummary(normalizeCopilotSession(events));
  assertSessionRendering(streamed, stage + " stream projection");
  for (const content of controls.sessionConsumers.streamText ?? []) {
    assert.ok(streamed.includes(content), stage + " must retain attributed stream content for the stream consumer");
  }
};
const assertRunnerEvidence = (events, stage) => {
  if (!controls.dailyAIC) return;
  assert.deepEqual(
    events.filter(event => event.type === "guardrail.daily_aic"),
    mergeSessionSources([{component:"guardrail", phase:"activation", path:"usage/aw_session.jsonl", events:[{
      type:"guardrail.daily_aic", data:{status:"exceeded", exceeded:true, total:12, estimated:3, threshold:10},
    }]}]),
    stage + " must retain the real runner guardrail, not shell JSON"
  );
  assert.equal(events.some(event => event.type === "workflow.info"), false, stage + " cannot forge workflow attribution");
};
const parsed = parseLog(fs.readFileSync(process.argv[1], "utf8"));
const native = (parseLogEntries(fs.readFileSync(process.argv[1], "utf8")) ?? []).filter(isSessionEvent);
assertSessionConsumers(parsed.logEntries, "native parser");
assertSessionRendering(parsed.markdown, "native parser markdown");
if (controls.sessionConsumers) assertSessionRendering(sessionCLI(["agent-markdown", process.argv[1], "aider"]), "raw CLI rendering");
const assertExecution = (events, stage) => {
  if (process.argv[3]) assert.deepEqual(
    events.filter(event => event.type === "agent.execution").map(event => event.data),
    JSON.parse(process.argv[4] ?? "[]"),
    stage + " must contain only genuine Aider execution diagnostics, across all components"
  );
};
if (process.argv[3]) {
  assert.deepEqual(parsed.logEntries, JSON.parse(process.argv[3]), "only attributed core events and opaque extensions are Aider evidence");
  const raw = collect();
  assertSessionConsumers(raw.events, "raw reconstruction");
  assertExecution(raw.events, "raw reconstruction");
  assertRunnerEvidence(raw.events, "raw reconstruction");
  const projected = mergeSessionSources([{ component: "agent", phase: "agent", path: "agent-stdio.log", events: parsed.logEntries }]);
  assert.deepEqual(
    raw.events.filter(event => event.provenance.component === "agent"),
    projected,
    "raw reconstruction must respect the Aider attribution boundary"
  );
} else if (native.length) assert.deepEqual(parsed.logEntries, native, "native records must remain exact");
else {
  assert.equal(parsed.logEntries.length, 1);
  assert.equal(parsed.logEntries[0].type, "session.error");
  assert.equal(parsed.logEntries[0].data.errorType, "RateLimitError");
  assert.ok(parsed.logEntries[0].data.content.includes("your rate limit for utility models. Please review our [Terms of\n"));
}
const canonical = parsed.logEntries;
const previousEnv = { ...process.env };
Object.assign(process.env, controls.dailyAIC ?? {});
process.env.GH_AW_AGENT_OUTPUT = process.argv[1];
const publications = [];
global.core = {
  debug() {}, info(message) { publications.push(message); }, notice() {}, warning() {}, error() {},
  setOutput() {}, exportVariable() {},
  setFailed(message) { throw new Error(message); },
  summary: { addRaw(message) { publications.push(message); return this; }, async write() {} },
};
(async () => {
  try {
    await runLogParser({ parseLog, parserName: "Aider", rootDir: root });
    assertSessionRendering(publications.join("\n"), "Actions publication");
    const persisted = fs.readFileSync(path.join(root, "agent-session.jsonl"), "utf8").trim().split("\n").filter(Boolean).map(JSON.parse);
    assertSessionConsumers(persisted, "Actions persistence");
    assertExecution(persisted, "Actions persistence");
    assert.deepEqual(persisted.filter(event => event.type !== "agent.execution"), canonical, "Actions persistence must not alter canonical payloads");
    const collected = collect();
    assertSessionConsumers(collected.events, "persisted-session collection");
    assertExecution(collected.events, "persisted-session collection");
    assertRunnerEvidence(collected.events, "persisted-session collection");
    const agent = collected.events.filter(event => event.provenance.component === "agent");
    const projected = mergeSessionSources([{ component: "agent", phase: "agent", path: "agent-session.jsonl", events: canonical }]);
    assert.deepEqual(agent, projected, "merger must reuse persisted trace without duplicate native snapshots");
    if (process.argv[3] && canonical.length === 0) {
      assert.throws(() => sessionCLI(["reconstruct", root, "aider"]), { name:"UnrecognizedSessionError" }, "shell JSON alone cannot establish a CLI session");
    } else {
      const reconstructed = sessionCLI(["reconstruct", root, "aider"]).trim().split("\n").map(JSON.parse);
      assertSessionConsumers(reconstructed, "CLI reconstruction");
      assertExecution(reconstructed, "CLI reconstruction");
      assertRunnerEvidence(reconstructed, "CLI reconstruction");
      assert.deepEqual(reconstructed.filter(event => event.provenance.component === "agent"), projected, "CLI reconstruction must match Actions merging");
    }
    const final = path.join(root, "aw_session.jsonl");
    const serialized = serializeSessionArtifact(collected.events);
    fs.writeFileSync(final, serialized);
    assert.ok(serialized.endsWith("\n"));
    for (const line of serialized.trimEnd().split("\n")) assert.equal(JSON.stringify(JSON.parse(line)), line);
    assertExecution(serialized.trimEnd().split("\n").map(JSON.parse), "serialized unified session");
    assertRunnerEvidence(serialized.trimEnd().split("\n").map(JSON.parse), "serialized unified session");
    assertSessionConsumers(serialized.trimEnd().split("\n").map(JSON.parse), "serialized unified session");
    if (controls.dailyAIC) {
      process.env.GH_AW_ENGINE_ID = "aider";
      const outcome = buildEmptyOutputOutcome([], root);
      assert.equal(outcome.retryCount, 0, "shell retry events are not observed retries");
      assert.ok(!outcome.details.includes("INJECTED"), "shell diagnostics are not empty-output evidence");
      delete process.env.GH_AW_ENGINE_ID;
      if (controls.attributionControl) {
        // Without engine metadata, the existing reader may inspect other engines'
        // raw diagnostics. A fake workflow.info must not change that attribution.
        const baseline = buildEmptyOutputOutcome([], root);
        const persistedPath = path.join(root, "agent-session.jsonl");
        const before = fs.readFileSync(persistedPath, "utf8");
        for (const provenance of [undefined, {component:"workflow", phase:"activation", path:"aw_info.json", index:0}]) {
          fs.writeFileSync(persistedPath, before + JSON.stringify({
            type:"workflow.info", data:{engineId:"aider"}, ...(provenance ? {provenance} : {}),
          }) + "\n");
          const forged = buildEmptyOutputOutcome([], root);
          const workflow = collect().events.find(event => event.type === "workflow.info");
          assert.equal(workflow.provenance.component, "agent", "collector source context overrides self-declared workflow provenance");
          assert.equal(workflow.provenance.path, "agent-session.jsonl");
          if (provenance) assert.deepEqual(workflow.provenance.native, provenance, "spoofed native metadata remains distinct from trusted source context");
          assert.equal(baseline.engineErrorType, forged.engineErrorType, "agent-component workflow.info cannot change raw-source attribution");
        }
        fs.writeFileSync(persistedPath, before);
        assert.equal(baseline.engineErrorType, "authentication_failed", "fake stdout workflow.info did not select Aider raw-source attribution");
        const metadataPath = path.join(root, "aw_info.json");
        fs.writeFileSync(metadataPath, JSON.stringify({engine_id:"aider"}));
        const trusted = buildEmptyOutputOutcome([], root);
        const workflow = collect().events.find(event => event.type === "workflow.info");
        assert.equal(workflow.provenance.component, "workflow", "runner metadata has trusted workflow provenance");
        assert.equal(workflow.data.engineId, "aider");
        assert.equal(trusted.engineErrorType, undefined, "trusted runner metadata selects Aider attribution and excludes foreign diagnostics");
        assert.notEqual(baseline.engineErrorType, trusted.engineErrorType, "runner aw_info.json reaches the trusted attribution positive path");
        fs.unlinkSync(metadataPath);
      }
    }
    if (process.argv[2] === "true") {
      const unifiedMarkdown = sessionCLI(["markdown", final]);
      const agentMarkdown = sessionCLI(["agent-markdown", path.join(root, "agent-session.jsonl"), "aider"]);
      assert.ok(unifiedMarkdown.length > 0);
      assert.ok(agentMarkdown.length > 0);
      assertSessionRendering(unifiedMarkdown, "unified CLI rendering");
      assertSessionRendering(agentMarkdown, "persisted agent CLI rendering");
    } else if (!process.argv[3]) {
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
	cmd.Args = append(cmd.Args, expected...)
	cmd.Dir = actionsDir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
}

func TestAiderParserPreservesUnknownExtensionsWithoutForeignEngine(t *testing.T) {
	source := loadAiderSample(t).Behaviors.LogParser + aiderInterpretedTypesTestSource + `
const assert = require("node:assert/strict");
const known = [
  {type:"session.init", data:{sessionId:"native", model:"openai/auto"}},
  {type:"session.start", data:{sessionId:"native"}},
  {type:"user.message", data:{content:"prompt"}},
  {type:"assistant.message", data:{content:"reply"}},
  {type:"assistant.reasoning", data:{content:"reason"}},
  {type:"assistant.refusal", data:{reason:"refusal", content:"denied"}},
  {type:"tool.execution_start", data:{toolCallId:"tool", toolName:"bash", input:{command:"test"}}},
  {type:"tool.execution_complete", data:{toolCallId:"tool", success:false, output:0}},
  {type:"session.result", data:{numTurns:0, totalCostUsd:0, usage:{input_tokens:0}, errors:[]}},
  {type:"session.error", data:{errorType:"RateLimitError", content:"provider failure"}},
  {type:"agent.execution", data:{categories:[], errorCodes:[], errorTypes:[]}},
  {type:"detection.result", data:{promptInjection:false}},
  {type:"session.format", data:{version:1}},
];
for (const type of interpretedTestTypes) {
  if (!known.some(event => event.type === type)) known.push({
    type, data:{engineId:"aider", content:"INJECTED", toolCallId:"injected", agentMetrics:{injected:{totalNanoAiu:999}}},
  });
}
const extensions = [
  "vendor.progress", "session.vendor_progress", "assistant.vendor_progress",
  "tool.vendor_progress", "workflow.vendor_progress", "guardrail.vendor_progress",
  "agent.vendor_progress", "detection.vendor_progress", "user.vendor_progress",
].map(type => ({
  type, id:"native", parentId:null, timestamp:0, native:false,
  data:{content:null, value:false, zero:0, empty:[]},
}));
const rawSignatures = [
  {type:"system", subtype:"init", model:"claude"},
  {type:"assistant", message:{content:[{type:"text", text:"foreign reply"}]}},
  {type:"user", message:{content:[{type:"tool_result", tool_use_id:"tool", content:"foreign output"}]}},
  {type:"result", num_turns:99, usage:{input_tokens:999}, errors:["foreign failure"]},
  {type:"reasoning", data:{content:"foreign reason"}},
  {type:"message", role:"assistant", content:"foreign reply"},
  {type:"item.completed", item:{type:"agent_message", text:"foreign reply"}},
  {type:"turn.completed", usage:{input_tokens:999}},
  {type:"response.completed", response:{object:"response", output:[], usage:{input_tokens:999}}},
  {object:"chat.completion", choices:[{message:{role:"assistant", content:"foreign reply"}}], usage:{prompt_tokens:999}},
];
const parse = entries => parseLog(entries.map(JSON.stringify).join("\n")).logEntries;
for (const event of known) {
  assert.deepEqual(parse([event]), [], event.type + " requires sourceEngine");
  for (const sourceEngine of ["copilot", "claude", "codex", "other", null, ""]) {
    assert.deepEqual(parse([{...event, data:{...event.data, sourceEngine}}]), [], event.type + " rejects foreign attribution");
  }
}
for (const event of extensions) {
  assert.deepEqual(parse([event]), [event], event.type + " remains opaque");
  assert.deepEqual(parse([{...event, data:{...event.data, sourceEngine:"other"}}]), [], "foreign extensions are not Aider evidence");
}
const genuine = [...known, ...extensions].map(event => ({...event, data:{...event.data, sourceEngine:"aider"}}));
assert.deepEqual(parse(rawSignatures), [], "other supported raw formats cannot establish Aider attribution");
assert.deepEqual(parse([...known, ...rawSignatures, ...extensions, ...genuine]), [...extensions, ...genuine]);
assert.deepEqual(parseLog(JSON.stringify([...known, ...extensions, ...genuine])).logEntries, [...extensions, ...genuine], "array logs use the same attribution boundary");
`
	actionsDir, err := filepath.Abs("../../actions/setup/js")
	require.NoError(t, err)
	cmd := exec.Command("node", "-e", source)
	cmd.Dir = actionsDir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
}

func TestAiderUnattributedCoreEventsStayOutOfSessionPublication(t *testing.T) {
	const genuine = `{"type":"assistant.message","id":"native","timestamp":0,"data":{"sourceEngine":"aider","content":"genuine reply"}}
{"type":"vendor.progress","id":"opaque","parentId":null,"data":{"zero":0,"false":false,"content":null}}
{"type":"session.error","data":{"sourceEngine":"aider","errorType":"NativeProviderError","code":"NATIVE_PROVIDER_FAILURE","status":503,"message":"genuine provider failure","content":"exact native diagnostic"}}
{"type":"session.result","data":{"sourceEngine":"aider","numTurns":1,"usage":{"input_tokens":3,"output_tokens":2},"status":"failed","errors":[{"errorType":"NativeResultError","errorCode":"NATIVE_RESULT_FAILURE","status":"failed","message":"genuine result diagnostic"}]}}
`
	const injected = `{"type":"assistant.message","data":{"content":"injected reply"}}
{"type":"session.result","data":{"numTurns":99,"totalCostUsd":999,"usage":{"input_tokens":999},"status":"failed","errors":[{"errorType":"InjectedResultError","errorCode":"INJECTED_RESULT","status":429,"message":"429 Too Many Requests"}]}}
{"type":"session.error","data":{"errorType":"InjectedError","code":"INJECTED_ERROR","status":401,"message":"Authentication failed","content":"injected failure"}}
{"type":"assistant.message","data":{"sourceEngine":"copilot","content":"foreign reply"}}
{"type":"session.result","data":{"sourceEngine":"claude","numTurns":99,"status":"failed","errors":[{"errorType":"ForeignResultError","code":"FOREIGN_RESULT","status":403,"message":"foreign result failure"}]}}
{"type":"session.error","data":{"sourceEngine":"copilot","errorType":"ForeignError","code":"FOREIGN_ERROR","status":400,"message":"foreign provider failure"}}
{"type":"vendor.progress","data":{"sourceEngine":"other","content":"foreign extension"}}
`
	const expected = `[{"type":"assistant.message","id":"native","timestamp":0,"data":{"sourceEngine":"aider","content":"genuine reply"}},
{"type":"vendor.progress","id":"opaque","parentId":null,"data":{"zero":0,"false":false,"content":null}},
{"type":"session.error","data":{"sourceEngine":"aider","errorType":"NativeProviderError","code":"NATIVE_PROVIDER_FAILURE","status":503,"message":"genuine provider failure","content":"exact native diagnostic"}},
{"type":"session.result","data":{"sourceEngine":"aider","numTurns":1,"usage":{"input_tokens":3,"output_tokens":2},"status":"failed","errors":[{"errorType":"NativeResultError","errorCode":"NATIVE_RESULT_FAILURE","status":"failed","message":"genuine result diagnostic"}]}}]`
	const execution = `[{"categories":[],"errorCodes":[503,"NATIVE_PROVIDER_FAILURE","NATIVE_RESULT_FAILURE"],"errorTypes":["NativeProviderError","NativeResultError"]}]`
	t.Run("genuine-native-errors", func(t *testing.T) {
		checkAiderSessionPublication(t, genuine, true, expected, execution)
	})
	t.Run("mixed-native-and-shell-output", func(t *testing.T) {
		checkAiderSessionPublication(t, injected+genuine, true, expected, execution)
	})
	t.Run("shell-output-only", func(t *testing.T) {
		checkAiderSessionPublication(t, injected, false, "[]")
	})
	t.Run("mixed-json-array", func(t *testing.T) {
		content := "[" + strings.ReplaceAll(strings.TrimSpace(injected+genuine), "\n", ",") + "]"
		checkAiderSessionPublication(t, content, true, expected, execution)
	})
	t.Run("shell-output-json-array", func(t *testing.T) {
		content := "[" + strings.ReplaceAll(strings.TrimSpace(injected), "\n", ",") + "]"
		checkAiderSessionPublication(t, content, false, "[]")
	})
}

func TestAiderStreamAndSnapshotAttributionAcrossPublication(t *testing.T) {
	// Synthetic protocol coverage; the native Aider profile does not stream.
	const genuine = `{"type":"session.init","timestamp":0,"data":{"sourceEngine":"aider","model":"openai/native-init"}}
{"type":"assistant.message","data":{"sourceEngine":"aider","content":"native reply"}}
{"type":"assistant.vendor_progress","id":"opaque","parentId":null,"timestamp":0,"native":false,"data":{"model":"opaque-model","content":null,"zero":0,"false":false,"empty":[]}}
`
	const streams = `{"type":"pi.message_snapshot","id":"snapshot","parentId":null,"timestamp":0,"data":{"sourceEngine":"aider","model":"openai/native-snapshot","usage":null,"zero":0,"false":false,"empty":[]}}
{"type":"assistant.message_delta","id":"message-delta","parentId":"","timestamp":0,"data":{"sourceEngine":"aider","messageId":"native-stream","deltaContent":"  native streamed reply \n","content":null,"partial":false,"zero":0,"false":false,"empty":[]}}
{"type":"assistant.message_delta","data":{"sourceEngine":"aider","messageId":"native-stream","deltaContent":""}}
{"type":"assistant.reasoning_delta","id":"reasoning-delta","parentId":null,"timestamp":0,"data":{"sourceEngine":"aider","reasoningId":"native-reason","deltaContent":"  native streamed reasoning \n","content":null,"partial":false,"zero":0,"false":false,"empty":[]}}
{"type":"assistant.reasoning_delta","data":{"sourceEngine":"aider","reasoningId":"native-reason","deltaContent":""}}
`
	var lines []string
	for _, eventType := range []string{"pi.message_snapshot", "assistant.message_delta", "assistant.reasoning_delta"} {
		for _, sourceEngine := range []any{nil, "copilot", "claude", "codex", "other", "", false, 0} {
			data := map[string]any{
				"model":        "INJECTED_MODEL",
				"messageId":    "forged-message",
				"reasoningId":  "forged-reason",
				"deltaContent": "INJECTED_STREAM",
			}
			event := map[string]any{"type": eventType, "data": data}
			if sourceEngine == nil {
				lines = append(lines, string(mustMarshalAiderTestJSON(t, event)))
			}
			data["sourceEngine"] = sourceEngine
			lines = append(lines, string(mustMarshalAiderTestJSON(t, event)))
		}
	}
	injected := strings.Join(lines, "\n") + "\n"
	for _, tc := range []struct {
		name       string
		canonical  string
		model      string
		streamText []string
	}{
		{"initialization", genuine, "openai/native-init", nil},
		{"native-streams-and-snapshot", genuine + streams, "openai/native-snapshot", []string{"native streamed reply", "native streamed reasoning"}},
	} {
		expected := "[" + strings.ReplaceAll(strings.TrimSpace(tc.canonical), "\n", ",") + "]"
		controls := string(mustMarshalAiderTestJSON(t, map[string]any{
			"sessionConsumers": map[string]any{
				"model":      tc.model,
				"visible":    []string{"native reply"},
				"absent":     []string{"INJECTED_MODEL", "INJECTED_STREAM"},
				"streamText": tc.streamText,
			},
		}))
		t.Run(tc.name+"/native-control", func(t *testing.T) {
			checkAiderSessionPublication(t, tc.canonical, true, expected, "[]", controls)
		})
		// Put every forged snapshot after native initialization and snapshots.
		t.Run(tc.name+"/mixed-jsonl", func(t *testing.T) {
			checkAiderSessionPublication(t, tc.canonical+injected, true, expected, "[]", controls)
		})
		t.Run(tc.name+"/mixed-json-array", func(t *testing.T) {
			content := "[" + strings.ReplaceAll(strings.TrimSpace(tc.canonical+injected), "\n", ",") + "]"
			checkAiderSessionPublication(t, content, true, expected, "[]", controls)
		})
	}
}

func TestAiderRuntimeAccountingRequiresAttribution(t *testing.T) {
	// Synthetic interoperability controls, not observed Aider usage events.
	source := loadAiderSample(t).Behaviors.LogParser + `
const assert = require("node:assert/strict");
const { normalizeCopilotSession } = require("./copilot_session.cjs");
const start = {type:"session.init", data:{sourceEngine:"aider", model:"openai/native"}};
const records = [
  {type:"assistant.usage", data:{apiCallId:"native-call", inputTokens:0, outputTokens:0}},
  {type:"assistant.turn_end", data:{turnId:"native-turn"}},
  {type:"model.call_failure", data:{error:{code:429, message:"native failure"}}},
];
for (const sourceEngine of [undefined, null, "copilot", "", false, 0]) {
  const untrusted = records.map(record => ({
    ...record, data:{...record.data, ...(sourceEngine === undefined ? {} : {sourceEngine})},
  }));
  const parsed = parseLog([start, ...untrusted].map(JSON.stringify).join("\n")).logEntries;
  assert.deepEqual(parsed, [start], "unattributed accounting and error events are not Aider evidence");
  assert.deepEqual(normalizeCopilotSession(parsed).filter(event => event.type === "session.result"), []);
}
const native = [start, ...records.map(record => ({...record, data:{...record.data, sourceEngine:"aider"}}))];
const parsed = parseLog(native.map(JSON.stringify).join("\n")).logEntries;
assert.deepEqual(parsed, native, "attributed extension payloads remain exact");
const results = normalizeCopilotSession(parsed).filter(event => event.type === "session.result");
assert.ok(results.some(event => event.data.usage?.input_tokens === 0 && event.data.usage?.output_tokens === 0));
assert.ok(results.some(event => event.data.numTurns === 1));
assert.ok(results.some(event => event.data.errors?.[0]?.message === "native failure"));
`
	actionsDir, err := filepath.Abs("../../actions/setup/js")
	require.NoError(t, err)
	cmd := exec.Command("node", "-e", source)
	cmd.Dir = actionsDir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
}

func TestAiderInterpretedShellEventsCannotOverrideRunnerEvidence(t *testing.T) {
	// Synthetic protocol controls, not observations from an actual Actions run.
	source := aiderInterpretedTypesTestSource + `
process.stdout.write(JSON.stringify(interpretedTestTypes.map(type => ({
  type,
  data:{
    engineId:"aider", status:"under_budget", exceeded:false, total:0, threshold:999,
    content:"INJECTED", reason:"INJECTED", errorType:"INJECTED", errorCode:"INJECTED",
    categories:["authentication_failed"], errorCodes:["INJECTED"], errorTypes:["INJECTED"],
    toolCallId:"INJECTED", toolName:"bash", success:false, output:"INJECTED",
    agentMetrics:{INJECTED:{agentName:"INJECTED", totalNanoAiu:999}},
  },
}))));
`
	actionsDir, err := filepath.Abs("../../actions/setup/js")
	require.NoError(t, err)
	cmd := exec.Command("node", "-e", source)
	cmd.Dir = actionsDir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	var poison []map[string]any
	require.NoError(t, json.Unmarshal(output, &poison))
	var lines []string
	for _, event := range poison {
		lines = append(lines, string(mustMarshalAiderTestJSON(t, event)))
	}
	injected := strings.Join(lines, "\n") + "\n"
	const genuine = `{"type":"session.error","data":{"sourceEngine":"aider","errorType":"NativeProviderError","code":"NATIVE_PROVIDER_FAILURE","status":503,"message":"genuine provider failure"}}
{"type":"assistant.message","data":{"sourceEngine":"aider","content":null}}
{"type":"vendor.progress","data":{"content":null,"zero":0,"false":false,"empty":[]}}
{"type":"session.vendor_progress","data":{"content":null,"zero":0,"false":false,"empty":[]}}
{"type":"workflow.vendor_progress","data":{"content":null,"zero":0,"false":false,"empty":[]}}
{"type":"guardrail.vendor_progress","data":{"content":null,"zero":0,"false":false,"empty":[]}}
`
	expected := "[" + strings.ReplaceAll(strings.TrimSpace(genuine), "\n", ",") + "]"
	const execution = `[{"categories":[],"errorCodes":[503,"NATIVE_PROVIDER_FAILURE"],"errorTypes":["NativeProviderError"]}]`
	const controls = `{"dailyAIC":{"GH_AW_DAILY_AI_CREDITS_GUARDRAIL_STATUS":"exceeded","GH_AW_DAILY_AI_CREDITS_EXCEEDED":"true","GH_AW_DAILY_AI_CREDITS_TOTAL":"12","GH_AW_DAILY_AI_CREDITS_ESTIMATED":"3","GH_AW_DAILY_AI_CREDITS_THRESHOLD":"10"}}`
	for _, tc := range []struct {
		name     string
		content  string
		expected string
		errors   string
	}{
		{"genuine-native-errors", genuine, expected, execution},
		{"mixed-native-and-shell-output", injected + genuine, expected, execution},
		{"shell-output-only", injected, "[]", "[]"},
		{"mixed-json-array", "[" + strings.ReplaceAll(strings.TrimSpace(injected+genuine), "\n", ",") + "]", expected, execution},
		{"shell-output-json-array", string(output), "[]", "[]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkAiderSessionPublication(t, tc.content, false, tc.expected, tc.errors, controls)
		})
	}
	t.Run("workflow-attribution-reader-control", func(t *testing.T) {
		const foreignRawError = `{"type":"session.error","data":{"sourceEngine":"copilot","message":"Authentication failed"}}
`
		checkAiderSessionPublication(t, injected+genuine+foreignRawError, false, expected, execution,
			`{"attributionControl":true,"dailyAIC":{"GH_AW_DAILY_AI_CREDITS_GUARDRAIL_STATUS":"exceeded","GH_AW_DAILY_AI_CREDITS_EXCEEDED":"true","GH_AW_DAILY_AI_CREDITS_TOTAL":"12","GH_AW_DAILY_AI_CREDITS_ESTIMATED":"3","GH_AW_DAILY_AI_CREDITS_THRESHOLD":"10"}}`)
	})
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
