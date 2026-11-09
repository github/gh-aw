import fs from "node:fs";
import { createRequire } from "node:module";
import { describe, test, expect } from "vitest";
import { Readable } from "node:stream";
import { readLockFileHeaders } from "./work_queue_provisioning.cjs";

const require = createRequire(import.meta.url);
const runs = require("./fixtures/claude_codex_subagent_runs.cjs");
const workflow = fs.readFileSync(new URL("../../../.github/workflows/smoke-claude-sub-agents.md", import.meta.url), "utf8");
const script = workflow
  .split("      script: |\n")[1]
  .split("\n---")[0]
  .replace(/^        /gm, "");
const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
const MARKER = "GH-AW-CLAUDE-SUBAGENT-MARKER-7F3A";
const clone = value => JSON.parse(JSON.stringify(value));

/**
 * Passing evidence derived from the Typist run 39415087125 record shapes (fixture mutation:
 * the worker is registered, the first `Agent` call names it in the foreground, and the
 * started/configured/completed records belong to it on the resolved id). The worker's prompt
 * record follows smoke-claude-sub-agents run 39536972987 (Claude Code 2.1.296): the stream
 * carries the worker's `user` prompt with `parent_tool_use_id`, but none of its assistant messages.
 */
function passingEvidence() {
  const model = "claude-haiku-5-5";
  const toolUse = { ...clone(runs.typistAgentToolUse), input: { description: "Return marker", subagent_type: "marker-worker", prompt: "marker?" } };
  const started = clone(runs.typistFallbackSubagentStarted);
  Object.assign(started.data, { toolCallId: toolUse.id, agentName: "marker-worker", agentType: "marker-worker", executionMode: "foreground" });
  const configured = clone(runs.typistFallbackSubagentConfigured);
  configured.data.model = model;
  return {
    model,
    events: [
      { type: "model.resolution", data: { engine: "claude", role: "marker-worker", configured_model: "small", resolved_model: model, provider: "anthropic", status: "resolved", cause: null } },
      started,
      configured,
      { type: "subagent.completed", agentId: started.agentId, data: { toolCallId: toolUse.id, durationMs: 1200, totalTokens: 50, totalToolCalls: 0 } },
    ],
    stream: [
      { ...clone(runs.typistInit), agents: [...runs.typistInit.agents, "marker-worker"] },
      { type: "assistant", parent_tool_use_id: null, message: { id: "msg_parent", model: "claude-sonnet-5", content: [toolUse] } },
      { type: "user", parent_tool_use_id: toolUse.id, message: { role: "user", content: [{ type: "text", text: "marker?" }] } },
      { type: "user", message: { role: "user", content: [{ type: "tool_result", tool_use_id: toolUse.id, content: [{ type: "text", text: MARKER }] }] } },
    ],
    requests: [
      { provider: "anthropic", model: "claude-sonnet-5", path: "/v1/messages?beta=true", status: 200 },
      { provider: "anthropic", model, path: "/v1/messages?beta=true", status: 200 },
    ],
    upstreamErrors: null,
    definition: `---\nname: "marker-worker"\ndescription: Returns a fixed marker\nmodel: "${model}"\n---\nReply.\n`,
  };
}

async function assertEvidence({ events, stream, requests, upstreamErrors, definition }) {
  const files = {
    "aw_session.jsonl": events,
    "agent-stdio.log": stream,
    "token-usage.jsonl": requests,
    "upstream-errors.jsonl": upstreamErrors,
  };
  const mockRequire = name => {
    if (name.endsWith("unified_session.cjs")) return { writeUnifiedSession() {} };
    if (name === "node:fs") {
      return {
        existsSync: file => !file.endsWith("upstream-errors.jsonl") || files["upstream-errors.jsonl"] !== null,
        readFileSync: file => {
          if (file.endsWith("marker-worker.md")) return definition;
          const key = Object.keys(files).find(suffix => file.endsWith(suffix));
          return files[key].map(record => JSON.stringify(record)).join("\n");
        },
      };
    }
    return require(name);
  };
  await new AsyncFunction("require", "process", script)(mockRequire, {
    env: { RUNNER_TEMP: "/tmp", GITHUB_WORKSPACE: "/workspace", SMOKE_EXECUTION: "success" },
  });
}

describe("claude_small_subagent_smoke_runs_resolved_model_first (post-step checks)", () => {
  test("accepts the declared worker invoked first on its resolved model", async () => {
    await expect(assertEvidence(passingEvidence())).resolves.toBeUndefined();
  });

  test("rejects the recorded Typist registration failure and general-purpose fallback", async () => {
    // Recorded Typist records: init without the custom agent, a failed named delegation and
    // a built-in general-purpose sub-agent on the main model.
    const data = passingEvidence();
    data.stream[0] = clone(runs.typistInit);
    await expect(assertEvidence(data)).rejects.toThrow("native initialization did not register marker-worker");

    const fallback = passingEvidence();
    fallback.events.splice(1, 0, clone(runs.typistFallbackSubagentStarted));
    await expect(assertEvidence(fallback)).rejects.toThrow("fell back to general-purpose");

    const failed = passingEvidence();
    failed.stream[3].message.content[0] = { ...clone(runs.typistAgentToolResult), tool_use_id: failed.stream[1].message.content[0].id };
    await expect(assertEvidence(failed)).rejects.toThrow("marker-worker delegation failed");
  });

  test("rejects a resolution record without the worker being invoked first", async () => {
    const data = passingEvidence();
    data.stream.splice(1, 0, { type: "assistant", parent_tool_use_id: null, message: { id: "msg_read", content: [{ type: "tool_use", id: "toolu_read", name: "Read", input: { file_path: "README.md" } }] } });
    await expect(assertEvidence(data)).rejects.toThrow("first agent action must invoke marker-worker");

    const missing = passingEvidence();
    missing.events.shift();
    await expect(assertEvidence(missing)).rejects.toThrow("marker-worker has no model.resolution record");
  });

  test("rejects a worker configured, served or staged on another model", async () => {
    const configured = passingEvidence();
    configured.events[2].data.model = "claude-sonnet-5";
    await expect(assertEvidence(configured)).rejects.toThrow("was not configured on claude-haiku-5-5");

    // The main agent on the worker's model makes the proxy evidence ambiguous.
    const ambiguous = passingEvidence();
    ambiguous.stream[1].message.model = ambiguous.model;
    await expect(assertEvidence(ambiguous)).rejects.toThrow("main agent also used claude-haiku-5-5");

    const proxy = passingEvidence();
    proxy.requests = proxy.requests.filter(request => request.model !== proxy.model);
    await expect(assertEvidence(proxy)).rejects.toThrow("the proxy served no request on claude-haiku-5-5");

    // AWF logs failed requests to upstream-errors.jsonl, not token-usage.jsonl (record fields as
    // in gh-aw-firewall's upstream-log.test.js capture of run 38327292581). A failure on the
    // worker's model fails the check even when a retry succeeded.
    const rejected = passingEvidence();
    rejected.upstreamErrors = [{ request_id: "req-1", provider: "anthropic", model: rejected.model, status: 400, path: "/v1/messages?beta=true" }];
    await expect(assertEvidence(rejected)).rejects.toThrow("a request on claude-haiku-5-5 failed");

    // A failure on another model is not the worker's.
    const unrelated = passingEvidence();
    unrelated.upstreamErrors = [{ request_id: "req-2", provider: "anthropic", model: "claude-sonnet-5", status: 529, path: "/v1/messages?beta=true" }];
    await expect(assertEvidence(unrelated)).resolves.toBeUndefined();

    const staged = passingEvidence();
    staged.definition = "---\ndescription: Returns a fixed marker\nmodel: small\n---\nReply.\n";
    await expect(assertEvidence(staged)).rejects.toThrow("definition has no native name");
  });
});

describe("smoke-claude-sub-agents is read-only", () => {
  const lock = fs.readFileSync(new URL("../../../.github/workflows/smoke-claude-sub-agents.lock.yml", import.meta.url), "utf8");

  test("the compiled lock grants no issues: write and exposes no issue-creating safe output", () => {
    expect(lock).not.toMatch(/^\s*issues:\s*write\s*$/m);
    expect(lock).not.toMatch(/create_issue|create_report_incomplete_issue/);
    expect(lock).toContain('GH_AW_FAILURE_REPORT_AS_ISSUE: "false"');
  });

  test("the agent finishes through a non-publishing noop", async () => {
    const { manifest } = await readLockFileHeaders(Readable.from([lock]));
    expect(manifest.mcp_servers.find(server => server.name === "safeoutputs").tools).toContain("noop");
    expect(lock).toContain('GH_AW_NOOP_REPORT_AS_ISSUE: "false"');
    expect(workflow).toContain("Finish by calling the `noop` safe output once");
  });
});
