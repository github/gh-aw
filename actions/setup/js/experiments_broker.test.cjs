import { afterEach, describe, expect, it, vi } from "vitest";
const { assign, report } = require("./experiments_broker.cjs");
const { loadState } = require("./pick_experiment.cjs");
const { buildFrictionSummary } = require("./generate_usage_activity_summary.cjs");
const fs = require("fs");

const START = Date.parse("2026-11-20T12:00:00Z");
const jwt = claims => [JSON.stringify({ alg: "synthetic" }), JSON.stringify(claims), "synthetic-signature"].map(value => Buffer.from(value).toString("base64url")).join(".");
const OIDC = jwt({ exp: START / 1000 + 300 });
const SPEC = { feature: ["control", "candidate"], style: { variants: ["brief", "full"], weight: [70, 30], description: "Synthetic experiment" } };
const DECISION = { assignments: { feature: "candidate", style: "brief" }, counts: { feature: { control: 7, candidate: 8 }, style: { brief: 9, full: 6 } } };
const DIRECTORY = "experiments-broker-test";
const ASSIGNMENTS_FILE = `${DIRECTORY}/assignments.json`;
const RESULTS_FILE = `${DIRECTORY}/summary.json`;
const METADATA_FILE = `${DIRECTORY}/aw_info.json`;
const json = value => new Response(JSON.stringify(value));
const makeEnv = () => ({
  GH_AW_EXPERIMENTS_BROKER_URL: "https://broker.example.test/experiments?route=synthetic",
  ACTIONS_ID_TOKEN_REQUEST_URL: "https://oidc.example.test/token?job=synthetic",
  ACTIONS_ID_TOKEN_REQUEST_TOKEN: "synthetic-bootstrap",
  GH_AW_EXPERIMENT_SPEC: JSON.stringify(SPEC),
  GH_AW_EXPERIMENT_STATE_DIR: DIRECTORY,
  GH_AW_EXPERIMENT_RESULTS_FILE: RESULTS_FILE,
  GH_AW_EXPERIMENT_METADATA_FILE: METADATA_FILE,
  GH_AW_EXPERIMENT_AGENT_RESULT: "success",
  GH_AW_EXPERIMENT_INVOCATION_ID: "default",
  GH_AW_WORKFLOW_ID: "experiment-workflow",
  GH_AW_WORKFLOW_REF: "acme/project/.github/workflows/experiment-workflow.lock.yml@refs/heads/main",
  GITHUB_REPOSITORY: "acme/project",
  GITHUB_SERVER_URL: "https://github.example.test",
  GITHUB_RUN_ID: "123",
  GITHUB_RUN_ATTEMPT: "2",
  GITHUB_JOB: "activation",
});

function setup(overrides = {}) {
  const files = new Map([
    [ASSIGNMENTS_FILE, JSON.stringify(DECISION.assignments)],
    [RESULTS_FILE, JSON.stringify({ schema: "usage-activity-summary/v1", session: { turns: 2 }, experiments: { assignments: DECISION.assignments } })],
    [
      METADATA_FILE,
      JSON.stringify({
        run_id: 123,
        run_attempt: "2",
        repository: "acme/project",
        server_url: "https://github.example.test",
        workflow_id: makeEnv().GH_AW_WORKFLOW_REF,
        workflow_ref: makeEnv().GH_AW_WORKFLOW_REF,
        model: "synthetic",
      }),
    ],
  ]);
  const core = { setSecret: vi.fn(), setOutput: vi.fn(), exportVariable: vi.fn(), info: vi.fn(), warning: vi.fn(), error: vi.fn() };
  const fsImpl = {
    statSync: vi.fn(file => {
      if (!files.has(file)) throw new Error("sensitive-detail");
      return { size: Buffer.byteLength(files.get(file)) };
    }),
    readFileSync: vi.fn(file => {
      if (!files.has(file)) throw new Error("sensitive-detail");
      return files.get(file);
    }),
    mkdirSync: vi.fn(),
    writeFileSync: vi.fn((file, value) => files.set(file, value)),
  };
  const fetchImpl = vi.fn(async (_url, init) => {
    if (init.method !== "POST") return json({ value: OIDC });
    return JSON.parse(init.body).operation === "assign" ? json(DECISION) : new Response(null, { status: 204 });
  });
  return { files, core, env: makeEnv(), fsImpl, fetchImpl, now: () => START, ...overrides };
}

async function fails(promise, kind) {
  const error = await promise.then(
    () => {
      throw new Error("Expected rejection");
    },
    error => error
  );
  expect(error.code).toBe(`ERR_EXPERIMENTS_BROKER_${kind}`);
  expect(error.cause).toBeUndefined();
  for (const secret of [OIDC, "synthetic-bootstrap", "broker.example.test", "oidc.example.test", "sensitive-detail"]) expect(`${error.stack} ${JSON.stringify(error)}`).not.toContain(secret);
}

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("experiments broker assignment", () => {
  it("uses the exact endpoint as OIDC audience and persists only compatible assignments and cumulative state", async () => {
    const fixture = setup();
    fixture.env.GITHUB_AW_OTEL_TRACE_ID = "a".repeat(32);
    fixture.env.GITHUB_AW_OTEL_PARENT_SPAN_ID = "b".repeat(16);
    fixture.env.OTEL_RESOURCE_ATTRIBUTES = "existing=value";
    expect(await assign(fixture)).toEqual(DECISION);
    expect(fixture.fetchImpl).toHaveBeenCalledTimes(2);
    const [oidcUrl, oidcInit] = fixture.fetchImpl.mock.calls[0];
    expect(oidcUrl.searchParams.get("audience")).toBe(fixture.env.GH_AW_EXPERIMENTS_BROKER_URL);
    expect(oidcUrl.searchParams.get("job")).toBe("synthetic");
    expect(oidcInit).toMatchObject({ redirect: "manual", headers: { authorization: "bearer synthetic-bootstrap" }, signal: expect.any(AbortSignal) });
    expect(oidcInit.headers).not.toHaveProperty("traceparent");
    expect(oidcInit.headers).not.toHaveProperty("baggage");
    const [endpoint, init] = fixture.fetchImpl.mock.calls[1];
    expect(endpoint).toBe(fixture.env.GH_AW_EXPERIMENTS_BROKER_URL);
    expect(init).toMatchObject({
      method: "POST",
      redirect: "manual",
      signal: oidcInit.signal,
      headers: {
        "content-type": "application/json",
        accept: "application/json",
        "cache-control": "no-store",
        traceparent: `00-${"a".repeat(32)}-${"b".repeat(16)}-01`,
        baggage: "cicd.pipeline.task.name=activation",
      },
    });
    expect(JSON.parse(init.body)).toEqual({
      operation: "assign",
      oidc_token: OIDC,
      job_id: "activation",
      workflow_id: "experiment-workflow",
      workflow_ref: fixture.env.GH_AW_WORKFLOW_REF,
      invocation_id: "default",
      run_id: "123",
      run_attempt: "2",
      repository: "acme/project",
      server_url: "https://github.example.test",
      experiments: SPEC,
    });
    expect(fixture.core.setSecret.mock.calls).toEqual([[OIDC]]);
    expect(fixture.core.setOutput.mock.calls).toEqual([
      ["feature", "candidate"],
      ["style", "brief"],
      ["experiments", JSON.stringify(DECISION.assignments)],
    ]);
    expect(fixture.core.exportVariable).toHaveBeenCalledWith("OTEL_RESOURCE_ATTRIBUTES", "existing=value,experiment.feature=candidate,experiment.style=brief");
    expect(JSON.parse(fixture.files.get(ASSIGNMENTS_FILE))).toEqual(DECISION.assignments);
    const state = JSON.parse(fixture.files.get(`${DIRECTORY}/state.jsonl`));
    expect(state).toEqual({ counts: DECISION.counts, runs: [{ run_id: "123", timestamp: "2026-11-20T12:00:00.000Z", assignments: DECISION.assignments }] });
    vi.spyOn(fs, "readFileSync").mockImplementation(fixture.fsImpl.readFileSync);
    expect(loadState(`${DIRECTORY}/state.jsonl`)).toMatchObject(state);
    for (const log of [fixture.core.info, fixture.core.warning, fixture.core.error]) expect(log).not.toHaveBeenCalled();
    for (const raw of fixture.files.values()) expect(raw).not.toContain(OIDC);
  });

  it("honors custom state paths, preserves zero and maximum counts, and replaces rather than appends history", async () => {
    const fixture = setup();
    fixture.env.GH_AW_EXPERIMENT_STATE_FILE = `${DIRECTORY}/custom/state.jsonl`;
    const response = structuredClone(DECISION);
    response.counts.feature = { control: 0, candidate: Number.MAX_SAFE_INTEGER };
    fixture.fetchImpl.mockImplementation(async (_url, init) => (init.method === "POST" ? json(response) : json({ value: OIDC })));
    await assign(fixture);
    await assign(fixture);
    const state = JSON.parse(fixture.files.get(fixture.env.GH_AW_EXPERIMENT_STATE_FILE));
    expect(state.counts).toEqual(response.counts);
    expect(state.runs).toHaveLength(1);
    expect(fixture.fetchImpl).toHaveBeenCalledTimes(4);
    expect(fixture.fetchImpl.mock.calls[1][1].body).toBe(fixture.fetchImpl.mock.calls[3][1].body);
  });

  it("uses the same invocation hint for assignment and report, distinct from other reusable invocations", async () => {
    for (const invocation of ["a1b2c3d4-", "e5f6a7b8-"]) {
      const fixture = setup();
      fixture.env.GH_AW_EXPERIMENT_INVOCATION_ID = invocation;
      await assign(fixture);
      fixture.env.GITHUB_JOB = "report_experiments";
      await report(fixture);
      const assigned = JSON.parse(fixture.fetchImpl.mock.calls[1][1].body);
      const reported = JSON.parse(fixture.fetchImpl.mock.calls[3][1].body);
      expect(assigned.invocation_id).toBe(invocation);
      expect(reported.invocation_id).toBe(invocation);
      expect(assigned.workflow_ref).toBe(reported.workflow_ref);
      expect(assigned.run_id).toBe(reported.run_id);
      expect(assigned.run_attempt).toBe(reported.run_attempt);
    }
  });

  it("does not override broker decisions for date windows, weights, or continual policy", async () => {
    const fixture = setup();
    fixture.env.GH_AW_EXPERIMENT_SPEC = JSON.stringify({ ...SPEC, feature: { variants: ["control", "candidate"], start_date: "2099-01-01", weight: [100, 0], continual: { seed: "synthetic", ramp: [0, 100] } } });
    await assign(fixture);
    expect(fixture.core.setOutput).toHaveBeenCalledWith("feature", "candidate");
  });

  it("encodes delimiters and Unicode in OTEL attributes without altering assignment outputs", async () => {
    const variant = "a,b=c% d/é";
    const fixture = setup({ env: { ...makeEnv(), GH_AW_EXPERIMENT_SPEC: JSON.stringify({ feature: ["control", variant] }) } });
    fixture.fetchImpl.mockImplementation(async (_url, init) => (init.method === "POST" ? json({ assignments: { feature: variant }, counts: { feature: { control: 0, [variant]: 1 } } }) : json({ value: OIDC })));
    await assign(fixture);
    expect(fixture.core.setOutput).toHaveBeenCalledWith("feature", variant);
    expect(fixture.core.exportVariable).toHaveBeenCalledWith("OTEL_RESOURCE_ATTRIBUTES", `experiment.feature=${encodeURIComponent(variant)}`);
  });

  it("preserves the final aggregate output for a valid experiment named experiments", async () => {
    const fixture = setup({ env: { ...makeEnv(), GH_AW_EXPERIMENT_SPEC: '{"experiments":["control","candidate"]}' } });
    const selected = { experiments: "candidate" };
    fixture.fetchImpl.mockImplementation(async (_url, init) => (init.method === "POST" ? json({ assignments: selected, counts: { experiments: { control: 0, candidate: 1 } } }) : json({ value: OIDC })));
    await assign(fixture);
    expect(fixture.core.setOutput.mock.calls).toEqual([
      ["experiments", "candidate"],
      ["experiments", JSON.stringify(selected)],
    ]);
  });

  it("handles prototype-like declared names and variants as data", async () => {
    const fixture = setup({ env: { ...makeEnv(), GH_AW_EXPERIMENT_SPEC: '{"__proto__":["__proto__","constructor"]}' } });
    fixture.fetchImpl.mockImplementation(async (_url, init) => (init.method === "POST" ? new Response('{"assignments":{"__proto__":"__proto__"},"counts":{"__proto__":{"__proto__":1,"constructor":0}}}') : json({ value: OIDC })));
    await assign(fixture);
    expect(JSON.parse(fixture.files.get(ASSIGNMENTS_FILE))).toEqual(JSON.parse('{"__proto__":"__proto__"}'));
    expect({}.polluted).toBeUndefined();
  });

  it.each([
    null,
    [],
    {},
    { assignments: DECISION.assignments },
    { counts: DECISION.counts },
    { ...DECISION, token: "sensitive-detail" },
    { ...DECISION, assignments: {} },
    { ...DECISION, assignments: { feature: "candidate" } },
    { ...DECISION, assignments: { ...DECISION.assignments, unknown: "control" } },
    { ...DECISION, assignments: { ...DECISION.assignments, feature: "unknown" } },
    { ...DECISION, assignments: { ...DECISION.assignments, feature: 1 } },
    { ...DECISION, counts: {} },
    { ...DECISION, counts: { ...DECISION.counts, unknown: { control: 1 } } },
    { ...DECISION, counts: { feature: { control: 1 }, style: DECISION.counts.style } },
    { ...DECISION, counts: { feature: { control: 1, candidate: 1, extra: 0 }, style: DECISION.counts.style } },
    { ...DECISION, counts: { feature: [], style: DECISION.counts.style } },
    ...[-1, 0.1, Number.MAX_SAFE_INTEGER + 1, "1", null].map(count => ({ ...DECISION, counts: { feature: { control: count, candidate: 1 }, style: DECISION.counts.style } })),
  ])("rejects incomplete or unrecognized assignment responses %# before writing outputs", async value => {
    const fixture = setup();
    fixture.fetchImpl.mockImplementation(async (_url, init) => (init.method === "POST" ? json(value) : json({ value: OIDC })));
    await fails(assign(fixture), "MALFORMED_RESPONSE");
    expect(fixture.fsImpl.writeFileSync).not.toHaveBeenCalled();
    expect(fixture.core.setOutput).not.toHaveBeenCalled();
    expect(fixture.core.exportVariable).not.toHaveBeenCalled();
  });

  it.each(["", "{", "null", "[]", "{}", '{"feature":["one"]}', '{"feature":["a","a"]}', '{"feature":["a",1]}', '{"feature":["a","\\ud800"]}', '{"invalid-name":["a","b"]}', '{"feature":{"weight":[1,1]}}'])(
    "rejects malformed or missing experiment declarations %#",
    async raw => {
      const fixture = setup({ env: { ...makeEnv(), GH_AW_EXPERIMENT_SPEC: raw } });
      await fails(assign(fixture), "CONFIGURATION");
      expect(fixture.fetchImpl).not.toHaveBeenCalled();
    }
  );

  it("sanitizes artifact write errors and publishes no outputs on failure", async () => {
    const fixture = setup();
    fixture.fsImpl.writeFileSync.mockImplementation(() => {
      throw new Error(`sensitive-detail ${OIDC}`);
    });
    await fails(assign(fixture), "STORAGE");
    expect(fixture.core.setOutput).not.toHaveBeenCalled();
  });
});

describe("experiments broker reporting", () => {
  it("reports only fixed aggregate numeric/status observations, never raw logs, prompts, arbitrary strings, or items", async () => {
    const fixture = setup();
    fixture.env.GITHUB_JOB = "report_experiments";
    const summary = {
      schema: "usage-activity-summary/v1",
      prompt: "sensitive-detail",
      token: OIDC,
      status: "sensitive-detail",
      session: {
        total_events: 8,
        turns: 2,
        assistant_messages: 3,
        reasoning_events: 1,
        tool_execution_starts: 1,
        tool_execution_completes: 1,
        failed_tool_executions: 0,
        session_starts: 1,
        session_shutdowns: 1,
        events: ["sensitive-detail"],
      },
      firewall: { total_requests: 4, allowed_requests: 3, blocked_requests: 1, allowed_domains: ["sensitive-detail"] },
      gateway: { total_calls: 2, failed_calls: 1, total_input_size: 20, total_output_size: 30, max_input_size: 12, max_output_size: 18, total_duration_ms: 0.5, max_duration_ms: 0.3, tool_calls: [{ arguments: "sensitive-detail" }] },
      integrity: { total_filtered: 1, filtered_reason_counts: { "sensitive-detail": 1 } },
      safe_outputs: { total_items: 1, items: [{ body: "sensitive-detail" }], items_by_type: { "sensitive-detail": 1 } },
      ledger: { transactions_added: 0, compaction: { replacement: "sensitive-detail" } },
      friction: {
        measurement_state: "causal",
        dimension_states: { aic: "causal", tokens: "causal", turns: "measured", tool_calls: "measured", latency_ms: "measured", raw: "sensitive-detail" },
        counted_occurrences: 2,
        cost: { aic: 1.25, turns: 1, tool_calls: 2, latency_ms: 0.5, tokens: { input: 99 }, raw: "sensitive-detail" },
        total_run_aic: 10,
        friction_ratio: 0.125,
        drivers: ["sensitive-detail"],
      },
      experiments: { assignments: DECISION.assignments, unknown: "sensitive-detail" },
    };
    fixture.files.set(RESULTS_FILE, JSON.stringify(summary));
    await report(fixture);
    const body = JSON.parse(fixture.fetchImpl.mock.calls[1][1].body);
    expect(body).toMatchObject({ operation: "report", assignments: DECISION.assignments, job_id: "report_experiments", workflow_id: "experiment-workflow", run_id: "123", run_attempt: "2", oidc_token: OIDC });
    expect(body).not.toHaveProperty("experiments");
    expect(body.results).toEqual({
      session: { total_events: 8, session_starts: 1, session_shutdowns: 1, turns: 2, assistant_messages: 3, reasoning_events: 1, tool_execution_starts: 1, tool_execution_completes: 1, failed_tool_executions: 0 },
      firewall: { total_requests: 4, allowed_requests: 3, blocked_requests: 1 },
      gateway: { total_calls: 2, failed_calls: 1, total_input_size: 20, total_output_size: 30, max_input_size: 12, max_output_size: 18, total_duration_ms: 0.5, max_duration_ms: 0.3 },
      integrity: { total_filtered: 1 },
      safe_outputs: { total_items: 1 },
      ledger: { transactions_added: 0 },
      friction: {
        counted_occurrences: 2,
        total_run_aic: 10,
        friction_ratio: 0.125,
        measurement_state: "causal",
        dimension_states: { aic: "causal", tokens: "causal", turns: "measured", tool_calls: "measured", latency_ms: "measured" },
        cost: { aic: 1.25, turns: 1, tool_calls: 2, latency_ms: 0.5, tokens: { input: 99 } },
      },
      agent_result: "success",
    });
    expect(JSON.stringify(body.results)).not.toContain("sensitive-detail");
    expect(JSON.stringify(body.results)).not.toContain(OIDC);
    expect(fixture.fsImpl.writeFileSync).not.toHaveBeenCalled();
    expect(fixture.core.setOutput).not.toHaveBeenCalled();
    expect(fixture.core.exportVariable).not.toHaveBeenCalled();
  });

  it("keeps unavailable sections absent rather than claiming zero, and repeats identical reports without retries", async () => {
    const fixture = setup();
    fixture.files.set(RESULTS_FILE, JSON.stringify({ schema: "usage-activity-summary/v1", session: null, friction: { measurement_state: "unavailable" }, experiments: { assignments: DECISION.assignments } }));
    await report(fixture);
    await report(fixture);
    expect(JSON.parse(fixture.fetchImpl.mock.calls[1][1].body).results).toEqual({ friction: { measurement_state: "unavailable" }, agent_result: "success" });
    expect(fixture.fetchImpl.mock.calls[1][1].body).toBe(fixture.fetchImpl.mock.calls[3][1].body);
    expect(fixture.fetchImpl).toHaveBeenCalledTimes(4);
  });

  it("validates assignments without a spec if it is unavailable", async () => {
    const fixture = setup();
    delete fixture.env.GH_AW_EXPERIMENT_SPEC;
    await report(fixture);
    expect(JSON.parse(fixture.fetchImpl.mock.calls[1][1].body).assignments).toEqual(DECISION.assignments);
  });

  it.each([
    { gateway: null, integrity: null, session: null, firewall: null },
    { gateway: { tool_calls: [{ tool_call_id: "synthetic", server_name: "github", tool_name: "issue_read", outcome: "failure", duration_ms: 42 }] }, integrity: null, session: null, firewall: null },
  ])("preserves actual friction dimension availability without reporting placeholder zero costs %#", async activity => {
    vi.stubGlobal("core", { warning: vi.fn() });
    const friction = buildFrictionSummary(activity, `${DIRECTORY}/missing-token-usage.jsonl`);
    expect(friction.cost.aic).toBe(0);
    expect(friction.cost.tokens.total).toBe(0);
    expect(friction.dimension_states.aic).toBe("unavailable");
    expect(friction.dimension_states.tokens).toBe("unavailable");
    const fixture = setup();
    fixture.files.set(RESULTS_FILE, JSON.stringify({ schema: "usage-activity-summary/v1", experiments: { assignments: DECISION.assignments }, friction }));
    await report(fixture);
    const result = JSON.parse(fixture.fetchImpl.mock.calls[1][1].body).results.friction;
    expect(result.dimension_states).toEqual(friction.dimension_states);
    expect(result.cost || {}).not.toHaveProperty("aic");
    expect(result.cost || {}).not.toHaveProperty("tokens");
    for (const dimension of ["turns", "tool_calls", "latency_ms"]) {
      if (["measured", "causal", "statistical"].includes(friction.dimension_states[dimension])) expect(result.cost[dimension]).toBe(friction.cost[dimension]);
      else expect(result.cost || {}).not.toHaveProperty(dimension);
    }
  });

  it("omits unsupported, unknown, and partial dimensions while retaining valid measured zero observations", async () => {
    const fixture = setup();
    fixture.files.set(
      RESULTS_FILE,
      JSON.stringify({
        schema: "usage-activity-summary/v1",
        experiments: { assignments: DECISION.assignments },
        friction: {
          dimension_states: { aic: "unavailable", tokens: "unsupported", tool_calls: "measured" },
          cost: { aic: 0, tokens: { total: 0 }, turns: 0, tool_calls: 0, latency_ms: 0 },
          total_run_aic: 0,
          total_run_aic_partial: true,
          friction_ratio: 0,
        },
      })
    );
    await report(fixture);
    expect(JSON.parse(fixture.fetchImpl.mock.calls[1][1].body).results.friction).toEqual({ dimension_states: { aic: "unavailable", tokens: "unsupported", tool_calls: "measured" }, cost: { tool_calls: 0 } });
  });

  it.each(["success", "failure", "cancelled", "skipped"])("reports the compiler-provided %s agent result", async status => {
    const fixture = setup();
    fixture.env.GH_AW_EXPERIMENT_AGENT_RESULT = status;
    await report(fixture);
    expect(JSON.parse(fixture.fetchImpl.mock.calls[1][1].body).results.agent_result).toBe(status);
  });

  it.each(["", "unknown", "sensitive-detail", undefined])("rejects missing or unrecognized agent status %#", async status => {
    const fixture = setup();
    fixture.env.GH_AW_EXPERIMENT_AGENT_RESULT = status;
    await fails(report(fixture), "INPUT");
    expect(fixture.fetchImpl).not.toHaveBeenCalled();
  });

  it.each([undefined, null, {}, { assignments: {} }, { assignments: { feature: "control", style: "brief" } }, { assignments: { ...DECISION.assignments, extra: "unknown" } }])(
    "rejects missing or mismatched summary assignments %#",
    async experiments => {
      const fixture = setup();
      fixture.files.set(RESULTS_FILE, JSON.stringify({ schema: "usage-activity-summary/v1", experiments }));
      await fails(report(fixture), "INPUT");
      expect(fixture.fetchImpl).not.toHaveBeenCalled();
    }
  );

  it.each([
    ["run_id", 124],
    ["run_id", "123"],
    ["run_id", null],
    ["run_attempt", "1"],
    ["repository", "another/project"],
    ["server_url", "https://another.example.test"],
    ["workflow_id", "another-workflow"],
    ["workflow_ref", "acme/project/.github/workflows/another.lock.yml@refs/heads/main"],
  ])("rejects stale or cross-producer metadata %s=%s", async (key, value) => {
    const fixture = setup();
    const metadata = JSON.parse(fixture.files.get(METADATA_FILE));
    metadata[key] = value;
    fixture.files.set(METADATA_FILE, JSON.stringify(metadata));
    await fails(report(fixture), "INPUT");
    expect(fixture.fetchImpl).not.toHaveBeenCalled();
  });

  it.each([null, [], {}, { feature: "candidate" }, { ...DECISION.assignments, feature: "unknown" }, { ...DECISION.assignments, unknown: "control" }])("fails closed on invalid assignments %#", async value => {
    const fixture = setup();
    fixture.files.set(ASSIGNMENTS_FILE, JSON.stringify(value));
    await fails(report(fixture), "INPUT");
    expect(fixture.fetchImpl).not.toHaveBeenCalled();
  });

  it.each([
    null,
    [],
    {},
    { schema: "unknown" },
    { schema: "usage-activity-summary/v1", session: [] },
    ...[-1, 0.5, "1", null, Number.MAX_SAFE_INTEGER + 1].map(turns => ({ schema: "usage-activity-summary/v1", session: { turns } })),
    { schema: "usage-activity-summary/v1", gateway: { total_duration_ms: -1 } },
    { schema: "usage-activity-summary/v1", friction: { measurement_state: "sensitive-detail" } },
    { schema: "usage-activity-summary/v1", friction: { cost: null } },
    { schema: "usage-activity-summary/v1", friction: { cost: { aic: "1" } } },
    { schema: "usage-activity-summary/v1", friction: { cost: { turns: 0.5 } } },
    { schema: "usage-activity-summary/v1", friction: { cost: { tokens: { total: 0.5 } } } },
    { schema: "usage-activity-summary/v1", friction: { cost: { tokens: null } } },
    { schema: "usage-activity-summary/v1", friction: { dimension_states: { tokens: "sensitive-detail" } } },
    { schema: "usage-activity-summary/v1", friction: { dimension_states: null } },
    { schema: "usage-activity-summary/v1", friction: { total_run_aic_partial: "true" } },
  ])("rejects malformed observations %# before identity acquisition", async summary => {
    const fixture = setup();
    fixture.files.set(RESULTS_FILE, JSON.stringify(summary && !Array.isArray(summary) ? { ...summary, experiments: { assignments: DECISION.assignments } } : summary));
    await fails(report(fixture), "INPUT");
    expect(fixture.fetchImpl).not.toHaveBeenCalled();
  });

  it.each([ASSIGNMENTS_FILE, RESULTS_FILE, METADATA_FILE])("fails closed on missing artifact %s", async file => {
    const fixture = setup();
    fixture.files.delete(file);
    await fails(report(fixture), "INPUT");
    expect(fixture.fetchImpl).not.toHaveBeenCalled();
  });

  it("requires a configured results file and bounds local inputs", async () => {
    const fixture = setup();
    delete fixture.env.GH_AW_EXPERIMENT_RESULTS_FILE;
    await fails(report(fixture), "INPUT");
    fixture.env.GH_AW_EXPERIMENT_RESULTS_FILE = RESULTS_FILE;
    delete fixture.env.GH_AW_EXPERIMENT_METADATA_FILE;
    await fails(report(fixture), "INPUT");
    fixture.env.GH_AW_EXPERIMENT_METADATA_FILE = METADATA_FILE;
    fixture.fsImpl.statSync.mockReturnValue({ size: 1048577 });
    await fails(report(fixture), "INPUT");
    expect(fixture.fetchImpl).not.toHaveBeenCalled();
  });
});

describe.each([
  ["assign", assign],
  ["report", report],
])("experiments broker %s transport", (_operation, run) => {
  it.each([
    "",
    "http://broker.example.test",
    "ftp://broker.example.test",
    "******broker.example.test",
    "https://broker.example.test/#fragment",
    " https://broker.example.test",
    "https://broker.example.test\\path",
    "https://broker.example.test/\nsecret",
  ])("rejects invalid endpoints %# without network access", async endpoint => {
    const fixture = setup();
    fixture.env.GH_AW_EXPERIMENTS_BROKER_URL = endpoint;
    await fails(run(fixture), "CONFIGURATION");
    expect(fixture.fetchImpl).not.toHaveBeenCalled();
  });

  it.each(["GH_AW_EXPERIMENTS_BROKER_URL", "GH_AW_WORKFLOW_ID", "GH_AW_WORKFLOW_REF", "GH_AW_EXPERIMENT_INVOCATION_ID", "GITHUB_JOB", "GITHUB_RUN_ID", "GITHUB_RUN_ATTEMPT", "GITHUB_REPOSITORY", "GITHUB_SERVER_URL"])(
    "requires identity input %s",
    async key => {
      const fixture = setup();
      delete fixture.env[key];
      await fails(run(fixture), "CONFIGURATION");
      expect(fixture.fetchImpl).not.toHaveBeenCalled();
    }
  );

  it.each(["", "a,b", "../other", "x".repeat(129), "job\nother"])("rejects unsafe or unbounded invocation hints %#", async invocation => {
    const fixture = setup();
    fixture.env.GH_AW_EXPERIMENT_INVOCATION_ID = invocation;
    await fails(run(fixture), "CONFIGURATION");
    expect(fixture.fetchImpl).not.toHaveBeenCalled();
  });

  it.each([
    [401, "UNAUTHORIZED"],
    [403, "UNAUTHORIZED"],
    [409, "CONFLICT"],
    [400, "UNSUPPORTED"],
    [404, "UNSUPPORTED"],
    [422, "UNSUPPORTED"],
    [501, "UNSUPPORTED"],
    [302, "UNAVAILABLE"],
    [429, "UNAVAILABLE"],
    [500, "UNAVAILABLE"],
  ])("fails closed on status %s without retries or response diagnostics", async (status, kind) => {
    const fixture = setup();
    fixture.fetchImpl.mockImplementation(async (_url, init) => (init.method === "POST" ? new Response(`sensitive-detail ${OIDC}`, { status, headers: { location: "https://elsewhere.example.test" } }) : json({ value: OIDC })));
    await fails(run(fixture), kind);
    expect(fixture.fetchImpl).toHaveBeenCalledTimes(2);
    expect(fixture.fsImpl.writeFileSync).not.toHaveBeenCalled();
  });

  it("rejects unexpected successful redirects", async () => {
    const fixture = setup();
    fixture.fetchImpl.mockImplementation(async (_url, init) => {
      if (init.method !== "POST") return json({ value: OIDC });
      const response = _operation === "assign" ? json(DECISION) : new Response(null, { status: 204 });
      Object.defineProperty(response, "redirected", { value: true });
      return response;
    });
    await fails(run(fixture), "UNAVAILABLE");
    expect(fixture.fetchImpl).toHaveBeenCalledTimes(2);
  });

  it.each(["ACTIONS_ID_TOKEN_REQUEST_URL", "ACTIONS_ID_TOKEN_REQUEST_TOKEN"])("requires OIDC input %s", async key => {
    const fixture = setup();
    delete fixture.env[key];
    await fails(run(fixture), "OIDC");
    expect(fixture.fetchImpl).not.toHaveBeenCalled();
  });

  it.each([401, 403, 302, 500])("rejects OIDC status %s without contacting the broker", async status => {
    const fixture = setup({ fetchImpl: vi.fn(async () => new Response(`sensitive-detail ${OIDC}`, { status })) });
    await fails(run(fixture), "OIDC");
    expect(fixture.fetchImpl).toHaveBeenCalledTimes(1);
  });

  it("rejects OIDC redirects even with a 200 response", async () => {
    const response = json({ value: OIDC });
    Object.defineProperty(response, "redirected", { value: true });
    const fixture = setup({ fetchImpl: vi.fn(async () => response) });
    await fails(run(fixture), "OIDC");
    expect(fixture.fetchImpl).toHaveBeenCalledTimes(1);
  });

  it.each(["not-a-jwt", "a.e30.c", jwt({}), jwt({ exp: "99999999999" }), jwt({ exp: START / 1000 }), jwt({ exp: START / 1000 - 1 })])("masks unusable OIDC tokens before decoding %#", async token => {
    const fixture = setup({ fetchImpl: vi.fn(async () => json({ value: token })) });
    await fails(run(fixture), "OIDC");
    expect(fixture.core.setSecret).toHaveBeenCalledWith(token);
    expect(fixture.fetchImpl).toHaveBeenCalledTimes(1);
  });

  it("fails closed if masking fails and exposes no secret diagnostic", async () => {
    const fixture = setup();
    fixture.core.setSecret.mockImplementation(() => {
      throw new Error(`sensitive-detail ${OIDC}`);
    });
    await fails(run(fixture), "OIDC");
    expect(fixture.fetchImpl).toHaveBeenCalledTimes(1);
  });

  it.each(["oidc", "broker"])("sanitizes %s network failures", async phase => {
    const fixture = setup();
    fixture.fetchImpl.mockImplementation(async (_url, init) => {
      if (phase === "broker" && init.method !== "POST") return json({ value: OIDC });
      throw new Error(`sensitive-detail ${OIDC}`);
    });
    await fails(run(fixture), phase === "oidc" ? "OIDC" : "UNAVAILABLE");
    expect(fixture.fetchImpl).toHaveBeenCalledTimes(phase === "oidc" ? 1 : 2);
  });

  it("bounds the OIDC response to 64 KiB", async () => {
    const fixture = setup({ fetchImpl: vi.fn(async () => new Response(" ".repeat(65537))) });
    await fails(run(fixture), "OIDC");
    expect(fixture.fetchImpl).toHaveBeenCalledTimes(1);
  });

  it.each(["oidc-fetch", "oidc-body", "broker-fetch", "broker-body"])("aborts the complete operation after ten seconds during %s", async phase => {
    vi.useFakeTimers();
    const fixture = setup();
    const stalledBody = () => new ReadableStream({ start() {} });
    fixture.fetchImpl.mockImplementation(async (_url, init) => {
      const oidc = init.method !== "POST";
      if (phase === (oidc ? "oidc-fetch" : "broker-fetch")) return new Promise(() => {});
      if (phase === (oidc ? "oidc-body" : "broker-body")) return { status: oidc || _operation === "assign" ? 200 : 204, redirected: false, body: stalledBody() };
      return oidc ? json({ value: OIDC }) : _operation === "assign" ? json(DECISION) : new Response(null, { status: 204 });
    });
    const pending = fails(run(fixture), "UNAVAILABLE");
    await vi.advanceTimersByTimeAsync(10000);
    await pending;
    expect(fixture.fetchImpl.mock.calls.every(([, init]) => init.signal.aborted)).toBe(true);
    expect(fixture.fsImpl.writeFileSync).not.toHaveBeenCalled();
    expect(fixture.core.setOutput).not.toHaveBeenCalled();
  });
});

describe("experiments broker response body validation", () => {
  it.each([
    '{"assignments":{"feature":"candidate","feature":"control","style":"brief"},"counts":{"feature":{"control":7,"candidate":8},"style":{"brief":9,"full":6}}}',
    '{"assignments":{"feature":"candidate","\\u0066eature":"control","style":"brief"},"counts":{"feature":{"control":7,"candidate":8},"style":{"brief":9,"full":6}}}',
    '{"assignments":{"feature":"candidate","style":"brief"},"counts":{"feature":{"control":7,"candidate":8,"\\u0063andidate":9},"style":{"brief":9,"full":6}}}',
    '{"assignments":{"feature":"candidate","style":"brief"},"assignments":{"feature":"candidate","style":"brief"},"counts":{"feature":{"control":7,"candidate":8},"style":{"brief":9,"full":6}}}',
  ])("rejects duplicate and escaped-equivalent response keys %#", async raw => {
    const fixture = setup();
    fixture.fetchImpl.mockImplementation(async (_url, init) => (init.method === "POST" ? new Response(raw) : json({ value: OIDC })));
    await fails(assign(fixture), "MALFORMED_RESPONSE");
    expect(fixture.fsImpl.writeFileSync).not.toHaveBeenCalled();
  });

  it.each(["{", " ".repeat(65537)])("rejects malformed or oversized assignment responses %#", async raw => {
    const fixture = setup();
    fixture.fetchImpl.mockImplementation(async (_url, init) => (init.method === "POST" ? new Response(raw) : json({ value: OIDC })));
    await fails(assign(fixture), "MALFORMED_RESPONSE");
  });

  it("rejects invalid UTF-8 rather than persisting replacement characters", async () => {
    const fixture = setup();
    fixture.fetchImpl.mockImplementation(async (_url, init) => (init.method === "POST" ? new Response(new Uint8Array([0xff])) : json({ value: OIDC })));
    await fails(assign(fixture), "MALFORMED_RESPONSE");
  });

  it("requires report 204 and rejects a 200 response even when its JSON looks valid", async () => {
    const fixture = setup();
    fixture.fetchImpl.mockImplementation(async (_url, init) => (init.method === "POST" ? json({}) : json({ value: OIDC })));
    await fails(report(fixture), "UNAVAILABLE");
  });

  it("rejects report responses with a body", async () => {
    const fixture = setup();
    fixture.fetchImpl.mockImplementation(async (_url, init) => (init.method === "POST" ? { status: 204, redirected: false, body: new Response("sensitive-detail").body } : json({ value: OIDC })));
    await fails(report(fixture), "MALFORMED_RESPONSE");
  });
});
