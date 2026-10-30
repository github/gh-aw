const { createSessionLookups } = require("./grader_session.cjs");
const { runCustomGraders } = require("./custom_graders.cjs");

const event = (type, data, extra = {}) => ({ type, data, provenance: { component: "harness", phase: "agent", path: "retry.json", index: 0, ...extra } });
const attempt = (number, extra = {}, provenance = {}) => event("harness.attempt", { attempt: number, layer: "harness", exitCode: 0, failureClasses: [], ...extra }, provenance);
const tool = (type, attemptId, data = {}) => event(type, { toolCallId: "same", toolName: "view", ...data }, { component: "agent", attemptId });

describe("retry session consumers", () => {
  it("counts retry attempts, not first attempts, summaries, continuations, or detection", () => {
    const session = [
      attempt(1),
      attempt(2, { outcome: "running" }),
      attempt(2, { outcome: "success" }),
      attempt(1, { layer: "startup" }),
      attempt(2, { layer: "startup" }),
      attempt(1, {}, { path: "other-invocation.json" }),
      attempt(2, {}, { phase: "detection" }),
      event("harness.retry", { attemptResults: [{ attempt: 1 }, { attempt: 2 }], startupAttemptResults: [{ attempt: 1 }, { attempt: 2 }] }),
      event("session.resume", {}),
    ];
    const lookups = createSessionLookups(session);
    expect(lookups.stats.retryCount).toBe(2);
    expect(lookups.trace.attempts).toHaveLength(6);
    expect(lookups.trace.attempts[1].outcome).toBe("success");
  });

  it("uses legacy summary attempts per layer and preserves MCP retry accounting", () => {
    const session = [
      attempt(1),
      attempt(2),
      event("harness.retry", { attemptResults: [{ attempt: 1 }, { attempt: 2 }, { attempt: 3 }], startupAttemptResults: [{ attempt: 1 }, { attempt: 2, backoffMs: 100 }] }),
      event("mcp.event", { event: "retry" }, { component: "gateway" }),
    ];
    expect(createSessionLookups(session).stats.retryCount).toBe(4);
    expect(createSessionLookups(session).trace.attempts.at(-1)).toMatchObject({ layer: "startup", backoffMs: 100 });
  });

  it("exposes immutable attempt history in isolated scripts without file access", () => {
    const result = runCustomGraders(
      [attempt(1), attempt(2, { attemptId: "retry-2", outcome: "interrupted" })],
      [
        {
          id: "history",
          source: "inline",
          script: 'const history = helpers.createSessionLookups(session).trace.attempts; return history.find(a => a.attemptId === "retry-2").outcome === "interrupted" && Object.isFrozen(history) ? history.length : 0;',
        },
      ]
    );
    expect(result.output.results[0]).toMatchObject({ value: 2, status: "pass" });
  });

  it("separates restarted tools with reused IDs, including gateway observations", () => {
    const session = [
      attempt(1, { attemptId: "a" }),
      attempt(2, { attemptId: "b", strategy: "restart" }),
      tool("tool.execution_start", "a"),
      tool("tool.execution_start", "b"),
      tool("tool.execution_complete", "b", { success: true }),
      tool("tool.execution_complete", "a", { success: false }),
      event("mcp.tool_call", { toolCallId: "same", toolName: "view" }, { component: "gateway", attemptId: "c" }),
    ];
    const lookups = createSessionLookups(session);
    expect(lookups.toolCalls.map(call => call.success)).toEqual([false, true, undefined]);
    expect(lookups.toolCallsById.size).toBe(2);
  });

  it("correlates a resumed completion with its prior attempt", () => {
    const session = [
      attempt(1, { attemptId: "a" }),
      attempt(2, { attemptId: "b", strategy: "resume" }),
      tool("tool.execution_start", "a", { sessionId: "observed" }),
      tool("tool.execution_complete", "b", { sessionId: "observed", success: true }),
    ];
    expect(createSessionLookups(session).toolCalls).toHaveLength(1);
    expect(createSessionLookups(session).stats.failedToolCalls).toBe(0);
    expect(createSessionLookups([session[1], session[0], ...session.slice(2)]).toolCalls).toHaveLength(1);
  });

  it("keeps gateway request IDs and anonymous native calls separate after restart", () => {
    const session = [
      attempt(1, { attemptId: "a" }),
      attempt(2, { attemptId: "b", strategy: "restart" }),
      ...["a", "b"].map(attemptId => event("mcp.rpc.request", { serverName: "github", rpcId: 1, toolName: "edit", method: "tools/call" }, { component: "mcp", attemptId })),
      event("mcp.rpc.response", { serverName: "github", rpcId: 1, status: "success" }, { component: "mcp", attemptId: "b" }),
      event("mcp.rpc.response", { serverName: "github", rpcId: 1, status: "error" }, { component: "mcp", attemptId: "a" }),
      tool("tool.execution_start", "a", { toolCallId: undefined }),
      tool("tool.execution_complete", "b", { toolCallId: undefined, success: true }),
    ];
    expect(createSessionLookups(session).stats).toMatchObject({ toolCallCount: 4, failedToolCalls: 2 });
  });

  it("deduplicates summaries independent of source ordering and separates invocation paths", () => {
    const session = [
      event("harness.retry", { attemptResults: [{ attempt: 1 }, { attempt: 2 }] }),
      attempt(1, { attemptId: "a" }),
      attempt(2, { attemptId: "b" }),
      event("harness.retry", { attemptResults: [{ attempt: 1 }, { attempt: 2 }] }, { path: "another-retry.json" }),
    ];
    expect(createSessionLookups(session).trace.attempts).toHaveLength(4);
    expect(createSessionLookups(session).stats.retryCount).toBe(2);
  });

  it("isolates reused harness attempt IDs across startup invocations", () => {
    const session = ["startup:1", "startup:2"].flatMap(startupAttemptId => [
      attempt(1, { attemptId: "harness:1", startupAttemptId }),
      attempt(2, { attemptId: "harness:2", startupAttemptId, strategy: "resume" }),
      ...[tool("tool.execution_start", "harness:1", { sessionId: "observed" }), tool("tool.execution_complete", "harness:2", { sessionId: "observed", success: true })].map(item => ({
        ...item,
        provenance: { ...item.provenance, startupAttemptId },
      })),
    ]);
    const lookups = createSessionLookups(session);
    expect(lookups.trace.attempts).toHaveLength(4);
    expect(lookups.stats).toMatchObject({ retryCount: 2, toolCallCount: 2, failedToolCalls: 0 });
    expect(lookups.toolCallsById.size).toBe(2);
  });

  it("uses compound IDs alone to keep resume inside its startup scope", () => {
    const session = [
      ...[1, 2].flatMap(startup => [attempt(1, { attemptId: `startup:${startup}:harness:1` }), attempt(2, { attemptId: `startup:${startup}:harness:2`, strategy: "resume" })]),
      tool("tool.execution_start", "startup:1:harness:1", { sessionId: "observed" }),
      tool("tool.execution_start", "startup:2:harness:1", { sessionId: "observed" }),
      tool("tool.execution_complete", "startup:1:harness:2", { sessionId: "observed", success: false }),
      tool("tool.execution_complete", "startup:2:harness:2", { sessionId: "observed", success: true }),
    ];
    const lookups = createSessionLookups(session);
    expect(lookups.toolCalls.map(call => call.success)).toEqual([false, true]);
    expect(lookups.stats).toMatchObject({ retryCount: 2, toolCallCount: 2 });
  });

  it("retains distinct observed intervals even when an attempt ID is reused", () => {
    const session = [
      attempt(2, { attemptId: "harness:2", startedAt: 10, outcome: "running" }),
      attempt(2, { attemptId: "harness:2", startedAt: 10, outcome: "failure", finishedAt: 20 }),
      attempt(2, { attemptId: "harness:2", startedAt: 30, outcome: "success", finishedAt: 40 }),
    ];
    const lookups = createSessionLookups(session);
    expect(lookups.trace.attempts.map(item => item.outcome)).toEqual(["failure", "success"]);
    expect(lookups.stats.retryCount).toBe(2);
  });

  it("counts native engine retry observations separately from process retries and errors", () => {
    const native = (attempt, index, phase = "agent") => event("engine.retry", { attempt, maxRetries: 5, backoffMs: 100 }, { component: "agent", index, phase });
    const session = [attempt(1), attempt(2), native(1, 0), native(2, 1), native(1, 2), native(1, 0, "detection"), native(0, 3), event("session.result", { is_error: true }, { component: "agent" })];
    const lookups = createSessionLookups(session);
    expect(lookups.stats).toMatchObject({ retryCount: 1, harnessRetryCount: 1, engineRetryCount: 3 });
    expect(lookups.trace.engineRetries.map(retry => retry.attempt)).toEqual([1, 2, 1, 1]);
    const result = runCustomGraders(session, [{ id: "native-retries", source: "inline", script: "return helpers.createSessionLookups(session).trace.engineRetries.length;" }]);
    expect(result.output.results[0]).toMatchObject({ value: 4, status: "pass" });
  });

  it("counts an observed retry without counters without inventing a process attempt", () => {
    const lookups = createSessionLookups([event("engine.retry", { sourceEngine: "claude", errors: ["retry"] }, { component: "agent" })]);
    expect(lookups.stats).toMatchObject({ retryCount: 0, harnessRetryCount: 0, engineRetryCount: 1 });
    expect(lookups.trace.attempts).toEqual([]);
    expect(lookups.trace.engineRetries).toHaveLength(1);
    expect(lookups.trace.engineRetries[0]).not.toHaveProperty("attempt");
  });

  it("replays saved legacy retry diagnostics through the same history lookup", () => {
    const saved = event("session.result", { engineRetry: { attempt: 2, maxRetries: 5 } }, { component: "agent", index: 7 });
    const original = structuredClone(saved);
    const lookups = createSessionLookups([saved]);
    expect(lookups.stats.engineRetryCount).toBe(1);
    expect(lookups.trace.engineRetries).toMatchObject([{ attempt: 2, maxRetries: 5, eventIndex: 0, provenance: { index: 7 } }]);
    expect(saved).toEqual(original);
  });
  it.each([undefined, "different"])("does not infer resumed identity when sessionId is %s", sessionId => {
    const session = [
      attempt(1, { attemptId: "a" }),
      attempt(2, { attemptId: "b", strategy: "resume" }),
      tool("tool.execution_start", "a", { sessionId: sessionId ? "original" : undefined }),
      tool("tool.execution_complete", "b", { sessionId, success: true }),
    ];
    expect(createSessionLookups(session).toolCalls).toHaveLength(2);
  });
});
