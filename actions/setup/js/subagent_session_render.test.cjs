import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { renderSubagentSummary } from "./subagent_session_render.cjs";
import { generatePlainTextSummary, generateCopilotCliStyleSummary, MAX_STEP_SUMMARY_SIZE } from "./log_parser_shared.cjs";
import { publishUnifiedSessionSummary } from "./unified_session_render.cjs";
import { writeUnifiedSession } from "./unified_session.cjs";

const modelMetrics = {
  opus: { requests: { count: 3 }, usage: { inputTokens: 100, outputTokens: 10, cacheReadTokens: 20, cacheWriteTokens: 0 } },
};
const metrics = {
  main: { totalNanoAiu: 1000000000, modelMetrics },
  child: { agentName: "research", agentDisplayName: "Research <routing>", totalNanoAiu: 3000000000, modelMetrics },
};
const lifecycle = [
  { type: "session.start", data: { sessionId: "root", selectedModel: "opus" } },
  { type: "subagent.started", agentId: "child", data: { agentName: "research", agentDisplayName: "Research <routing>", model: "opus", executionMode: "background", agentDescription: "PRIVATE_PROMPT" } },
  { type: "subagent.configured", agentId: "child", data: { model: "opus", reasoningEffort: "xhigh" } },
  { type: "subagent.started", agentId: "nested", parentId: "event-chain-parent", data: { agentName: "explore", parentId: "child", model: "haiku", executionMode: "sync" } },
];

describe("subagent session summaries", () => {
  afterEach(() => {
    vi.unstubAllEnvs();
    vi.unstubAllGlobals();
  });

  it("renders exact snapshots once with identity, hierarchy, tokens and credits in both views", () => {
    const events = [...lifecycle, { type: "session.shutdown", data: { agentMetrics: metrics } }, { type: "session.result", data: { agentMetrics: metrics } }];
    const original = structuredClone(events);
    for (const render of [generatePlainTextSummary, generateCopilotCliStyleSummary]) {
      const output = render(events);
      expect(output).toContain("Subagents (per-agent snapshots, not additional session usage)");
      expect(output).toContain("main credits: 1.000 (25.0% of agent credits)");
      expect(output).toContain("child credits: 3.000 (75.0% of agent credits)");
      expect(output).toContain("agentId=child");
      expect(output).toContain("agentName=research model=opus reasoningEffort=xhigh executionMode=background");
      expect(output).toContain("parentId=child model=haiku executionMode=sync");
      expect(output).toContain("requests=3 inputTokens=100 outputTokens=10 cacheReadTokens=20 cacheWriteTokens=0");
      expect(output.match(/child credits:/g)).toHaveLength(1);
      expect(output).toContain("Usage: unavailable");
      expect(output).not.toContain("PRIVATE_PROMPT");
    }
    expect(generateCopilotCliStyleSummary(events)).toContain("Research <routing>");
    expect(events).toEqual(original);
  });

  it("keeps repeated instances distinct, accepts zero usage and does not invent absent fields", () => {
    const events = [
      ...lifecycle,
      { type: "subagent.started", agentId: "second", data: { agentName: "research", agentDisplayName: "Research <routing>" } },
      { type: "session.result", data: { agentMetrics: { child: { totalNanoAiu: 0, modelMetrics: { opus: { requests: { count: 0 }, usage: { inputTokens: 0 } } } }, second: { modelMetrics: {} } } } },
    ];
    const output = renderSubagentSummary(events).join("\n");
    expect(output).toContain("child credits: 0.000");
    expect(output).toContain("requests=0 inputTokens=0");
    expect(output).toContain("agentId=second");
    expect(output).not.toContain("outputTokens=");
    expect(output).not.toContain("% of agent credits");
    expect(output).toContain("Usage: unavailable");
  });

  it("uses only the final attempt and final snapshot, even when a projection repeats initialization", () => {
    const events = [...lifecycle, { type: "session.result", data: { agentMetrics: metrics } }, { type: "session.start", data: { sessionId: "last" } }, { type: "session.init", data: { sessionId: "last", sourceEngine: "copilot" } }];
    expect(renderSubagentSummary(events)).toEqual([]);
    events.push({ type: "subagent.started", agentId: "final", data: { agentName: "explore" } });
    events.push({ type: "session.result", data: { agentMetrics: { final: { totalNanoAiu: 0 } } } });
    events.push({ type: "session.init", data: { sessionId: "last", sourceEngine: "copilot" } });
    const output = renderSubagentSummary(events).join("\n");
    expect(output).toContain("agentId=final");
    expect(output).not.toContain("agentId=child");
    expect(output).toContain("final credits: 0.000");
  });

  it("redacts secrets and strips control characters from identity fields", () => {
    vi.stubEnv("GH_AW_SECRET_NAMES", "TOKEN");
    vi.stubEnv("SECRET_TOKEN", "secret-subagent-token");
    vi.stubGlobal("core", { debug: vi.fn(), info: vi.fn(), warning: vi.fn() });
    const events = [{ type: "subagent.started", agentId: "child", data: { agentDisplayName: "secret-subagent-token\ninjected", model: "opus" } }];
    const output = renderSubagentSummary(events).join("\n");
    expect(output).not.toContain("secret-subagent-token");
    expect(output).not.toContain("\ninjected");
    expect(events[0].data.agentDisplayName).toContain("secret-subagent-token");
  });

  it("replaces earlier accounting rather than retaining missing agents in the latest snapshot", () => {
    const output = renderSubagentSummary([...lifecycle, { type: "session.shutdown", data: { agentMetrics: metrics } }, { type: "session.result", data: { agentMetrics: { main: metrics.main } } }]).join("\n");
    expect(output).toContain("agentId=child");
    expect(output).not.toContain("child credits:");
    expect(output).toContain("Usage: unavailable");
  });

  it("bounds published summary output including oversized subagent names", () => {
    const events = [
      { type: "session.format", data: { version: 1 }, provenance: { component: "collector", phase: "conclusion", path: "metadata", index: 0 } },
      { type: "subagent.started", agentId: "large", data: { agentDisplayName: "x".repeat(MAX_STEP_SUMMARY_SIZE * 2) }, provenance: { component: "agent", phase: "agent", path: "events.jsonl", index: 0 } },
    ];
    const output = generateCopilotCliStyleSummary(events, { maxBytes: 8192 });
    expect(Buffer.byteLength(output)).toBeLessThanOrEqual(8192);
    expect(output).toContain("Unified session summary omitted: remaining step-summary byte limit reached.");
    const bounded = generateCopilotCliStyleSummary(events, { maxBytes: 512 * 1024 });
    expect(Buffer.byteLength(bounded)).toBeLessThanOrEqual(512 * 1024);
    expect(bounded).toContain("Subagents");
    expect(bounded).toContain("truncated");
  });

  it("persists and publishes subagent accounting into the actual GitHub step summary", async () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-subagent-summary-"));
    try {
      const native = path.join(root, "sandbox/agent/logs/copilot-session-state/root/events.jsonl");
      fs.mkdirSync(path.dirname(native), { recursive: true });
      fs.writeFileSync(native, [...lifecycle, { type: "assistant.message", data: { content: "Done" } }, { type: "session.shutdown", data: { agentMetrics: metrics } }].map(JSON.stringify).join("\n") + "\n");
      writeUnifiedSession({ rootDir: root, engine: "copilot" });
      const summaryPath = path.join(root, "summary.md");
      fs.writeFileSync(summaryPath, "Existing summary\n");
      vi.stubEnv("GITHUB_STEP_SUMMARY", summaryPath);
      const core = { info: vi.fn(), warning: vi.fn() };
      vi.stubGlobal("core", core);
      await publishUnifiedSessionSummary(path.join(root, "usage/aw_session.jsonl"));
      const output = fs.readFileSync(summaryPath, "utf8");
      expect(output).toContain("Existing summary");
      expect(output).toContain("Research &lt;routing&gt;");
      expect(output).toContain("child credits: 3.000 (75.0% of agent credits)");
      expect(output).toContain("reasoningEffort=xhigh executionMode=background");
      expect(output).toContain("cacheWriteTokens=0");
      expect(output).not.toContain("PRIVATE_PROMPT");
      expect(Buffer.byteLength(output)).toBeLessThanOrEqual(MAX_STEP_SUMMARY_SIZE);
      expect(core.info.mock.calls.flat().join("\n")).toContain("child credits: 3.000");
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });
});
