"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { spawnSync } = require("node:child_process");
const { test } = require("node:test");

const source = fs.readFileSync(path.join(__dirname, "../.github/workflows/shared/copilot-session-data-fetch.md"), "utf8");
const fetchStep = source
  .split("    run: |\n")[2]
  .split("\n---")[0]
  .replace(/^      /gm, "");
const summaryCode = fetchStep.match(/python3 - <<'PY'\n([\s\S]*?)\nPY/)[1];

function execute(command, args, options = {}) {
  const result = spawnSync(command, args, { encoding: "utf8", timeout: 20000, ...options });
  assert.equal(result.status, 0, result.stderr || result.stdout);
  return result.stdout;
}

function fixture(t) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "copilot-session-fetch-"));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  fs.mkdirSync(path.join(root, "logs"));
  return root;
}

function event(type, id, timestamp, data = {}, metadata = {}) {
  return { type, timestamp, data: { toolCallId: id, ...data }, ...metadata };
}

test("summaries pair interleaved calls by ID and agent, measure UTF-8 bytes, and retain unavailable measurements", t => {
  const root = fixture(t);
  fs.writeFileSync(path.join(root, "sessions-list.json"), JSON.stringify([{ id: 1, status: "completed", conclusion: "success" }]));
  const events = [
    event("tool.execution_start", "same", "2026-10-26T00:00:00.100Z", { toolName: "bash", arguments: { command: "é" } }),
    event("tool.execution_start", "same", "2026-10-26T00:00:00.200Z", { toolName: "edit", input: "" }, { agentId: "child" }),
    event("tool.execution_complete", "same", "2026-10-26T00:00:00.250Z", { result: "", success: false }, { agentId: "child" }),
    event("tool.execution_complete", "same", "2026-10-26T00:00:00.425Z", { result: "😀", success: true }),
    event("tool.execution_start", "pending", "invalid", { toolName: "validate" }),
    event("tool.execution_start", "bad-time", "invalid", { toolName: "test" }),
    event("tool.execution_complete", "bad-time", "2026-10-26T00:00:00Z", { success: true }),
    event("tool.execution_complete", "unknown", "2026-10-26T00:00:00Z"),
  ];
  const original = events.map(event => JSON.stringify(event)).join("\n") + '\nnot-json\n{"type":"tool.execution_start","data":null}\n';
  fs.writeFileSync(path.join(root, "logs/1-events.jsonl"), original);
  execute("python3", ["-c", summaryCode.replace("/tmp/gh-aw/agent/session-data", root)]);
  const [summary] = JSON.parse(fs.readFileSync(path.join(root, "tool-summaries.json")));
  assert.equal(summary.event_count, 8);
  assert.equal(summary.invalid_event_count, 2);
  assert.equal(summary.unmatched_completions, 1);
  assert.equal(summary.incomplete_tool_calls, 1);
  assert.equal(summary.tool_calls[0].duration_ms, 325);
  assert.equal(summary.tool_calls[0].input_bytes, Buffer.byteLength(JSON.stringify({ command: "é" })));
  assert.equal(summary.tool_calls[0].output_bytes, 4);
  assert.equal(summary.tool_calls[1].duration_ms, 50);
  assert.equal(summary.tool_calls[1].input_bytes, 0);
  assert.equal(summary.tool_calls[1].output_bytes, 0);
  assert.equal(summary.tool_calls[1].success, false);
  assert.equal(summary.tool_calls[2].input_bytes, null);
  assert.equal(summary.tool_calls[3].duration_ms, null);
  assert.equal(summary.tool_calls[3].output_bytes, null);
  assert.equal(fs.readFileSync(path.join(root, "logs/1-events.jsonl"), "utf8"), original);
  assert.ok(!JSON.stringify(summary).includes("😀"));
});

test("transcript-only and missing sessions explicitly lack tool measurements", t => {
  const root = fixture(t);
  fs.writeFileSync(path.join(root, "sessions-list.json"), JSON.stringify([1, 2].map(id => ({ id, status: "in_progress", conclusion: "in_progress" }))));
  fs.writeFileSync(path.join(root, "logs/1-conversation.txt"), "conversation");
  execute("python3", ["-c", summaryCode.replace("/tmp/gh-aw/agent/session-data", root)]);
  const summaries = JSON.parse(fs.readFileSync(path.join(root, "tool-summaries.json")));
  assert.deepEqual(
    summaries.map(summary => summary.source),
    ["conversation", "missing"]
  );
  assert.ok(summaries.every(summary => summary.measurements_available === false && summary.tool_calls.length === 0));
});

test("fetch selects coding-agent runs, persists artifacts and fallbacks, normalizes nulls, and restores complete caches", t => {
  const root = fixture(t);
  const bin = path.join(root, "bin");
  fs.mkdirSync(bin);
  const runs = [
    { id: 10, path: ".github/workflows/ci.yml", head_branch: "copilot/fix", status: "completed", conclusion: "success" },
    { id: 11, path: "dynamic/copilot-swe-agent/copilot", head_branch: "copilot/descriptive-slug", status: "completed", conclusion: null },
    { id: 12, path: "dynamic/copilot-swe-agent/copilot", head_branch: "main", status: "completed", conclusion: "success" },
    { id: 13, path: "dynamic/copilot-swe-agent/copilot", head_branch: "copilot/transcript", status: "completed", conclusion: "failure" },
  ];
  fs.writeFileSync(path.join(root, "runs.json"), JSON.stringify({ workflow_runs: runs }));
  execute("python3", [
    "-c",
    `
import zipfile
with zipfile.ZipFile(${JSON.stringify(path.join(root, "events.zip"))}, "w") as archive:
    archive.writestr("session with spaces/events.jsonl", '{"type":"assistant.message","data":{"content":"hello"}}')
with zipfile.ZipFile(${JSON.stringify(path.join(root, "conversation.zip"))}, "w") as archive:
    archive.writestr("session/conversation.md", "Full conversation artifact")
`,
  ]);
  const gh = `#!/bin/bash
set -euo pipefail
echo "$*" >> "$MOCK_ROOT/api-calls"
endpoint="$2"
case "$endpoint" in
  *actions/runs\\?*) payload="$(cat "$MOCK_ROOT/runs.json")" ;;
  */runs/11/artifacts) payload='{"artifacts":[{"id":101,"expired":false}]}' ;;
  */runs/12/artifacts) payload='{"artifacts":[{"id":102,"expired":false}]}' ;;
  */artifacts/101/zip) cat "$MOCK_ROOT/events.zip"; exit ;;
  */artifacts/102/zip) cat "$MOCK_ROOT/conversation.zip"; exit ;;
  */runs/13/artifacts) payload='{"artifacts":[]}' ;;
  */runs/13/jobs) payload='{"jobs":[{"id":103}]}' ;;
  */jobs/103/logs) echo '[cca-engine] turn=1 assistant.message: final answer'; exit ;;
  *) echo "Unexpected endpoint: $endpoint" >&2; exit 1 ;;
esac
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--jq" ]; then printf '%s' "$payload" | jq "$2"; exit; fi
  shift
done
exit 1
`;
  fs.writeFileSync(path.join(bin, "gh"), gh, { mode: 0o755 });
  const script = fetchStep.replaceAll("/tmp/gh-aw/agent/session-data", path.join(root, "data")).replaceAll("/tmp/gh-aw/cache-memory", path.join(root, "cache")).replaceAll("./.github/skills/jqschema/jqschema.sh", "jq -n '{}' ");
  const env = { ...process.env, GH_TOKEN: "", GITHUB_TOKEN: "", MOCK_ROOT: root, GITHUB_REPOSITORY: "owner/repo", GITHUB_OUTPUT: path.join(root, "output"), RUNNER_TEMP: root, PATH: `${bin}:${process.env.PATH}` };
  execute("bash", ["-e", "-o", "pipefail", "-c", script], { env });
  const sessions = JSON.parse(fs.readFileSync(path.join(root, "data/sessions-list.json")));
  assert.deepEqual(
    sessions.map(session => session.id),
    [11, 12, 13]
  );
  assert.equal(sessions[0].conclusion, "unknown");
  assert.equal(fs.readFileSync(path.join(root, "data/logs/12-conversation.txt"), "utf8"), "Full conversation artifact\n");
  assert.match(fs.readFileSync(path.join(root, "data/logs/13-conversation.txt"), "utf8"), /final answer/);
  assert.equal(JSON.parse(fs.readFileSync(path.join(root, "data/logs/11-events.jsonl"))).type, "assistant.message");
  const calls = fs.readFileSync(path.join(root, "api-calls"), "utf8");
  assert.match(calls, /artifacts --paginate/);
  assert.match(calls, /jobs --paginate/);
  fs.rmSync(path.join(root, "data"), { recursive: true });
  execute("bash", ["-e", "-o", "pipefail", "-c", script], { env });
  assert.equal(fs.readFileSync(path.join(root, "api-calls"), "utf8"), calls);
  assert.equal(JSON.parse(fs.readFileSync(path.join(root, "data/tool-summaries.json")))[0].source, "events");
});

test("null conclusions normalize unfinished and unknown sessions without inventing success", () => {
  const filter = fetchStep.match(/jq '(map\(\.conclusion[\s\S]*?)' \\/)[1];
  const normalized = JSON.parse(
    execute("jq", [filter], { input: JSON.stringify([{ status: "in_progress", conclusion: null }, { status: "queued" }, { status: "completed", conclusion: null }, { status: "completed", conclusion: "failure" }, {}]) })
  );
  assert.deepEqual(
    normalized.map(session => session.conclusion),
    ["in_progress", "queued", "unknown", "failure", "unknown"]
  );
});

test("analyzer keeps large structured events intact while trimming text", t => {
  const root = fixture(t);
  const analyzer = fs.readFileSync(path.join(__dirname, "../.github/workflows/copilot-opt.md"), "utf8");
  const trim = analyzer
    .split("  - name: Trim large session logs\n    run: |\n")[1]
    .split("\n---")[0]
    .replace(/^      /gm, "")
    .replace("/tmp/gh-aw/agent/session-data/logs", path.join(root, "logs"));
  const raw = JSON.stringify({ type: "assistant.message", data: { content: "a".repeat(12000) } }) + "\n";
  fs.writeFileSync(path.join(root, "logs/1-events.jsonl"), raw);
  fs.writeFileSync(path.join(root, "logs/1-conversation.txt"), "a".repeat(12000));
  execute("bash", ["-e", "-c", trim]);
  assert.equal(fs.readFileSync(path.join(root, "logs/1-events.jsonl"), "utf8"), raw);
  assert.ok(fs.statSync(path.join(root, "logs/1-conversation.txt")).size < 10240);
});
