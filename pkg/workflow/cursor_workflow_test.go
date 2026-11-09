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

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func loadCursorSample(t *testing.T) *EngineDefinition {
	t.Helper()
	content, err := os.ReadFile("../../.github/workflows/shared/cursor.md")
	require.NoError(t, err)
	parts := strings.SplitN(string(content), "---", 3)
	require.Len(t, parts, 3)
	var frontmatter struct {
		Engine EngineDefinition `yaml:"engine"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(parts[1]), &frontmatter))
	require.NotNil(t, frontmatter.Engine.Behaviors)
	require.NotNil(t, frontmatter.Engine.Behaviors.MCP)
	return &frontmatter.Engine
}

func TestCursorHarness(t *testing.T) {
	def := loadCursorSample(t)
	scriptPath := filepath.Join(t.TempDir(), "cursor_harness.cjs")
	require.NoError(t, os.WriteFile(scriptPath, []byte(def.Behaviors.HarnessScript), 0o600))
	for _, scenario := range []string{
		"x64", "arm64", "secret-fallback", "no-model", "unsupported-arch",
		"checksum", "download", "extraction", "missing-binary", "verification", "execution", "missing-prompt",
	} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "node", "-e", cursorHarnessTestDriver, scriptPath, scenario)
			cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", output)
		})
	}
}

const cursorHarnessTestDriver = `
const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");
const [scriptPath, scenario] = process.argv.slice(1);
const calls = [];
const logs = [];
let cleaned = false;
const prompt = "--api-key not-an-option\n$(touch /not-executed)";
const apiKey = "fixture-cursor-key";
const arch = scenario === "arm64" ? "arm64" : scenario === "unsupported-arch" ? "riscv64" : "x64";
const checksum = arch === "arm64"
  ? "cbba40d90d7f699874e133a5c15bf3854cee006565bd3bb00a6c79b49d62b0d0"
  : "6f6bf39dd2df29dcd38643cdfab0155ce71699953889f732deec123f6562bb2d";
const env = {CURSOR_API_KEY: apiKey, CURSOR_MODEL: "cursor/auto", GH_AW_PROMPT: "/prompt.md"};
if (scenario === "secret-fallback") {
  delete env.CURSOR_API_KEY;
  env.SECRET_CURSOR_API_KEY = apiKey;
}
if (scenario === "no-model") delete env.CURSOR_MODEL;
if (scenario === "missing-prompt") delete env.GH_AW_PROMPT;
const mockFS = {
  mkdtempSync: () => "/tmp/cursor-fixture",
  existsSync: () => scenario !== "missing-binary",
  readFileSync: path => {
    if (path === "/prompt.md") return prompt;
    assert.equal(path, "/tmp/cursor-fixture/cursor-agent.tar.gz");
    return Buffer.from("fixture-archive");
  },
  rmSync: (path, options) => {
    assert.equal(path, "/tmp/cursor-fixture");
    assert.equal(options.recursive, true);
    cleaned = true;
  },
};
const spawnSync = (command, args, options) => {
  assert.equal(options.shell, undefined);
  assert.equal(options.stdio, "inherit");
  calls.push({command, args, options});
  if (scenario === "download" && command === "curl") return {status: 22};
  if (scenario === "extraction" && command === "tar") return {status: 2};
  if (scenario === "verification" && args[0] === "--version") return {status: 9};
  if (scenario === "execution" && args[0] === "-p") return {status: 7};
  return {status: 0};
};
const mockProcess = {
  argv: ["node", scriptPath, "cursor-agent", "-p", "--force", "--trust", "--approve-mcps", "--output-format", "stream-json"],
  env, arch, stderr: {write: text => logs.push(text)},
};
vm.runInNewContext(fs.readFileSync(scriptPath, "utf8"), {
  require: name => {
    if (name === "fs") return mockFS;
    if (name === "child_process") return {spawnSync};
    if (name === "crypto") return {createHash: algorithm => {
      assert.equal(algorithm, "sha256");
      return {update: data => {
        assert.equal(data.toString(), "fixture-archive");
        return {digest: encoding => {
          assert.equal(encoding, "hex");
          return scenario === "checksum" ? "bad-checksum" : checksum;
        }};
      }};
    }};
    return require(name);
  },
  process: mockProcess,
});
assert.equal(cleaned, true);
const output = logs.join("");
assert.equal(output.includes(prompt), false);
assert.equal(output.includes(apiKey), false);
const failures = {
  "unsupported-arch": [0, "Unsupported Cursor Agent architecture"],
  "checksum": [1, "checksum did not match"],
  "download": [1, "download failed with exit code 22"],
  "extraction": [2, "extraction failed with exit code 2"],
  "missing-binary": [2, "executable was not found"],
  "verification": [3, "verification failed with exit code 9"],
  "execution": [4, "execution failed with exit code 7"],
  "missing-prompt": [3, "GH_AW_PROMPT is required"],
};
if (failures[scenario]) {
  const [count, message] = failures[scenario];
  assert.equal(mockProcess.exitCode, 1);
  assert.equal(calls.length, count);
  assert.ok(output.includes(message), output);
} else {
  assert.equal(mockProcess.exitCode || 0, 0);
  assert.equal(calls.length, 4);
  assert.equal(calls[0].command, "curl");
  assert.equal(calls[0].args.at(-1),
    "https://downloads.cursor.com/lab/2026.10.14-941492e/linux/" + arch + "/agent-cli-package.tar.gz");
  assert.equal(calls[1].command, "tar");
  assert.deepEqual(Array.from(calls[1].args), ["-xzf", "/tmp/cursor-fixture/cursor-agent.tar.gz", "-C", "/tmp/cursor-fixture"]);
  assert.equal(calls[2].command, "/tmp/cursor-fixture/dist-package/cursor-agent");
  assert.deepEqual(Array.from(calls[2].args), ["--version"]);
  assert.equal(calls[3].command, calls[2].command);
  assert.deepEqual(Array.from(calls[3].args), [
    "-p", "--force", "--trust", "--approve-mcps", "--output-format", "stream-json",
    ...(scenario === "no-model" ? [] : ["--model", "auto"]), "--", prompt,
  ]);
  assert.equal(calls[3].options.env.CURSOR_API_KEY, apiKey);
}
`

func TestCursorMCPAdapter(t *testing.T) {
	dir := t.TempDir()
	gatewayPath := filepath.Join(dir, "gateway.json")
	adapterPath := filepath.Join(dir, "adapter.cjs")
	require.NoError(t, os.WriteFile(adapterPath, []byte(loadCursorSample(t).Behaviors.MCP.ConfigAdapter), 0o600))
	gateway := `{"mcpServers":{
		"github":{"type":"http","url":"http://localhost:8080/mcp/github","headers":{"Authorization":"fixture-secret"},"tools":["search"]},
		"safeoutputs":{"url":"http://localhost:8080/mcp/safeoutputs"},
		"local":{"command":"node","args":["server.cjs"]}
	}}`
	require.NoError(t, os.WriteFile(gatewayPath, []byte(gateway), 0o600))
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", adapterPath)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"), "GITHUB_WORKSPACE=" + dir,
		"MCP_GATEWAY_OUTPUT=" + gatewayPath, "MCP_GATEWAY_PORT=8080",
		`GH_AW_MCP_CLI_SERVERS=["safeoutputs"]`,
	}
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	assert.Empty(t, output, "adapter must not print MCP credentials")
	configPath := filepath.Join(dir, ".cursor", "mcp.json")
	content, err := os.ReadFile(configPath)
	require.NoError(t, err)
	var config struct {
		Servers map[string]map[string]any `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(content, &config))
	require.Len(t, config.Servers, 2)
	github := config.Servers["github"]
	assert.Equal(t, "http://host.docker.internal:8080/mcp/github", github["url"])
	assert.Equal(t, map[string]any{"Authorization": "fixture-secret"}, github["headers"])
	assert.NotContains(t, github, "type")
	assert.NotContains(t, github, "tools")
	assert.Equal(t, "node", config.Servers["local"]["command"])
	info, err := os.Stat(configPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestCursorNativeSessionParser(t *testing.T) {
	def := loadCursorSample(t)
	assert.Equal(t, "stream-json", def.Behaviors.Execution.Args[len(def.Behaviors.Execution.Args)-1])
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "-e", def.Behaviors.LogParser+cursorParserTestDriver, t.TempDir())
	cmd.Dir = "../../actions/setup/js"
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	var events any
	require.NoError(t, json.Unmarshal(output, &events))
	schemaJSON, err := os.ReadFile("../../docs/public/schemas/agent-session.schema.json")
	require.NoError(t, err)
	var definition any
	require.NoError(t, json.Unmarshal(schemaJSON, &definition))
	compiler := jsonschema.NewCompiler()
	const location = "https://github.github.com/gh-aw/schemas/agent-session.schema.json"
	require.NoError(t, compiler.AddResource(location, definition))
	schema, err := compiler.Compile(location)
	require.NoError(t, err)
	require.NoError(t, schema.Validate(events), "real Cursor adapter output must satisfy the existing canonical schema")
}

const cursorParserTestDriver = `
const assert = require("node:assert/strict");
const { normalizeAgentSession } = require("./agent_session.cjs");
const parserEvents = [];
const originalParseLog = parseLog;
parseLog = content => {
  const parsed = originalParseLog(content);
  parserEvents.push(...parsed.logEntries);
  return parsed;
};
const init = {type: "system", subtype: "init", session_id: "session-1", model: "Auto", cwd: "/workspace", apiKeySource: "env"};
const user = {type: "user", session_id: "session-1", message: {role: "user", content: [{type: "text", text: "Read the file"}]}};
const assistant = {type: "assistant", session_id: "session-1", id: "message-1", message: {role: "assistant", content: [{type: "text", text: "Reading."}]}};
const start = {type: "tool_call", subtype: "started", session_id: "session-1", call_id: "read-1",
  tool_call: {readToolCall: {args: {path: "file.txt"}}}};
const complete = {type: "tool_call", subtype: "completed", session_id: "session-1", call_id: "read-1",
  tool_call: {readToolCall: {args: {path: "file.txt"}, result: {success: {content: "file contents", totalLines: 1, isEmpty: false}}}}};
const result = {type: "result", subtype: "success", is_error: false, duration_ms: 1234,
  duration_api_ms: 1200, result: "Done", session_id: "session-1", request_id: "request-1"};
const input = [init, user, assistant, start, complete, result].map(JSON.stringify).join("\n");
const events = parseLog("2026.10.14-941492e\n[INFO] infrastructure\n" + input).logEntries;
assert.deepEqual(events.map(event => event.type), [
  "session.init", "user.message", "assistant.message", "tool.execution_start",
  "tool.execution_complete", "session.result", "assistant.message", "cursor.diagnostic",
]);
assert.equal(events[0].data.model, "Auto");
assert.equal(events[0].data.sessionId, "session-1");
assert.equal(events[0].data.apiKeySource, "env");
assert.equal(events[1].data.content, "Read the file");
assert.equal(events[2].id, "message-1");
assert.equal(events[2].data.content, "Reading.");
assert.equal(events[3].data.toolCallId, "read-1");
assert.equal(events[3].data.toolName, "readToolCall");
assert.deepEqual(events[3].data.input, {path: "file.txt"});
assert.equal(events[4].data.toolCallId, "read-1");
assert.equal(events[4].data.success, true);
assert.deepEqual(events[4].data.output, complete.tool_call.readToolCall.result);
assert.equal(events[5].data.durationMs, 1234);
assert.equal(events[5].data.request_id, "request-1");
assert.equal(events[5].data.is_error, false);
assert.equal(Object.hasOwn(events[5].data, "usage"), false);
assert.equal(Object.hasOwn(events[5].data, "numTurns"), false);
assert.deepEqual(normalizeAgentSession(events), events);
const unknownInit = parseLog(JSON.stringify({type: "system", subtype: "init"})).logEntries[0];
assert.equal(Object.hasOwn(unknownInit.data, "model"), false);
assert.equal(Object.hasOwn(unknownInit.data, "sessionId"), false);
const missing = parseLog(JSON.stringify({type: "tool_call", subtype: "completed", tool_call: {shellToolCall: {}}})).logEntries[0];
for (const key of ["toolCallId", "input", "output", "success"]) assert.equal(Object.hasOwn(missing.data, key), false, key);
const failure = parseLog(JSON.stringify({type: "tool_call", subtype: "completed", call_id: "bad",
  tool_call: {function: {name: "mcp_github_search", arguments: {query: "test"}, result: {error: "denied"}}}})).logEntries[0];
assert.equal(failure.data.toolName, "mcp_github_search");
assert.deepEqual(failure.data.input, {query: "test"});
assert.equal(failure.data.success, false);
const zero = parseLog(JSON.stringify({...result, num_turns: 0, usage: {input_tokens: 0}})).logEntries[0];
assert.equal(zero.data.numTurns, 0);
assert.equal(zero.data.usage.input_tokens, 0);
const extension = {type: "cursor.extension", id: "saved", data: {nativeExtra: [false, 0, null]}};
assert.deepEqual(parseLog(JSON.stringify(extension)).logEntries, [extension]);
const future = {type: "new_native_event", nativeExtra: [false, 0, null]};
assert.deepEqual(parseLog(JSON.stringify(future)).logEntries[0].data.nativeExtra, [false, 0, null]);
const prose = "Running bash is only prose.\nTool read is not an observed call.\nturn limit is quoted.";
assert.deepEqual(parseLog(prose).logEntries, [{type: "assistant.message", data: {sourceEngine: "cursor", content: prose}}]);
const legacy = parseLog(prose + '\n{"type":"result","num_turns":1,"usage":{"output_tokens":0}}').logEntries;
assert.equal(legacy.find(event => event.type === "assistant.message").data.content, prose);
assert.equal(legacy.find(event => event.type === "session.result").data.numTurns, 1);
assert.equal(legacy.some(event => event.type.startsWith("tool.")), false);
assert.equal(parseLog(prose).maxTurnsHit, false);
assert.deepEqual(parseLog("").logEntries, []);
const malformed = parseLog(input + '\n{"private":"do-not-copy"');
assert.deepEqual(malformed.logEntries.at(-1), {
  type: "session.collection_warning",
  data: {sourceEngine: "cursor", code: "malformed_jsonl", line: 7},
});
assert.equal(malformed.markdown.includes("**Parse errors:** 1"), true);
assert.equal(JSON.stringify(malformed.logEntries).includes("do-not-copy"), false);
assert.equal(malformed.logEntries.filter(event => event.type === "session.result").length, 1);
const backtick = String.fromCharCode(96);
for (const example of [
  '{"answer":42}',
  JSON.stringify(start),
  JSON.stringify(init),
  '{"type":"result","num_turns":999}',
  '{"private":"do-not-copy"',
]) {
  for (const [open, close] of [["~~~json", "~~~"], [backtick.repeat(3) + "json", backtick.repeat(3)], [backtick.repeat(4) + "json", backtick.repeat(4)]]) {
    const answer = "Example only.\n\n" + open + "\n" + example + "\n" + close + "\n\nNo tool ran.";
    const parsed = parseLog(answer);
    assert.deepEqual(parsed.logEntries, [{type: "assistant.message", data: {sourceEngine: "cursor", content: answer}}]);
    assert.equal(parsed.markdown, "**Observed tool starts:** 0");
    assert.deepEqual(parsed.mcpFailures, []);
    const withTrailer = parseLog(answer + '\n{"type":"result","num_turns":1}');
    assert.equal(withTrailer.logEntries.find(event => event.type === "assistant.message").data.content, answer);
    assert.equal(withTrailer.logEntries.find(event => event.type === "session.result").data.numTurns, 1);
    assert.equal(withTrailer.logEntries.length, 2);
  }
}
for (const answer of [
  '{"answer":42}',
  'An unfenced example:\n' + JSON.stringify(start),
  'A malformed example:\n{"private":"do-not-copy"',
]) {
  assert.deepEqual(parseLog(answer).logEntries, [{type: "assistant.message", data: {sourceEngine: "cursor", content: answer}}]);
}
const rejected = parseLog([
  "[INFO] infrastructure", "2026.10.14-941492e", '{"private":"do-not-copy"',
  JSON.stringify(init), '{"private":"do-not-copy"}', '{"type":42,"private":"do-not-copy"}',
  JSON.stringify(result),
].join("\n"));
assert.deepEqual(rejected.logEntries.filter(event => event.type === "session.collection_warning").map(event => event.data), [
  {sourceEngine: "cursor", code: "malformed_jsonl", line: 3},
  {sourceEngine: "cursor", code: "malformed_cursor_event", line: 5},
  {sourceEngine: "cursor", code: "malformed_cursor_event", line: 6},
]);
assert.equal(rejected.markdown.includes("**Parse errors:** 3"), true);
assert.equal(JSON.stringify(rejected.logEntries).includes("do-not-copy"), false);
assert.equal(rejected.logEntries.at(-1).data.content, "2026.10.14-941492e");
const rootDir = process.argv[1];
require("node:fs").writeFileSync(require("node:path").join(rootDir, "agent-session.jsonl"),
  rejected.logEntries.map(JSON.stringify).join("\n"));
const collected = require("./unified_session.cjs").collectUnifiedSession({rootDir, engine: "cursor", dailyAIC: {}, warn: () => {}});
assert.equal(collected.events.find(event => event.type === "session.collection").data.warnings, 3);
const rendered = require("./log_parser_shared.cjs").generateCopilotCliStyleSummary(collected.events);
assert.equal(rendered.includes("malformed_jsonl"), true);
assert.equal(rendered.includes("malformed_cursor_event"), true);
assert.equal(rendered.includes("do-not-copy"), false);
for (const diagnostic of [
  "MCP server github failed",
  "MCP server 'github' failed",
  'MCP server "github" failed',
  "MCP connection error",
  "Failed to connect to MCP",
]) {
  const parsed = parseLog(JSON.stringify(init) + "\n" + diagnostic + "\n" + diagnostic);
  assert.deepEqual(parsed.mcpFailures, [diagnostic.startsWith("MCP server") ? "github" : diagnostic]);
  assert.equal(parsed.logEntries.at(-1).type, "cursor.diagnostic");
  assert.equal(parsed.logEntries.some(event => event.type === "session.result"), false);
  assert.deepEqual(parseLog(diagnostic).mcpFailures, []);
  assert.deepEqual(parseLog("Example:\n~~~text\n" + diagnostic + "\n~~~").mcpFailures, []);
  const quoted = {...assistant, message: {role: "assistant", content: [{type: "text", text: diagnostic}]}};
  assert.deepEqual(parseLog([init, quoted, {...result, result: diagnostic}].map(JSON.stringify).join("\n")).mcpFailures, []);
}
assert.deepEqual(parseLog(JSON.stringify(init) + "\nMCP server github connected").mcpFailures, []);
const liveTimestamp = 1793526675866;
const liveText = "Quoted \"text\", backslash \\, newline\nand Unicode \u2603";
const liveStart = {...start, timestamp_ms: liveTimestamp, model_call_id: "model-call-1",
  tool_call: {...start.tool_call, hookAdditionalContexts: [], toolCallId: "read-1", startedAtMs: String(liveTimestamp)}};
const liveComplete = {...complete, timestamp_ms: liveTimestamp + 1, model_call_id: "model-call-1",
  tool_call: {...complete.tool_call, hookAdditionalContexts: [{context: "metadata, not a tool"}],
    toolCallId: "read-1", startedAtMs: String(liveTimestamp), completedAtMs: String(liveTimestamp + 1)}};
const liveResult = {...result, result: liveText, usage: {inputTokens: 16565, outputTokens: 1129, cacheReadTokens: 95360, cacheWriteTokens: 0}};
const live = parseLog([
  init,
  {type: "thinking", subtype: "delta", text: liveText, session_id: "session-1", timestamp_ms: liveTimestamp - 2},
  {type: "thinking", subtype: "completed", session_id: "session-1", timestamp_ms: liveTimestamp - 1},
  {...assistant, timestamp_ms: liveTimestamp, model_call_id: "model-call-1"},
  liveStart, liveComplete, liveResult,
].map(JSON.stringify).join("\n")).logEntries;
assert.deepEqual(live.map(event => event.type), [
  "session.init", "assistant.reasoning", "assistant.reasoning", "assistant.message",
  "tool.execution_start", "tool.execution_complete", "session.result", "assistant.message",
]);
assert.equal(live[1].data.content, liveText);
assert.equal(live[1].data.delta, true);
assert.equal(live[2].data.delta, false);
assert.equal(Object.hasOwn(live[2].data, "content"), false);
assert.equal(live[4].data.messageId, "model-call-1");
assert.equal(live[5].data.messageId, "model-call-1");
assert.equal(live[6].data.status, "success");
const {normalizeUnifiedSessionEvent} = require("./unified_session_payload.cjs");
const unifiedLive = live.map(event => normalizeUnifiedSessionEvent(event, "agent"));
assert.equal(unifiedLive[1].data.content, liveText);
assert.equal(unifiedLive[1].timestamp, liveTimestamp - 2);
assert.equal(unifiedLive[3].timestamp, liveTimestamp);
assert.equal(unifiedLive[3].data.apiCallId, "model-call-1");
assert.equal(unifiedLive[4].timestamp, liveTimestamp);
assert.equal(unifiedLive[5].timestamp, liveTimestamp + 1);
assert.deepEqual(unifiedLive[4].data.input, liveStart.tool_call.readToolCall.args);
assert.deepEqual(unifiedLive[5].data.output, liveComplete.tool_call.readToolCall.result);
assert.equal(unifiedLive[6].data.usage.cacheReadInputTokens, 95360);
assert.equal(unifiedLive[6].data.usage.cacheCreationInputTokens, 0);
assert.equal(unifiedLive[7].data.content, liveText);
assert.equal(unifiedLive[7].data.apiCallId, "request-1");
assert.deepEqual(JSON.parse(JSON.stringify(unifiedLive)), unifiedLive);
const duplicate = parseLog([init, {...assistant, message: {content: [{type: "text", text: liveText}]}}, liveResult].map(JSON.stringify).join("\n")).logEntries;
assert.equal(duplicate.filter(event => event.type === "assistant.message").length, 1);
for (const timestamp_ms of [-1, "1793526675866", Number.MAX_SAFE_INTEGER + 1]) {
  const invalidTimestamp = parseLog(JSON.stringify({...assistant, timestamp_ms})).logEntries[0];
  assert.equal(Object.hasOwn(invalidTimestamp, "timestamp"), false);
}
const metadataOnly = parseLog(JSON.stringify({type: "tool_call", subtype: "started", tool_call: {
  hookAdditionalContexts: [], metadata: {context: "not a call"},
}})).logEntries;
assert.equal(metadataOnly.some(event => event.type.startsWith("tool.execution")), false);
assert.equal(metadataOnly[0].type, "cursor.tool_call");
assert.equal(parseLog(JSON.stringify({...result, subtype: "error", is_error: true})).logEntries[0].data.status, "error");
process.stdout.write(JSON.stringify(parserEvents));
`

func TestCursorSmokeExercisesNativeMCPAndGoBuild(t *testing.T) {
	source, err := os.ReadFile("../../.github/workflows/smoke-cursor.md")
	require.NoError(t, err)
	assert.NotContains(t, string(source), "shared/gh.md")
	assert.Contains(t, string(source), "Do not substitute `gh`, curl, or another CLI")
	assert.Contains(t, string(source), "    - go\n")
	assert.Contains(t, string(source), "runtimes:\n  go:\n")

	for _, workflow := range []string{"smoke-cursor", "engine-conformance-cursor"} {
		lock, err := os.ReadFile("../../.github/workflows/" + workflow + ".lock.yml")
		require.NoError(t, err)
		assert.Contains(t, string(lock), `const version = "2026.10.14-941492e"`)
		assert.Contains(t, string(lock), `...modelArgs, "--", prompt`)
		if workflow == "smoke-cursor" {
			assert.NotContains(t, string(lock), "--enable-gh-proxy")
			assert.Contains(t, string(lock), `"container": "ghcr.io/github/github-mcp-server:`)
			assert.Contains(t, string(lock), "proxy.golang.org")
			assert.Contains(t, string(lock), "sum.golang.org")
			assert.Contains(t, string(lock), "storage.googleapis.com")
		}
	}
}
