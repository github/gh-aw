const fs = require("fs");
const cp = require("child_process");
const { performance } = require("perf_hooks");
const { computeGraders, createSessionLookups, runBuiltinGrader, withDefaultGraders, DEFAULT_GRADER_IDS, BUILTIN_GRADERS, BUILTIN_META, normalizeResult, evaluateThreshold } = require("./trace_graders.cjs");

function event(type, data, phase = "agent", source = "agent-session.jsonl") {
  return { type, data, provenance: { component: "agent", phase, path: source, index: 0 } };
}

function safeOutputResult(data, source = "safe-output-items.jsonl") {
  return { type: "safe_output.result", data, provenance: { component: "safe_output", phase: "safe_outputs", path: source, index: 0 } };
}

function fixture() {
  return [
    event("workflow.info", { runId: 123 }, "activation"),
    event("tool.execution_start", { toolCallId: "a", toolName: "view", input: { path: "a" } }),
    event("tool.execution_complete", { toolCallId: "a", success: true }),
    event("tool.execution_start", { toolCallId: "b", toolName: "view", input: { path: "a" } }),
    event("tool.execution_complete", { toolCallId: "b", success: false }),
    event("tool.execution_start", { toolCallId: "c", toolName: "edit" }),
    event("firewall.token_usage", { requestId: "r1", usage: { inputTokens: 10, outputTokens: 5 }, durationMs: 100 }),
    event("firewall.token_usage", { requestId: "r2", usage: { inputTokens: 30, outputTokens: 5 }, durationMs: 200 }),
    event("usage.report", { usage: { inputTokens: 999, outputTokens: 999 } }),
    event("firewall.token_usage", { usage: { inputTokens: 10000 }, durationMs: 9999 }, "detection"),
    event("mcp.event", { event: "retry" }),
    event("safe_output.result", { type: "create_issue", number: 1 }, "safe_outputs"),
    event("experiment.assignment", { assignments: { model: "fast" } }, "activation"),
    event("eval.result", { id: "goal", answer: "YES" }, "evals"),
  ];
}

afterEach(() => vi.restoreAllMocks());

describe("pure unified-session graders", () => {
  it("materializes statistics, lookups, and thresholds from canonical events", () => {
    const session = fixture();
    const lookups = createSessionLookups(session);
    expect(lookups.stats).toMatchObject({
      toolCallCount: 3,
      failedToolCalls: 2,
      totalInputTokens: 40,
      totalOutputTokens: 10,
      totalRequests: 2,
      totalDurationMs: 300,
      retryCount: 1,
      artifactCount: 1,
    });
    expect(lookups.eventsOfType("tool.execution_complete")).toHaveLength(2);
    expect(lookups.eventsOfType("missing")).toEqual([]);
    const { output } = computeGraders(session);
    const values = Object.fromEntries(output.results.map(result => [result.id, result.value]));
    expect(values).toEqual({
      "tool-success-rate": 1 / 3,
      "tool-failure-count": 2,
      retries: 1,
      loops: 1,
      "trajectory-efficiency": 2 / 3,
      "execution-step-count": 2,
      "execution-duration": 300,
      "working-set-rebuild-factor": 40 / 30,
      "context-growth": 50 / 15,
      "artifact-production": 1,
    });
    expect(output.run.id).toBe("123");
    expect(output.context).toEqual({
      experiments: { model: "fast" },
      evals: { total: 1, yes: 1, no: 0, unknown: 0, byQuestion: { goal: "YES" } },
    });
  });

  it("counts successful safe-output results without superseded dispatches", () => {
    const session = [
      safeOutputResult({ type: "publish", number: 42 }),
      safeOutputResult({ type: "publish", status: "success", url: "https://example.com/42" }, "usage/aw_session.jsonl"),
      safeOutputResult({ type: "failed", number: 7 }),
      safeOutputResult({ type: "failed", status: "failure", url: "https://example.com/failed" }, "usage/aw_session.jsonl"),
      safeOutputResult({ type: "cancelled", status: "cancelled" }, "usage/aw_session.jsonl"),
      safeOutputResult({ type: "skipped", status: "skipped" }, "usage/aw_session.jsonl"),
      safeOutputResult({ type: "legacy", number: 1 }),
    ];

    const lookups = createSessionLookups(session);
    expect(lookups.trace.artifacts.map(artifact => artifact.type)).toEqual(["publish", "legacy"]);
    expect(lookups.stats.artifactCount).toBe(2);
  });

  it("does not read files, execute commands, use environment data, or mutate the session", () => {
    const session = fixture();
    const before = structuredClone(session);
    const freeze = value => {
      if (value && typeof value === "object") {
        Object.values(value).forEach(freeze);
        Object.freeze(value);
      }
    };
    freeze(session);
    for (const name of ["readFileSync", "statSync", "readdirSync", "existsSync", "writeFileSync"]) {
      vi.spyOn(fs, name).mockImplementation(() => {
        throw new Error(`unexpected I/O: ${name}`);
      });
    }
    vi.spyOn(cp, "spawnSync").mockImplementation(() => {
      throw new Error("unexpected subprocess");
    });
    const first = computeGraders(session);
    expect(computeGraders(session)).toEqual(first);
    expect(session).toEqual(before);
    expect(first.output.results.every(result => result.status !== "error")).toBe(true);
  });

  it("enables only cheap graders by default, preserving disables and opt-ins", () => {
    expect(withDefaultGraders([]).map(grader => grader.id)).toEqual(DEFAULT_GRADER_IDS);
    const { output } = computeGraders(
      [],
      [
        { id: "loops", enabled: false },
        { id: "recurrence-determinism", enabled: true },
      ]
    );
    expect(output.results.map(result => result.id)).not.toContain("loops");
    expect(output.results.map(result => result.id)).toContain("recurrence-determinism");
    expect(output.results.find(result => result.id === "recurrence-determinism").status).toBe("unavailable");
    expect(output.results.map(result => result.id)).not.toContain("trajectory-ndtw");
  });

  it("can disable every default without re-enabling them", () => {
    const { output } = computeGraders(
      [],
      DEFAULT_GRADER_IDS.map(id => ({ id, enabled: false }))
    );
    expect(output.results).toEqual([]);
  });

  it.each([
    { id: "custom", source: "inline", script: "return 1" },
    { id: "operational-value", source: "operational-value", run: "test.sh" },
    { id: "loops", source: "builtin", script: "return 1" },
    { id: "loops", source: "builtin", run: "test.sh" },
    { id: "toString" },
    { id: "__proto__" },
  ])("rejects executable or unknown grader configuration: %j", grader => {
    expect(() => computeGraders(fixture(), [grader])).toThrow(/custom graders/);
  });

  it("rejects legacy payloads and malformed sessions instead of producing success-shaped defaults", () => {
    expect(() => computeGraders({ toolCalls: [] })).toThrow(/unified session/);
    expect(() => computeGraders([{ type: "tool.execution_start", data: {} }])).toThrow(/unified session/);
    expect(() => computeGraders([null])).toThrow(/unified session/);
    expect(() => withDefaultGraders([{ id: "loops" }, { id: "loops" }])).toThrow(/duplicate/);
  });

  it("scopes identical tool call IDs to each session", () => {
    const session = [
      event("tool.execution_start", { sessionId: "one", toolCallId: "same", toolName: "view" }),
      event("tool.execution_start", { sessionId: "two", toolCallId: "same", toolName: "view" }),
      event("tool.execution_complete", { sessionId: "two", toolCallId: "same", success: true }),
      event("tool.execution_complete", { sessionId: "one", toolCallId: "same", success: false }),
    ];
    expect(createSessionLookups(session).toolCalls.map(call => call.success)).toEqual([false, true]);
  });

  it("correlates tool calls across staged session files", () => {
    const session = [
      event("tool.execution_start", { sessionId: "one", toolCallId: "same", toolName: "view" }, "agent", "session-a.jsonl"),
      event("tool.execution_complete", { sessionId: "one", toolCallId: "same", success: false }, "agent", "session-b.jsonl"),
    ];
    const lookups = createSessionLookups(session);
    expect(lookups.toolCalls).toHaveLength(1);
    expect(lookups.stats).toMatchObject({ toolCallCount: 1, failedToolCalls: 1 });
  });

  it("does not pair a different known call ID with an unfinished call", () => {
    const session = [event("tool.execution_start", { toolCallId: "one", toolName: "view" }), event("tool.execution_complete", { toolCallId: "two", toolName: "view", success: true })];
    expect(createSessionLookups(session).stats).toMatchObject({ toolCallCount: 2, failedToolCalls: 1 });
  });

  it("separates subagents sharing a parent session and tool IDs", () => {
    const session = [
      event("tool.execution_start", { sessionId: "parent", agentId: "one", toolCallId: "same", toolName: "view" }),
      event("tool.execution_start", { sessionId: "parent", agentId: "two", toolCallId: "same", toolName: "view" }),
      event("tool.execution_complete", { sessionId: "parent", agentId: "two", toolCallId: "same", success: true }),
      event("tool.execution_complete", { sessionId: "parent", agentId: "one", toolCallId: "same", success: false }),
    ];
    expect(createSessionLookups(session).toolCalls.map(call => call.success)).toEqual([false, true]);
    expect(runBuiltinGrader("loops", session, BUILTIN_META.loops).value).toBe(0);
  });

  it("projects trajectory evidence from the session without reading a separate trace", () => {
    const session = [event("trajectory.ir", { states: [{ id: "a" }, { id: "b" }, { id: "a" }] }), event("trajectory.ir", { states: [{ id: "ignored" }] }, "evals")];
    expect(runBuiltinGrader("state-revisit-probability-rep", session, BUILTIN_META["state-revisit-probability-rep"]).value).toBeCloseTo(1 / 3);
    expect(runBuiltinGrader("event-entropy-rate", fixture(), BUILTIN_META["event-entropy-rate"]).value).not.toBeNull();
  });

  it("correlates missing IDs by tool name and retains orphan completions", () => {
    const session = [
      event("tool.execution_start", { toolName: "view", input: { path: "a" } }),
      event("tool.execution_complete", { toolName: "view", success: true }),
      event("tool.execution_start", { toolName: "view", input: { path: "b" } }),
      event("tool.execution_complete", { toolName: "view", success: false }),
      event("tool.execution_complete", { toolName: "edit", success: true }),
    ];
    expect(createSessionLookups(session).stats).toMatchObject({ toolCallCount: 3, failedToolCalls: 1 });
  });

  it("merges gateway-only calls into mixed native traces without duplicating IDs", () => {
    const mixed = [
      event("tool.execution_start", { toolCallId: "native", toolName: "view" }),
      event("tool.execution_complete", { toolCallId: "native", success: true }),
      event("mcp.rpc.request", { serverName: "github", rpcId: "native", toolName: "view", method: "tools/call" }),
      event("mcp.rpc.response", { serverName: "github", rpcId: "native", status: "error" }),
      event("mcp.rpc.request", { serverName: "github", rpcId: "gateway", toolName: "edit", method: "tools/call" }),
      event("mcp.rpc.response", { serverName: "github", rpcId: "gateway", status: "error" }),
      event("mcp.rpc.response", { serverName: "github", rpcId: "initialize", method: "initialize" }),
    ];
    expect(createSessionLookups(mixed).stats).toMatchObject({ toolCallCount: 2, failedToolCalls: 1 });
    const gatewayOnly = [event("mcp.rpc.request", { serverName: "github", rpcId: "a", toolName: "view", method: "tools/call" }), event("mcp.rpc.response", { serverName: "github", rpcId: "a", status: "error" })];
    expect(createSessionLookups(gatewayOnly).stats).toMatchObject({ toolCallCount: 1, failedToolCalls: 1 });
  });

  it("uses canonical usage when proxy telemetry is absent and duration evidence as a fallback", () => {
    const session = [event("usage.report", { usage: { inputTokens: 20, outputTokens: 5 } }), event("execution.result", { durationMs: 50 })];
    expect(createSessionLookups(session).stats).toMatchObject({ totalInputTokens: 20, totalRequests: 1, totalDurationMs: 50 });
    expect(runBuiltinGrader("execution-duration", [], BUILTIN_META["execution-duration"]).status).toBe("unavailable");
    expect(runBuiltinGrader("working-set-rebuild-factor", [], BUILTIN_META["working-set-rebuild-factor"]).status).toBe("unavailable");
  });

  it("compares arguments deterministically regardless of object-key order", () => {
    const session = [event("tool.execution_start", { toolName: "view", input: { a: 1, b: 2 } }), event("tool.execution_start", { toolName: "view", input: { b: 2, a: 1 } })];
    expect(runBuiltinGrader("loops", session, BUILTIN_META.loops).value).toBe(1);
  });

  it("does not fabricate complete statistics from partial or overflowing measurements", () => {
    const partial = [event("firewall.token_usage", { usage: { inputTokens: 2, outputTokens: 1 }, durationMs: 0.5 }), event("firewall.token_usage", { usage: { outputTokens: 1 } })];
    const stats = createSessionLookups(partial).stats;
    expect(stats).toMatchObject({ totalInputTokens: null, totalOutputTokens: 2, totalDurationMs: null });
    for (const id of ["context-growth", "working-set-rebuild-factor", "execution-duration"]) {
      expect(runBuiltinGrader(id, partial, BUILTIN_META[id]).status).toBe("unavailable");
    }
    expect(createSessionLookups(partial.slice(0, 1)).stats.totalDurationMs).toBe(0.5);
    const overflow = [event("usage.report", { usage: { inputTokens: Number.MAX_SAFE_INTEGER, outputTokens: 1 } }), event("usage.report", { usage: { inputTokens: 1, outputTokens: 1 } })];
    expect(createSessionLookups(overflow).stats.totalInputTokens).toBeNull();
    expect(runBuiltinGrader("context-growth", overflow, BUILTIN_META["context-growth"]).status).toBe("unavailable");
  });

  it("keeps default grading fast on a 20,000-call log", () => {
    const session = Array.from({ length: 20000 }, (_, index) => [
      event("tool.execution_start", { toolCallId: String(index), toolName: "view", input: { path: String(index) } }),
      event("tool.execution_complete", { toolCallId: String(index), success: true }),
    ]).flat();
    const started = performance.now();
    const { output } = computeGraders(session);
    expect(performance.now() - started).toBeLessThan(1500);
    expect(output.results.find(result => result.id === "tool-failure-count").value).toBe(0);
    expect(output.results).toHaveLength(DEFAULT_GRADER_IDS.length);
  });

  it("uses the session interface for every registered built-in", () => {
    for (const id of Object.keys(BUILTIN_GRADERS)) {
      expect(runBuiltinGrader(id, fixture(), BUILTIN_META[id]).status).not.toBe("error");
    }
  });

  it("normalizes unavailable and invalid results and evaluates thresholds", () => {
    expect(normalizeResult("loops", Infinity, BUILTIN_META.loops).status).toBe("error");
    expect(normalizeResult("loops", { value: null, message: "missing" }, BUILTIN_META.loops)).toMatchObject({ status: "unavailable", message: "missing" });
    expect(evaluateThreshold(0.8, "higher_is_better", 0.8)).toBe(true);
    expect(evaluateThreshold(2, "lower_is_better", 1)).toBe(false);
    expect(evaluateThreshold(2, "lower_is_better", undefined)).toBeNull();
  });
});
