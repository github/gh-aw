import { describe, expect, it } from "vitest";

const req = require;
const vectors = req("./fixtures/routing-history/aw2-reducer.json");
const { applyRun, averages, emptyState, trimRuns } = req("./routing_history.cjs");

// Numbers agree to a relative tolerance of 1e-9 (vectors.arithmetic); everything else exactly.
function expectSame(actual, expected, path = "state") {
  if (typeof expected === "number" && typeof actual === "number") {
    const scale = Math.max(Math.abs(expected), Math.abs(actual));
    if (Math.abs(actual - expected) > Math.max(1e-12, 1e-9 * scale)) throw new Error(`${path}: expected ${expected}, received ${actual}`);
    return;
  }
  if (expected === null || typeof expected !== "object") {
    if (actual !== expected) throw new Error(`${path}: expected ${JSON.stringify(expected)}, received ${JSON.stringify(actual)}`);
    return;
  }
  expect(Array.isArray(actual), path).toBe(Array.isArray(expected));
  expect(Object.keys(actual ?? {}).sort(), path).toEqual(Object.keys(expected).sort());
  for (const key of Object.keys(expected)) expectSame(actual[key], expected[key], `${path}.${key}`);
}

// Per-step expectations reduce each pair's history to its length and newest entry.
function project(state) {
  const projected = structuredClone(state);
  for (const scope of Object.values(projected.scopes)) {
    for (const pair of Object.values(scope.pairs)) {
      pair.history = { length: pair.history.length, newest: pair.history.at(-1) ?? null };
    }
  }
  return projected;
}

describe("routing history oracle vectors", () => {
  it.each(vectors.timelines.map(timeline => [timeline.name, timeline]))("replays the %s timeline", (_name, timeline) => {
    let state = timeline.initial.state;
    let runs = [];
    timeline.steps.forEach((step, index) => {
      const result = applyRun(state, runs, step.run);
      expectSame(result.lines, step.lines, `${timeline.name}[${index}].lines`);
      expectSame(project(result.state), step.state, `${timeline.name}[${index}].state`);
      state = result.state;
      runs = trimRuns([...runs, ...result.lines]);
    });
    expectSame(state, timeline.final.state, `${timeline.name}.final`);
  });

  it.each(vectors.cases.filter(vector => vector.run).map(vector => [vector.name, vector]))("%s", (_name, vector) => {
    const before = structuredClone(vector.state);
    const result = applyRun(vector.state, vector.runs, vector.run, { bounds: vector.bounds });
    expectSame(result.state, vector.expect.state);
    expectSame(result.lines, vector.expect.lines, "lines");
    if (vector.expect.runs) expectSame(trimRuns([...vector.runs, ...result.lines], vector.bounds), vector.expect.runs, "runs");
    expect(vector.state).toEqual(before);
  });
});

describe("routing history state", () => {
  it("starts empty, as a missing branch reads", () => {
    expect(emptyState("pr-sous-chef")).toEqual({
      v: 2,
      workflow: "pr-sous-chef",
      seq: 0,
      generation: null,
      reset_after_run: 0,
      last_run: 0,
      unexplained: 0,
      scopes: {},
      updated: null,
    });
  });

  it("ignores standalone model_credits on legacy entries whose total credits are null", () => {
    const partial = [1, "2026-11-01T00:00:00Z", true, null, 30.0, [40000, 300000, 0, 9000, null]];
    const full = [2, "2026-11-02T00:00:00Z", true, 10.0, 10.0, null];
    expect(averages([partial])).toEqual({ credits: null, modelCredits: null, bills: 0 });
    expect(averages([partial, full])).toEqual({ credits: 10.0, modelCredits: 10.0, bills: 1 });
  });
});

describe("live no_agent exploration accounting", () => {
  const GENERATION = "9f2c41d0";
  const SCOPE = "copilot/cost/shared";
  const INCUMBENT = "github-copilot/model-b@high";
  const CHALLENGER = "github-copilot/model-a@high";

  function baseLiveState() {
    const first = applyRun(emptyState("pr-sous-chef"), [], {
      workflow: "pr-sous-chef",
      run: { id: "45000000001", number: 1, attempt: 1 },
      at: "2026-11-01T00:00:00Z",
      generation: GENERATION,
      mode: "live",
      scope: SCOPE,
      observation: { outcome: "success" },
      pair: INCUMBENT,
      cost: { credits: 10, model_credits: 10, tokens: null },
      receipt: {
        workflow: "pr-sous-chef",
        run: { id: "45000000001", attempt: 1 },
        generation: GENERATION,
        mode: "live",
        scope: SCOPE,
        role: "serve",
        selected: INCUMBENT,
        incumbent: { before: null, after: INCUMBENT },
      },
      prepare: { explore_allowed: true },
    });
    return { state: first.state, runs: first.lines };
  }

  it("updates last_role for bound serve and ordinary receipts without switching incumbent or counting", () => {
    const { state, runs } = baseLiveState();
    state.scopes[SCOPE].last_role = "test";

    const serveResult = applyRun(state, runs, {
      workflow: "pr-sous-chef",
      run: { id: "45000000002", number: 2, attempt: 1 },
      at: "2026-11-02T00:00:00Z",
      generation: GENERATION,
      mode: "live",
      scope: SCOPE,
      observation: null,
      pair: CHALLENGER,
      cost: null,
      receipt: {
        workflow: "pr-sous-chef",
        run: { id: "45000000002", attempt: 1 },
        generation: GENERATION,
        mode: "live",
        scope: SCOPE,
        role: "serve",
        selected: CHALLENGER,
        incumbent: { before: INCUMBENT, after: CHALLENGER },
      },
      prepare: { explore_allowed: true },
    });
    expect(serveResult.state.scopes[SCOPE].last_role).toBe("serve");
    expect(serveResult.state.scopes[SCOPE].incumbent).toBe(INCUMBENT);
    expect(serveResult.state.scopes[SCOPE].pairs[CHALLENGER]).toBeUndefined();
    expect(serveResult.state.unexplained).toBe(0);
    expect(serveResult.lines).toMatchObject([{ note: "no_agent", role: "serve", pair: CHALLENGER, outcome: null, counted: false }]);

    const ordinaryResult = applyRun(state, runs, {
      workflow: "pr-sous-chef",
      run: { id: "45000000003", number: 3, attempt: 1 },
      at: "2026-11-03T00:00:00Z",
      generation: GENERATION,
      mode: "live",
      scope: SCOPE,
      observation: null,
      pair: CHALLENGER,
      cost: null,
      receipt: {
        workflow: "pr-sous-chef",
        run: { id: "45000000003", attempt: 1 },
        generation: GENERATION,
        mode: "live",
        scope: SCOPE,
        role: "ordinary",
        selected: CHALLENGER,
        incumbent: { before: INCUMBENT, after: INCUMBENT },
      },
      prepare: { explore_allowed: true },
    });
    expect(ordinaryResult.state.scopes[SCOPE].last_role).toBe("ordinary");
    expect(ordinaryResult.state.scopes[SCOPE].incumbent).toBe(INCUMBENT);
    expect(ordinaryResult.state.scopes[SCOPE].pairs[CHALLENGER]).toBeUndefined();
    expect(ordinaryResult.state.unexplained).toBe(0);
    expect(ordinaryResult.lines).toMatchObject([{ note: "no_agent", role: "ordinary", pair: CHALLENGER, outcome: null, counted: false }]);
  });

  it("respects the scope bound on live no_agent while leaving scopes untouched in observe mode", () => {
    const { state, runs } = baseLiveState();
    const secondScope = "copilot/cost/balanced";
    const thirdScope = "copilot/cost/robust";
    const step2 = applyRun(state, runs, {
      workflow: "pr-sous-chef",
      run: { id: "45000000002", number: 2, attempt: 1 },
      at: "2026-11-02T00:00:00Z",
      generation: GENERATION,
      mode: "live",
      scope: secondScope,
      observation: { outcome: "success" },
      pair: INCUMBENT,
      cost: { credits: 10, model_credits: 10, tokens: null },
      receipt: {
        workflow: "pr-sous-chef",
        run: { id: "45000000002", attempt: 1 },
        generation: GENERATION,
        mode: "live",
        scope: secondScope,
        role: "serve",
        selected: INCUMBENT,
        incumbent: { before: null, after: INCUMBENT },
      },
      prepare: { explore_allowed: true },
    });
    const combinedRuns = [...runs, ...step2.lines];

    const observeResult = applyRun(
      step2.state,
      combinedRuns,
      {
        workflow: "pr-sous-chef",
        run: { id: "45000000003", number: 3, attempt: 1 },
        at: "2026-11-03T00:00:00Z",
        generation: GENERATION,
        mode: "observe",
        scope: thirdScope,
        observation: null,
        pair: null,
        cost: null,
        receipt: null,
        prepare: null,
      },
      { bounds: { scopes: 2 } }
    );
    expect(Object.keys(observeResult.state.scopes).sort()).toEqual([secondScope, SCOPE]);

    const liveResult = applyRun(
      step2.state,
      combinedRuns,
      {
        workflow: "pr-sous-chef",
        run: { id: "45000000003", number: 3, attempt: 1 },
        at: "2026-11-03T00:00:00Z",
        generation: GENERATION,
        mode: "live",
        scope: thirdScope,
        observation: null,
        pair: CHALLENGER,
        cost: null,
        receipt: {
          workflow: "pr-sous-chef",
          run: { id: "45000000003", attempt: 1 },
          generation: GENERATION,
          mode: "live",
          scope: thirdScope,
          role: "test",
          selected: CHALLENGER,
          incumbent: { before: null, after: null },
        },
        prepare: { explore_allowed: true },
      },
      { bounds: { scopes: 2 } }
    );
    expect(Object.keys(liveResult.state.scopes).sort()).toEqual([secondScope, thirdScope]);
    expect(liveResult.state.scopes[thirdScope].last_role).toBe("test");
    expect(liveResult.state.scopes[thirdScope].pairs[CHALLENGER].tests).toBe(1);
    expect(liveResult.state.scopes[thirdScope].pairs[CHALLENGER].seen).toBe("2026-11-03T00:00:00Z");
  });

  it("treats mismatched or null-selected receipts as unbound on live no_agent", () => {
    const { state, runs } = baseLiveState();

    const mismatched = applyRun(state, runs, {
      workflow: "pr-sous-chef",
      run: { id: "45000000002", number: 2, attempt: 1 },
      at: "2026-11-02T00:00:00Z",
      generation: GENERATION,
      mode: "live",
      scope: SCOPE,
      observation: null,
      pair: CHALLENGER,
      cost: null,
      receipt: {
        workflow: "pr-sous-chef",
        run: { id: "45000000002", attempt: 1 },
        generation: GENERATION,
        mode: "live",
        scope: SCOPE,
        role: "serve",
        selected: INCUMBENT,
        incumbent: { before: INCUMBENT, after: INCUMBENT },
      },
      prepare: { explore_allowed: true },
    });
    expect(mismatched.state.unexplained).toBe(1);
    expect(mismatched.state.scopes[SCOPE].last_role).toBe("test");
    expect(mismatched.lines).toMatchObject([{ note: "no_agent", role: null, pair: CHALLENGER, counted: false }]);

    const nullSelected = applyRun(state, runs, {
      workflow: "pr-sous-chef",
      run: { id: "45000000003", number: 3, attempt: 1 },
      at: "2026-11-03T00:00:00Z",
      generation: GENERATION,
      mode: "live",
      scope: SCOPE,
      observation: null,
      pair: null,
      cost: null,
      receipt: {
        workflow: "pr-sous-chef",
        run: { id: "45000000003", attempt: 1 },
        generation: GENERATION,
        mode: "live",
        scope: SCOPE,
        role: "serve",
        selected: null,
        incumbent: { before: INCUMBENT, after: INCUMBENT },
      },
      prepare: { explore_allowed: true },
    });
    expect(nullSelected.state.unexplained).toBe(1);
    expect(nullSelected.state.scopes[SCOPE].last_role).toBe("test");
    expect(nullSelected.lines).toMatchObject([{ note: "no_agent", role: null, pair: null, counted: false }]);
  });

  it.each([
    ["unknown string", "invalid"],
    ["observe role in live mode", "observe"],
    ["missing role", undefined],
    ["numeric role", 123],
  ])("treats malformed live receipt role (%s) as unbound on live no_agent", (_label, badRole) => {
    const { state, runs } = baseLiveState();
    const receipt = {
      workflow: "pr-sous-chef",
      run: { id: "45000000004", attempt: 1 },
      generation: GENERATION,
      mode: "live",
      scope: SCOPE,
      selected: CHALLENGER,
      incumbent: { before: INCUMBENT, after: INCUMBENT },
      ...(badRole !== undefined ? { role: badRole } : {}),
    };

    const result = applyRun(state, runs, {
      workflow: "pr-sous-chef",
      run: { id: "45000000004", number: 4, attempt: 1 },
      at: "2026-11-04T00:00:00Z",
      generation: GENERATION,
      mode: "live",
      scope: SCOPE,
      observation: null,
      pair: CHALLENGER,
      cost: null,
      receipt,
      prepare: { explore_allowed: true },
    });

    expect(result.state.unexplained).toBe(1);
    expect(result.state.scopes[SCOPE].last_role).toBe("test");
    expect(result.state.scopes[SCOPE].pairs[CHALLENGER]).toBeUndefined();
    expect(result.lines).toHaveLength(1);
    const serialized = JSON.parse(JSON.stringify(result.lines[0]));
    expect(Object.hasOwn(serialized, "role")).toBe(true);
    expect(serialized).toMatchObject({ note: "no_agent", role: null, pair: CHALLENGER, counted: false });
  });
});
