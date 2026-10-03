import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { collectUnifiedSession, mergeSessionSources, normalizeRuntimeEvent, parseEngineSession, sessionTimestamp, writeUnifiedSession } from "./unified_session.cjs";
import { serializeSessionArtifact, writeSessionArtifact } from "./session_artifact.cjs";
import { normalizeUnifiedSessionEvent } from "./unified_session_payload.cjs";

const req = require;
const { success: piSuccess } = req("./fixtures/pi_ci_stream.cjs");
const claudeFixtures = req("./fixtures/claude_ci_sessions.cjs");
const codexNoTools = fs.readFileSync(new URL("./test_data/codex_ci_no_tools.jsonl", import.meta.url), "utf8");

describe("Unified conclusion session", () => {
  let root;
  let originalEnv;
  let originalCore;
  beforeEach(() => {
    root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-unified-session-"));
    originalEnv = { ...process.env };
    originalCore = global.core;
    global.core = { info: vi.fn(), warning: vi.fn() };
  });
  afterEach(() => {
    process.env = originalEnv;
    global.core = originalCore;
    vi.restoreAllMocks();
    fs.rmSync(root, { recursive: true, force: true });
  });

  function write(file, entries) {
    const target = path.join(root, file);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.writeFileSync(target, typeof entries === "string" ? entries : file.endsWith(".json") ? JSON.stringify(entries) : entries.map(entry => JSON.stringify(entry)).join("\n") + "\n");
    return target;
  }

  it.each(["aw_info.json", "usage/aw_info.json"])("records sandbox runtime and complete session info from %s", metadataPath => {
    const createdAt = "2026-10-02T00:00:00Z";
    write(metadataPath, { engine_id: "copilot", agent_version: "1.0.90", version: "configured", cli_version: "0.90.0", agent_runtime: "cloud-hypervisor", created_at: createdAt });
    const events = writeUnifiedSession({ rootDir: root });
    const sandbox = events.filter(event => event.type === "session.sandbox");
    expect(sandbox).toEqual([
      {
        type: "session.sandbox",
        timestamp: createdAt,
        data: { runtime: "cloud-hypervisor" },
        provenance: { component: "workflow", phase: "activation", path: metadataPath, index: 1, timestampMs: Date.parse(createdAt) },
      },
    ]);
    const published = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8").trimEnd().split("\n").map(JSON.parse);
    expect(published.filter(event => event.type === "session.sandbox")).toEqual(sandbox);
    expect(events.some(event => event.type === "session.runtime")).toBe(false);
    expect(published[0].type).toBe("session.format");
    expect(events.some(event => event.type === "session.init")).toBe(false);
    expect(events.find(event => event.type === "session.info")).toMatchObject({
      data: JSON.parse(fs.readFileSync(path.join(root, metadataPath), "utf8")),
      provenance: { component: "workflow", phase: "activation", path: metadataPath, index: 2, timestampMs: Date.parse(createdAt) },
    });
    writeUnifiedSession({ rootDir: root });
    expect(fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8").trimEnd().split("\n").map(JSON.parse)).toEqual(published);
  });

  it("retains the complete aw-info JSON payload without compaction or source mutation", () => {
    const metadata = {
      engine_id: "copilot",
      agent_version: "1.0.90",
      agent_runtime: "docker",
      engine_name: "GitHub Copilot CLI",
      created_at: "2026-10-02T00:00:00Z",
      allowed_domains: ["example.com"],
      supports_tools_allowlist: false,
      run_number: 0,
      target_repo: "",
      features: { custom: false },
      context: { run_id: 1, repo: "example/repo", workflow_id: "caller" },
      future_metadata: { value: null, nested: [0, false, "", { text: "  first\nsecond\t\n" }] },
    };
    const metadataPath = write("aw_info.json", metadata);
    const original = fs.readFileSync(metadataPath, "utf8");
    writeUnifiedSession({ rootDir: root });
    const published = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8").trimEnd().split("\n").map(JSON.parse);
    const info = published.filter(event => event.type === "session.info");
    expect(info).toHaveLength(1);
    expect(info[0].data).toEqual(metadata);
    expect(info[0].timestamp).toBe(metadata.created_at);
    expect(fs.readFileSync(metadataPath, "utf8")).toBe(original);
  });

  it("redacts registered values from nested aw-info property names", () => {
    write("aw_info.json", { future_metadata: { "opaque-mask": true, nested: { "key-opaque-mask-suffix": "visible" } } });
    write("agent-stdio.log", "::add-mask::opaque-mask\n");

    writeUnifiedSession({ rootDir: root });

    const content = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
    expect(content).not.toContain("opaque-mask");
    expect(
      content
        .trimEnd()
        .split("\n")
        .map(JSON.parse)
        .find(event => event.type === "session.info").data
    ).toEqual({ future_metadata: { "***": true, nested: { "key-***-suffix": "visible" } } });
  });

  it.each(["aw_info.json", "usage/aw_info.json"])("records general sandbox configuration from %s with source provenance", metadataPath => {
    const createdAt = "2026-10-02T00:00:00Z";
    write(metadataPath, {
      engine_id: "copilot",
      agent_runtime: "cloud-hypervisor",
      firewall_enabled: true,
      awf_version: "v0.30.1",
      awmg_version: "v1.0.0",
      allowed_domains: ["example.com", "api.github.com"],
      steps: { firewall: "squid", unrelated: "omit" },
      created_at: createdAt,
    });
    const events = writeUnifiedSession({ rootDir: root });
    const published = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8").trimEnd().split("\n").map(JSON.parse);
    expect(published.filter(event => event.type === "session.sandbox")).toEqual([
      {
        type: "session.sandbox",
        timestamp: createdAt,
        data: { runtime: "cloud-hypervisor", firewallEnabled: true, firewallType: "squid", firewallVersion: "v0.30.1", mcpGatewayVersion: "v1.0.0", allowedDomains: ["example.com", "api.github.com"] },
        provenance: { component: "workflow", phase: "activation", path: metadataPath, index: 1, timestampMs: Date.parse(createdAt) },
      },
    ]);
    expect(events.filter(event => event.type === "session.sandbox")).toHaveLength(1);
    expect(events.some(event => event.type === "session.runtime")).toBe(false);
    writeUnifiedSession({ rootDir: root });
    expect(fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8").trimEnd().split("\n").map(JSON.parse)).toEqual(published);
  });

  it.each([
    [
      { sandbox_configuration_observed: true, firewall_enabled: true, agent_runtime: "", allowed_domains: [] },
      { runtime: "docker", firewallEnabled: true, allowedDomains: [] },
    ],
    [
      { sandbox_configuration_observed: true, firewall_enabled: false, agent_runtime: "", awf_version: "", awmg_version: "", steps: { firewall: "" }, allowed_domains: [] },
      { runtime: "none", firewallEnabled: false, allowedDomains: [] },
    ],
    [{ agent_runtime: "docker-sudo-iptables" }, { runtime: "docker-sudo-iptables" }],
    [{ firewall_version: "v0.29.0" }, { firewallVersion: "v0.29.0" }],
    [{ awmg_version: "v1.0.0" }, { mcpGatewayVersion: "v1.0.0" }],
    [{ allowed_domains: ["example.com"] }, { allowedDomains: ["example.com"] }],
    [{ agent_runtime: "docker", firewall_enabled: "false", awf_version: 1, awmg_version: null, steps: false, allowed_domains: [1] }, { runtime: "docker" }],
  ])("retains partial sandbox observations and explicit false/empty values (%j)", (metadata, expected) => {
    write("aw_info.json", metadata);
    const sandbox = collectUnifiedSession({ rootDir: root }).events.filter(event => event.type === "session.sandbox");
    expect(sandbox).toHaveLength(1);
    expect(sandbox[0].data).toEqual(expected);
    expect(sandbox[0]).not.toHaveProperty("timestamp");
    expect(sandbox[0].provenance).not.toHaveProperty("timestampMs");
  });

  it.each([{}, { firewall_enabled: false, allowed_domains: [] }, { agent_runtime: "", firewall_enabled: null, awf_version: "", awmg_version: "", steps: [], allowed_domains: null }])(
    "does not fabricate a sandbox event without sandbox evidence (%j)",
    metadata => {
      write("aw_info.json", metadata);
      expect(collectUnifiedSession({ rootDir: root }).events.some(event => event.type === "session.sandbox")).toBe(false);
    }
  );

  it("selects original sandbox metadata and redacts sandbox fields before publication", () => {
    write("aw_info.json", { firewall_enabled: true, awf_version: "opaque-mask", allowed_domains: ["opaque-mask"] });
    write("usage/aw_info.json", { firewall_enabled: false, awf_version: "duplicate" });
    write("agent-stdio.log", "::add-mask::opaque-mask\n");
    write("agent-session.jsonl", [{ type: "assistant.message", data: { content: "done" } }]);
    writeUnifiedSession({ rootDir: root });
    const content = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
    expect(content).not.toContain("opaque-mask");
    expect(content).not.toContain("duplicate");
    expect(
      content
        .trimEnd()
        .split("\n")
        .map(JSON.parse)
        .filter(event => event.type === "session.sandbox")
    ).toEqual([expect.objectContaining({ data: { runtime: "docker", firewallEnabled: true, firewallVersion: "***", allowedDomains: ["***"] }, provenance: expect.objectContaining({ path: "aw_info.json" }) })]);
  });

  it.each([
    [
      { engine_id: "claude", agent_version: "", version: "2.1.160", cli_version: "0.90.0", agent_runtime: "", firewall_enabled: true },
      { runtime: "docker", firewallEnabled: true },
    ],
    [
      { engine_id: "codex", agent_version: "0.118.0", agent_runtime: "docker-sudo-iptables", firewall_enabled: true },
      { runtime: "docker-sudo-iptables", firewallEnabled: true },
    ],
    [
      { engine_id: "custom", agent_runtime: "", firewall_enabled: false, sandbox_configuration_observed: true },
      { runtime: "none", firewallEnabled: false },
    ],
    [{ agent_runtime: "cloud-hypervisor" }, { runtime: "cloud-hypervisor" }],
  ])("records partial sandbox metadata without inventing unavailable values (%j)", (metadata, expected) => {
    write("aw_info.json", metadata);
    const sandbox = collectUnifiedSession({ rootDir: root }).events.filter(event => event.type === "session.sandbox");
    expect(sandbox).toHaveLength(1);
    expect(sandbox[0].data).toEqual(expected);
    expect(sandbox[0]).not.toHaveProperty("timestamp");
    expect(sandbox[0].provenance).not.toHaveProperty("timestampMs");
  });

  it.each([{}, { agent_runtime: "", firewall_enabled: null, awf_version: "" }])("does not fabricate sandbox events from unavailable metadata (%j)", metadata => {
    write("aw_info.json", metadata);
    expect(collectUnifiedSession({ rootDir: root }).events.some(event => event.type === "session.sandbox")).toBe(false);
  });

  it("prefers original run metadata and redacts session info and sandbox fields", () => {
    write("aw_info.json", { engine_id: "custom", agent_version: "opaque-mask", agent_runtime: "docker" });
    write("usage/aw_info.json", { engine_id: "copilot", agent_version: "duplicate", agent_runtime: "cloud-hypervisor" });
    write("agent-stdio.log", "::add-mask::opaque-mask\n");
    write("agent-session.jsonl", [{ type: "assistant.message", data: { content: "done" } }]);
    writeUnifiedSession({ rootDir: root });
    const content = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
    expect(content).not.toContain("opaque-mask");
    expect(content).not.toContain("duplicate");
    expect(
      content
        .trimEnd()
        .split("\n")
        .map(JSON.parse)
        .find(event => event.type === "session.info")
    ).toMatchObject({
      data: { engine_id: "custom", agent_version: "***", agent_runtime: "docker" },
      provenance: { path: "aw_info.json" },
    });
    expect(
      content
        .trimEnd()
        .split("\n")
        .map(JSON.parse)
        .filter(event => event.type === "session.sandbox")
    ).toEqual([expect.objectContaining({ data: { runtime: "docker" }, provenance: expect.objectContaining({ path: "aw_info.json" }) })]);
    expect(content).not.toContain('"type":"session.runtime"');
  });

  it("merges every required component with essential payloads and timestamp ordering", () => {
    const message = { type: "assistant.message", id: "same-id", timestamp: "2026-10-02T00:00:03Z", data: { content: "Done.\n", extra: [0, false, null] }, nativeField: "preserved" };
    const rpc = { timestamp: "2026-10-02T00:00:02+00:00", event: "rpc_response", payload: { jsonrpc: "2.0", id: 0, result: { content: [{ text: "response" }] } }, server_id: "github", direction: "IN" };
    const audit = { ts: 1790899201.25, host: "example.com", decision: "TCP_DENIED", status: 403, extra: false };
    write("agent-session.jsonl", [message]);
    write("mcp-logs/rpc-messages.jsonl", [rpc]);
    write("sandbox/firewall/audit/audit.jsonl", [audit]);
    write("sandbox/firewall/logs/api-proxy-logs/events.jsonl", [{ timestamp: "2026-10-02T00:00:03.5Z", eventName: "token_steering", message: "budget warning" }]);
    write("sandbox/firewall/logs/api-proxy-logs/token-tracker-audit.jsonl", [{ ts: 1790899203600, event: "TRACK_END", rid: "request" }]);
    write("threat-detection/sandbox/firewall/logs/audit.jsonl", [{ timestamp: "2026-10-02T00:00:04Z", event: "http_access", status: 200 }]);
    write("safeoutputs.jsonl", [{ type: "create_issue", title: "Requested" }]);
    write("safe-output-items.jsonl", [{ type: "create_issue", timestamp: "2026-10-02T00:00:05Z", url: "https://github.com/example/repo/issues/1" }]);
    write("safe-output-errors.json", { errors: [{ type: "add_comment", error: "rejected" }] });
    write("experiments/assignments.json", { experiment: "variant-b" });
    write("experiments/state.jsonl", [{ run_id: "1", timestamp: "2026-10-02T00:00:00Z", assignments: { experiment: "variant-b" } }]);
    write("agent/graders/grader_manifest.json", { graders: [{ id: "quality" }] });
    write("agent/graders/grader_results.json", { results: [{ id: "quality", score: 0, passed: false }] });
    write("evals/evals.jsonl", [{ id: "quality", timestamp: "2026-10-02T00:00:06Z", answer: "no" }]);
    write("usage/agent/execution.json", { outcome: "failure", duration_ms: 0 });
    write("usage/detection/detection_result.json", { conclusion: "failure", secret_leak: false });
    const events = writeUnifiedSession({ rootDir: root });
    const persisted = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8").trimEnd().split("\n").map(JSON.parse);
    expect(persisted).toEqual(events);
    expect(persisted[0]).toEqual({
      type: "session.format",
      data: { version: 1 },
      provenance: { component: "collector", phase: "conclusion", path: "usage/aw_session.jsonl", index: 0 },
    });
    expect(persisted.filter(event => event.type === "session.format")).toHaveLength(1);
    expect(new Set(events.map(event => event.provenance.component))).toEqual(new Set(["agent", "mcp", "firewall", "safe_output", "experiment", "grader", "eval", "execution", "detection", "collector"]));
    expect(events.find(event => event.type === "assistant.message")).toMatchObject(normalizeUnifiedSessionEvent(message));
    expect(events.find(event => event.type === "assistant.message")).not.toHaveProperty("nativeField");
    expect(events.find(event => event.type === "mcp.rpc.response").data).toEqual({ serverName: "github", direction: "IN", rpcId: 0 });
    expect(events.find(event => event.type === "firewall.http_access").data).toEqual({ host: "example.com", decision: "TCP_DENIED", status: 403 });
    expect(events.find(event => event.type === "firewall.http_access").provenance.timestampMs).toBe(1790899201250);
    expect(events.find(event => event.data.requestId === "request").provenance.timestampMs).toBe(1790899203600);
    expect(events.filter(event => event.provenance.phase === "detection")).toHaveLength(2);
    expect(events.find(event => event.type === "grader.result").data.results[0]).toEqual({ id: "quality", score: 0, passed: false });
    const times = events.slice(1).map(event => event.provenance.timestampMs ?? Infinity);
    expect(times).toEqual([...times].sort((a, b) => (a === b ? 0 : a < b ? -1 : 1)));
    expect(events.at(-1).type).toBe("session.collection");
    expect(events.at(-1).data).toMatchObject({ warnings: 0, absentComponents: [] });
    const original = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
    writeUnifiedSession({ rootDir: root });
    expect(fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8")).toBe(original);
  });

  it("retains native provenance, equal-time source order, and invalid/absent timestamps without mutation", () => {
    const sources = [
      {
        component: "agent",
        phase: "agent",
        path: "session",
        events: [
          { type: "vendor.notice", timestamp: "invalid", data: { flag: false }, provenance: { vendor: 1 } },
          { type: "assistant.message", timestamp: "2026-10-02T00:00:01Z", id: "duplicate", data: { content: "" } },
          { type: "assistant.message", timestamp: "2026-10-02T00:00:01Z", id: "duplicate", data: { content: "second" } },
        ],
      },
      { component: "mcp", phase: "agent", path: "rpc", events: [{ type: "mcp.event", timestamp: "2026-10-02T00:00:01Z", data: {} }] },
    ];
    const original = structuredClone(sources);
    const events = mergeSessionSources(sources);
    expect(events.map(event => event.data.content)).toEqual(["", "second", undefined, undefined]);
    expect(events.at(-1).provenance).toMatchObject({ native: { vendor: 1 }, index: 0 });
    expect(events.at(-1).timestamp).toBe("invalid");
    expect(events.at(-1).provenance).not.toHaveProperty("timestampMs");
    expect(sources).toEqual(original);
    expect(mergeSessionSources(sources)).toEqual(events);
  });

  it("normalizes pretty-printed source JSON to compact JSONL without losing payload whitespace", () => {
    const native = [
      { type: "assistant.message", data: { content: "  first line\nsecond line\t\n", structured: { empty: "", zero: 0, flag: false } } },
      { type: "vendor.extension", data: { nested: [{ key: null }] } },
    ];
    const prettyPrinted = JSON.stringify(native, null, 2);
    write("aw_info.json", { engine_id: "custom" });
    write("agent-stdio.log", prettyPrinted);
    writeUnifiedSession({ rootDir: root });
    const content = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
    expect(content.endsWith("\n")).toBe(true);
    expect(content).not.toContain("\n\n");
    const lines = content.trimEnd().split("\n");
    for (const line of lines) expect(line).toBe(JSON.stringify(JSON.parse(line)));
    const agent = lines.map(JSON.parse).filter(event => event.provenance.component === "agent");
    expect(agent.map(({ provenance, ...event }) => event)).toEqual(native.map(normalizeUnifiedSessionEvent));
    expect(lines.some(line => line.includes("\\nsecond line\\t\\n"))).toBe(true);
  });

  it("uses source-schema timestamp units and nested Pi message times without fabricating time", () => {
    expect(sessionTimestamp({ ts: 1.25 }, "seconds")).toBe(1250);
    expect(sessionTimestamp({ ts: 1250 })).toBe(1250);
    expect(sessionTimestamp({ timestamp: 0 })).toBe(0);
    expect(sessionTimestamp({ message: { timestamp: 1250 } })).toBe(1250);
    for (const value of ["", "123", "2026-10-02", NaN, Infinity, 8640000000000001]) expect(sessionTimestamp({ timestamp: value })).toBeUndefined();
  });

  it("prefers persisted canonical events over raw logs and avoids replicated firewall accounting", () => {
    write("agent-session.jsonl", [{ type: "vendor.extension", data: { preserved: true } }]);
    write("sandbox/agent/logs/copilot-session-state/uuid/events.jsonl", [{ type: "user.message", data: { content: "duplicate" } }]);
    write("agent-stdio.log", "not a session");
    write("sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl", "");
    write("sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl", [{ timestamp: "2026-10-02T00:00:01Z", event: "token_usage", input_tokens: 999 }]);
    const { events } = collectUnifiedSession({ rootDir: root });
    expect(events.filter(event => event.provenance.component === "agent")).toHaveLength(1);
    expect(events.some(event => event.data.input_tokens === 999)).toBe(false);
    expect(events.at(-1).data.sources).toContainEqual(expect.objectContaining({ path: "sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl", events: 0 }));
  });

  it.each([false, true])("retains current Codex accounting through conclusion collection (canonical present: %j)", canonicalPresent => {
    write("agent-stdio.log", codexNoTools);
    if (canonicalPresent) write("agent-session.jsonl", parseEngineSession(codexNoTools, "codex"));
    writeUnifiedSession({ rootDir: root, engine: "codex" });
    const published = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8").trimEnd().split("\n").map(JSON.parse);
    const agent = published.filter(event => event.provenance.component === "agent");
    expect(agent.map(event => event.type)).toEqual(["session.init", "session.result", "turn.started", "assistant.message", "assistant.message", "session.result"]);
    expect(agent[1].data).toEqual({ errors: ["Model metadata unavailable; using fallback metadata."] });
    expect(agent.at(-1).data).toEqual({
      status: "completed",
      sourceType: "turn.completed",
      numTurns: 1,
      usage: { input_tokens: 16847, output_tokens: 167, cache_read_input_tokens: 8576, cache_creation_input_tokens: 0, reasoning_output_tokens: 0 },
    });
    expect(agent.at(-1)).not.toHaveProperty("usage");
    expect(agent.every(event => event.provenance.path === (canonicalPresent ? "agent-session.jsonl" : "agent-stdio.log"))).toBe(true);
    expect(published.some(event => event.type === "session.collection_warning")).toBe(false);
  });

  it.each(["", "{bad\n", '{"type":"result","usage":{}}\n'])("falls back from an unusable canonical session (%j) to native events", content => {
    const nativePath = "sandbox/agent/logs/copilot-session-state/uuid/events.jsonl";
    write("agent-session.jsonl", content);
    write(nativePath, [{ type: "assistant.message", data: { content: "recovered" } }]);
    write("agent-stdio.log", "not a session");
    const { events } = collectUnifiedSession({ rootDir: root, warn: vi.fn() });
    expect(events.filter(event => event.provenance.component === "agent")).toEqual([expect.objectContaining({ type: "assistant.message", data: { content: "recovered" }, provenance: expect.objectContaining({ path: nativePath }) })]);
    expect(events.at(-1).data.sources).toContainEqual(expect.objectContaining({ path: "agent-session.jsonl", events: 0 }));
  });

  it.each([false, true])("falls back to raw logs when canonical and native events are unusable (native present: %j)", nativePresent => {
    write("agent-session.jsonl", "");
    if (nativePresent) write("sandbox/agent/logs/copilot-session-state/uuid/events.jsonl", "{bad\n");
    write("agent-stdio.log", JSON.stringify([{ type: "assistant.message", data: { content: "raw fallback" } }]));
    const { events } = collectUnifiedSession({ rootDir: root, engine: "custom", warn: vi.fn() });
    expect(events.filter(event => event.provenance.component === "agent")).toEqual([expect.objectContaining({ data: { content: "raw fallback" }, provenance: expect.objectContaining({ path: "agent-stdio.log" }) })]);
  });

  it.each([
    ["experiments/state.jsonl", "usage/experiment/state.jsonl"],
    ["experiments/state.json", "usage/experiment/state.json"],
    ["experiments/assignments.json", "usage/experiment/assignments.json"],
    ["agent/graders/grader_manifest.json", "usage/graders/grader_manifest.json"],
    ["agent/graders/grader_results.json", "usage/graders/grader_results.json"],
    ["evals/evals.jsonl", "usage/evals.jsonl"],
    ["agent_usage.jsonl", "usage/agent_usage.jsonl"],
    ["threat-detection/detection_usage.jsonl", "usage/detection_usage.jsonl"],
    ["detection_usage.jsonl", "usage/detection_usage.jsonl"],
    ["evals/evals_token_usage.jsonl", "usage/evals/token_usage.jsonl"],
    ["agent_execution.json", "usage/agent/execution.json"],
    ["threat-detection/execution.json", "usage/detection/execution.json"],
    ["evals/evals/execution.json", "usage/evals/execution.json"],
    ["evals/execution.json", "usage/evals/execution.json"],
    ["threat-detection/detection_result.json", "usage/detection/detection_result.json"],
  ])("prefers original provenance from %s and retains the %s mirror fallback", (original, mirror) => {
    const observation = { value: "source", id: "source", type: "source", status: "source", model: "source", assignments: { experiment: "source" }, outcome: "source", conclusion: "source", run_id: "source" };
    const record = file => (file.endsWith(".json") ? observation : [observation]);
    const source = write(original, record(original));
    write(mirror, record(mirror));
    const { events } = collectUnifiedSession({ rootDir: root });
    const observed = events.filter(event => event.provenance.path === original || event.provenance.path === mirror);
    expect(observed).toHaveLength(1);
    expect(observed[0].provenance.path).toBe(original);
    fs.unlinkSync(source);
    const fallback = collectUnifiedSession({ rootDir: root }).events.filter(event => event.provenance.path === original || event.provenance.path === mirror);
    expect(fallback).toHaveLength(1);
    expect(fallback[0].provenance.path).toBe(mirror);
  });

  it("recovers adjacent valid records, records malformed/partial coverage and skips symlinks", () => {
    const warn = vi.fn();
    write("agent-session.jsonl", '{"type":"assistant.message","data":{"content":"first"}}\n{bad\nnull\n{"type":"vendor.after","data":{}}\n');
    write("safe-output-errors.json", "{truncated");
    const secret = write("outside.jsonl", [{ type: "assistant.message", data: { content: "must not follow" } }]);
    fs.mkdirSync(path.join(root, "mcp-logs"));
    fs.symlinkSync(secret, path.join(root, "mcp-logs", "unsafe.jsonl"));
    fs.symlinkSync(path.join(root, "mcp-logs"), path.join(root, "experiments"));
    const { events } = collectUnifiedSession({ rootDir: root, warn });
    expect(events.filter(event => event.provenance.component === "agent")).toHaveLength(2);
    expect(events.filter(event => event.type === "session.collection_warning").map(event => event.data.code)).toEqual(["malformed_jsonl", "non_object_record", "symlink_not_read", "malformed_json"]);
    expect(warn).toHaveBeenCalled();
    expect(JSON.stringify(events)).not.toContain("must not follow");
  });

  it("reports absent components for an empty run without a fake agent result", () => {
    const { events } = collectUnifiedSession({ rootDir: root });
    expect(events).toHaveLength(2);
    expect(events[0]).toMatchObject({ type: "session.format", data: { version: 1 } });
    expect(events[0].provenance).not.toHaveProperty("timestampMs");
    expect(events.some(event => event.type === "session.sandbox")).toBe(false);
    expect(events[1]).toMatchObject({ type: "session.collection", data: { absentComponents: ["agent", "mcp", "firewall", "safe_output", "experiment", "grader", "eval"], warnings: 0, untimedEvents: 0 } });
  });

  it("pins only the collector file-format header, preserving a native session.format extension", () => {
    write("agent-session.jsonl", [
      { type: "session.format", timestamp: "2026-10-02T00:00:02Z", data: { version: "native-engine-format" } },
      { type: "assistant.message", timestamp: "2026-10-02T00:00:01Z", data: { content: "first timed event" } },
    ]);
    const events = writeUnifiedSession({ rootDir: root });
    expect(events.map(event => event.type)).toEqual(["session.format", "assistant.message", "session.format", "session.collection"]);
    expect(events[0].data.version).toBe(1);
    expect(events[2]).toMatchObject({ data: { version: "native-engine-format" }, provenance: { component: "agent", index: 0 } });
  });

  it("fails explicitly on read errors and removes a stale output rather than uploading it", () => {
    const source = write("agent-session.jsonl", [{ type: "assistant.message", data: { content: "current" } }]);
    const output = write("usage/aw_session.jsonl", "old session");
    const originalRead = fs.readFileSync;
    vi.spyOn(fs, "readFileSync").mockImplementation((file, ...args) => {
      if (file === source) throw new Error("fixture read denied");
      return originalRead(file, ...args);
    });
    expect(() => writeUnifiedSession({ rootDir: root })).toThrow("fixture read denied");
    expect(fs.existsSync(output)).toBe(false);
  });

  it("retains unfamiliar MCP/firewall messages and does not confuse prototypes with event kinds", () => {
    for (const component of ["mcp", "firewall"]) {
      const raw = { timestamp: "invalid", event: "__proto__", future: [false, null, 0] };
      const event = normalizeRuntimeEvent(component, raw);
      expect(event).toMatchObject({ type: `${component}.event`, data: raw, timestamp: "invalid" });
      expect(raw.future).toEqual([false, null, 0]);
    }
  });

  it("redacts decoded secrets and mask values in all payloads, including IDs, before persistence", () => {
    const secret = 'secret"with\\escaped\ncharacters';
    const token = "ghp_" + "a".repeat(36);
    process.env.GH_AW_SECRET_NAMES = "SESSION_SECRET";
    process.env.SECRET_SESSION_SECRET = secret;
    const events = [{ type: "tool.execution_complete", id: secret, data: { toolCallId: token, output: { nested: secret, mask: "opaque-mask" }, success: false } }];
    const original = structuredClone(events);
    const content = serializeSessionArtifact(events, ["opaque-mask"]);
    expect(content).not.toContain(token);
    const event = JSON.parse(content);
    expect(event.id).toBe("***REDACTED***");
    expect(event.data.toolCallId).toBe("***REDACTED***");
    expect(event.data.output).toEqual({ nested: "***REDACTED***", mask: "***" });
    expect(events).toEqual(original);
    write("agent-session.jsonl", [{ type: "assistant.message", data: { content: "opaque-mask" } }]);
    write("agent-stdio.log", "::add-mask::opaque-mask\n");
    writeUnifiedSession({ rootDir: root });
    expect(fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8")).not.toContain("opaque-mask");
  });

  it("uses a standardized error code for invalid session data", () => {
    expect(() => serializeSessionArtifact({})).toThrow("ERR_VALIDATION: Expected a session event array");
  });

  it("writes atomically and cleans up failed outputs without following a temporary symlink", () => {
    const output = write("usage/aw_session.jsonl", "old");
    const target = write("outside.jsonl", "unchanged");
    fs.symlinkSync(target, `${output}.tmp`);
    expect(() => writeSessionArtifact(output, [{ type: "vendor.event", data: {} }])).toThrow("ERR_SYSTEM: Failed to write session artifact");
    expect(fs.readFileSync(target, "utf8")).toBe("unchanged");
    expect(fs.existsSync(output)).toBe(false);
    expect(fs.existsSync(`${output}.tmp`)).toBe(false);
  });

  it.each([
    ["claude", () => JSON.stringify(claudeFixtures.success)],
    [
      "copilot",
      () =>
        JSON.stringify([
          { type: "session.start", timestamp: "2026-10-02T00:00:01Z", data: { sessionId: "copilot" } },
          { type: "assistant.message", data: { content: "done" } },
        ]),
    ],
    ["codex", () => fs.readFileSync(path.join(import.meta.dirname, "test_data/codex_ci_smoke.jsonl"), "utf8")],
    [
      "gemini",
      () =>
        JSON.stringify([
          { type: "init", model: "gemini-test" },
          { type: "assistant", message: "done" },
          { type: "result", stats: { input_tokens: 10, output_tokens: 2 } },
        ]),
    ],
    ["pi", () => piSuccess.map(entry => JSON.stringify(entry)).join("\n")],
    [
      "custom",
      () =>
        JSON.stringify([
          { type: "assistant.message", data: { content: "custom done" } },
          { type: "vendor.custom", data: { score: 0 } },
        ]),
    ],
  ])("normalizes and persists a %s workflow session using its existing engine adapter", (engine, fixture) => {
    const content = fixture();
    const expected = parseEngineSession(content, engine);
    expect(expected.length).toBeGreaterThan(0);
    write("aw_info.json", { engine_id: engine });
    write(engine === "pi" ? "pi-streaming.jsonl" : "agent-stdio.log", content);
    write("mcp-logs/gateway.jsonl", [{ timestamp: "2026-10-02T00:00:02Z", event: "tool_call", tool_name: "lookup" }]);
    const events = writeUnifiedSession({ rootDir: root });
    const actual = events
      .filter(event => event.provenance.component === "agent")
      .sort((a, b) => a.provenance.index - b.provenance.index)
      .map(({ provenance, ...event }) => event);
    expect(actual).toEqual(JSON.parse(JSON.stringify(expected.map(normalizeUnifiedSessionEvent))));
    expect(events.some(event => event.type === "mcp.tool_call")).toBe(true);
    expect(events.every(event => event.type.includes(".") && event.data && typeof event.data === "object")).toBe(true);
  });
});
