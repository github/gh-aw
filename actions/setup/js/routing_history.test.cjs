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

  it.each([
    ["serve", CHALLENGER],
    ["ordinary", INCUMBENT],
  ])("updates last_role for bound %s receipt without switching incumbent or counting", (role, incumbentAfter) => {
    const { state, runs } = baseLiveState();
    state.scopes[SCOPE].last_role = "test";

    const result = applyRun(state, runs, {
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
        role,
        selected: CHALLENGER,
        incumbent: { before: INCUMBENT, after: incumbentAfter },
      },
      prepare: { explore_allowed: true },
    });
    expect(result.state.scopes[SCOPE].last_role).toBe(role);
    expect(result.state.scopes[SCOPE].incumbent).toBe(INCUMBENT);
    expect(result.state.scopes[SCOPE].pairs[CHALLENGER]).toBeUndefined();
    expect(result.state.unexplained).toBe(0);
    expect(result.lines).toMatchObject([{ note: "no_agent", role, pair: CHALLENGER, outcome: null, counted: false }]);
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

  it.each([
    ["missing incumbent", undefined],
    ["null incumbent", null],
    ["malformed incumbent fields", { before: 123, after: INCUMBENT }],
  ])("treats live receipt with %s as unbound on live no_agent", (_label, badIncumbent) => {
    const { state, runs } = baseLiveState();
    const receipt = {
      workflow: "pr-sous-chef",
      run: { id: "45000000005", attempt: 1 },
      generation: GENERATION,
      mode: "live",
      scope: SCOPE,
      role: "serve",
      selected: CHALLENGER,
      ...(badIncumbent !== undefined ? { incumbent: badIncumbent } : {}),
    };

    const result = applyRun(state, runs, {
      workflow: "pr-sous-chef",
      run: { id: "45000000005", number: 5, attempt: 1 },
      at: "2026-11-05T00:00:00Z",
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
    expect(result.lines).toMatchObject([{ note: "no_agent", role: null, pair: CHALLENGER, counted: false }]);
  });
});

describe("sibling-effort suspension settlement", () => {
  const scopeKey = "copilot/cost/shared";
  const gen = "9f2c41d0";
  const makeRun = (number, at, pair, outcome, role = "probe") => ({
    workflow: "pr-sous-chef",
    run: { id: String(45_000_000_000 + number), number, attempt: 1 },
    at,
    generation: gen,
    mode: "live",
    scope: scopeKey,
    observation: { outcome },
    pair,
    cost: { credits: 10.0, model_credits: 10.0, tokens: null },
    receipt: {
      workflow: "pr-sous-chef",
      run: { id: String(45_000_000_000 + number), attempt: 1 },
      generation: gen,
      mode: "live",
      scope: scopeKey,
      role,
      selected: pair,
      incumbent: { before: "github-copilot/model-b@high", after: "github-copilot/model-b@high" },
    },
    prepare: { explore_allowed: true },
  });

  const makeSuspendedPair = (history, sinceSeq, pending, seen) => ({
    history,
    credits: 10.0,
    model_credits: 10.0,
    bills: history.length,
    streak: pending,
    tests: history.length,
    suspended: { since_seq: sinceSeq, pending },
    seen,
    summary: null,
  });

  it("preserves prior successes on restore and drops them on confirmed break, emitting settlement events", () => {
    const baseState = {
      ...emptyState("pr-sous-chef"),
      seq: 6,
      generation: { hash: gen, since_run: 1 },
      last_run: 6,
      scopes: {
        [scopeKey]: {
          incumbent: "github-copilot/model-b@high",
          last_role: "serve",
          last_success_seq: 6,
          pairs: {
            "github-copilot/model-a@low": makeSuspendedPair(
              [
                [1, "2026-11-01T00:00:00Z", true, 10.0, 10.0, null],
                [2, "2026-11-02T00:00:00Z", true, 10.0, 10.0, null],
                [3, "2026-11-03T00:00:00Z", false, 10.0, 10.0, null],
                [4, "2026-11-04T00:00:00Z", false, 10.0, 10.0, null],
                [5, "2026-11-05T00:00:00Z", false, 10.0, 10.0, null],
              ],
              5,
              3,
              "2026-11-05T00:00:00Z"
            ),
          },
        },
      },
    };

    const okEvents = [];
    const okResult = applyRun(baseState, [], makeRun(7, "2026-11-07T00:00:00Z", "github-copilot/model-a@high", "success"), { events: okEvents });
    const okScope = okResult.state.scopes[scopeKey];
    expect(okEvents).toEqual([["github-copilot/model-a@high", "back"]]);
    expect(okScope.last_success_seq).toBe(7);
    expect(okScope.pairs["github-copilot/model-a@low"].suspended).toBeNull();
    expect(okScope.pairs["github-copilot/model-a@low"].streak).toBe(0);
    expect(okScope.pairs["github-copilot/model-a@low"].history.map(e => e[0])).toEqual([1, 2]);
    expect(okScope.pairs["github-copilot/model-a@high"].history.map(e => [e[0], e[2]])).toEqual([[7, true]]);

    const failEvents = [];
    const failResult = applyRun(baseState, [], makeRun(7, "2026-11-07T00:00:00Z", "github-copilot/model-a@high", "failure"), { events: failEvents });
    const failScope = failResult.state.scopes[scopeKey];
    expect(failEvents).toEqual([["github-copilot/model-a@high", "broken"]]);
    expect(failScope.last_success_seq).toBe(6);
    expect(failScope.pairs["github-copilot/model-a@low"].suspended).toBeNull();
    expect(failScope.pairs["github-copilot/model-a@low"].streak).toBe(0);
    expect(failScope.pairs["github-copilot/model-a@low"].history.map(e => e[0])).toEqual([3, 4, 5]);
    expect(failScope.pairs["github-copilot/model-a@high"].history.map(e => [e[0], e[2]])).toEqual([[7, false]]);
    expect(failScope.pairs["github-copilot/model-a@high"].streak).toBe(0);
  });

  it("settles only due same-base siblings on failure and never touches other providers or model prefixes", () => {
    const state = {
      ...emptyState("pr-sous-chef"),
      seq: 10,
      generation: { hash: gen, since_run: 1 },
      last_run: 10,
      scopes: {
        [scopeKey]: {
          incumbent: "github-copilot/model-b@high",
          last_role: "serve",
          last_success_seq: 6,
          pairs: {
            "github-copilot/model-a@low": makeSuspendedPair([[3, "2026-11-03T00:00:00Z", false, 10.0, 10.0, null]], 5, 3, "2026-11-03T00:00:00Z"),
            "github-copilot/model-a@medium": makeSuspendedPair([[8, "2026-11-04T00:00:00Z", false, 10.0, 10.0, null]], 8, 1, "2026-11-04T00:00:00Z"),
            "openai/model-a@low": makeSuspendedPair([[4, "2026-11-03T00:00:00Z", false, 10.0, 10.0, null]], 4, 1, "2026-11-03T00:00:00Z"),
            "github-copilot/model-a-mini@low": makeSuspendedPair([[5, "2026-11-03T00:00:00Z", false, 10.0, 10.0, null]], 5, 1, "2026-11-03T00:00:00Z"),
          },
        },
      },
    };

    const failResult = applyRun(state, [], makeRun(11, "2026-11-08T00:00:00Z", "github-copilot/model-a@high", "failure"));
    const failPairs = failResult.state.scopes[scopeKey].pairs;
    expect(failPairs["github-copilot/model-a@low"].suspended).toBeNull();
    expect(failPairs["github-copilot/model-a@medium"].suspended).toEqual({ since_seq: 8, pending: 1 });
    expect(failPairs["openai/model-a@low"].suspended).toEqual({ since_seq: 4, pending: 1 });
    expect(failPairs["github-copilot/model-a-mini@low"].suspended).toEqual({ since_seq: 5, pending: 1 });

    const okResult = applyRun(state, [], makeRun(11, "2026-11-08T00:00:00Z", "github-copilot/model-a@high", "success"));
    const okPairs = okResult.state.scopes[scopeKey].pairs;
    expect(okPairs["github-copilot/model-a@low"].suspended).toBeNull();
    expect(okPairs["github-copilot/model-a@medium"].suspended).toBeNull();
    expect(okPairs["openai/model-a@low"].suspended).toEqual({ since_seq: 4, pending: 1 });
    expect(okPairs["github-copilot/model-a-mini@low"].suspended).toEqual({ since_seq: 5, pending: 1 });
  });
});

describe("verified reconciliation coverage", () => {
  const GEN1 = "11111111";
  const GEN2 = "22222222";
  const PAIR = "github-copilot/model-a@high";
  const makeRun = (number, overrides = {}) => ({
    workflow: "triage",
    run: { id: String(9000 + number), number, attempt: 1 },
    at: `2026-11-24T10:${String(number % 60).padStart(2, "0")}:00Z`,
    generation: GEN1,
    mode: "observe",
    scope: "copilot/cost/shared",
    observation: { outcome: "success" },
    pair: PAIR,
    cost: { credits: 2, model_credits: 2, tokens: [100, 0, 0, 50, null] },
    receipt: null,
    prepare: null,
    listing: [],
    reconciliation: { verified: true, unresolved: [], resolved: [] },
    ...overrides,
  });

  it("does not re-charge an evicted run already at or below reconciled_through", () => {
    const state = {
      ...emptyState("triage"),
      generation: { hash: GEN1, since_run: 1 },
      last_run: 555,
      reconciled_through: 555,
      unresolved_runs: [],
      unexplained: 1,
    };
    // runs.jsonl retains only #56..#555; #50 was already charged at #51 and aged out.
    const runs = [
      { run: "9555", number: 555, attempt: 1, at: "2026-11-24T09:00:00Z", generation: GEN1, scope: "copilot/cost/shared", mode: "observe", role: "observe", pair: PAIR, outcome: "success", credits: 2, counted: true, note: null },
    ];
    const result = applyRun(
      state,
      runs,
      makeRun(556, {
        listing: [{ id: "9050", number: 50, conclusion: "failure", agent_ran: true }],
      })
    );
    expect(result.lines.map(line => ({ number: line.number, note: line.note }))).toEqual([{ number: 556, note: null }]);
    expect(result.state.unexplained).toBe(1);
    expect(result.state.reconciled_through).toBe(556);
    expect(result.state.unresolved_runs).toEqual([]);
  });

  it("reconciles a failed first run of a new generation instead of discarding it at since_run", () => {
    const state = {
      ...emptyState("triage"),
      generation: { hash: GEN1, since_run: 1 },
      last_run: 99,
      reconciled_through: 99,
      unresolved_runs: [],
      unexplained: 2,
    };
    const result = applyRun(
      state,
      [],
      makeRun(101, {
        generation: GEN2,
        listing: [{ id: "9100", number: 100, conclusion: "failure", agent_ran: true }],
      })
    );
    expect(result.state.generation).toEqual({ hash: GEN2, since_run: 101 });
    expect(result.lines.map(line => ({ number: line.number, note: line.note }))).toEqual([
      { number: 100, note: "unsaved" },
      { number: 101, note: null },
    ]);
    // GEN1's unexplained count clears on edit, then GEN2's failed run #100 charges 1.
    expect(result.state.unexplained).toBe(1);
    expect(result.state.reconciled_through).toBe(101);
    expect(result.state.unresolved_runs).toEqual([]);
  });

  it("retains unresolved active runs across a generation transition until resolved or self-saved", () => {
    let state = {
      ...emptyState("triage"),
      generation: { hash: GEN1, since_run: 1 },
      last_run: 99,
      reconciled_through: 99,
      unresolved_runs: [],
    };
    let runs = [];

    // #102 saves while #100 and #101 are still in progress.
    let step = applyRun(state, runs, makeRun(102, { reconciliation: { verified: true, unresolved: [100, 101], resolved: [] } }));
    state = step.state;
    runs = trimRuns([...runs, ...step.lines]);
    expect(state.reconciled_through).toBe(102);
    expect(state.unresolved_runs).toEqual([100, 101]);

    // #103 starts GEN2 while #100 and #101 are still unresolved.
    step = applyRun(state, runs, makeRun(103, { generation: GEN2, reconciliation: { verified: true, unresolved: [100, 101], resolved: [] } }));
    state = step.state;
    runs = trimRuns([...runs, ...step.lines]);
    expect(state.reconciled_through).toBe(103);
    expect(state.unresolved_runs).toEqual([100, 101]);

    // #100 (GEN1) finishes late as older_definition: removes itself from unresolved_runs without advancing reconciled_through.
    step = applyRun(state, runs, makeRun(100, { generation: GEN1, reconciliation: { verified: true, unresolved: [], resolved: [] } }));
    state = step.state;
    runs = trimRuns([...runs, ...step.lines]);
    expect(step.lines.map(line => ({ number: line.number, note: line.note }))).toEqual([{ number: 100, note: "older_definition" }]);
    expect(state.reconciled_through).toBe(103);
    expect(state.unresolved_runs).toEqual([101]);

    // #104 (GEN2) sees #101 completed with a failed save: charges #101 even though 101 <= 103.
    step = applyRun(
      state,
      runs,
      makeRun(104, {
        generation: GEN2,
        listing: [{ id: "9101", number: 101, conclusion: "failure", agent_ran: true }],
        reconciliation: { verified: true, unresolved: [], resolved: [101] },
      })
    );
    expect(step.lines.map(line => ({ number: line.number, note: line.note }))).toEqual([
      { number: 101, note: "unsaved" },
      { number: 104, note: null },
    ]);
    expect(step.state.unexplained).toBe(1);
    expect(step.state.reconciled_through).toBe(104);
    expect(step.state.unresolved_runs).toEqual([]);
  });

  it("does not advance reconciled_through on unverified listings and bootstraps legacy state from its fence rather than last_run", () => {
    const legacy = {
      ...emptyState("triage"),
      generation: { hash: GEN1, since_run: 1 },
      last_run: 100,
    };
    // Legacy state lacks reconciled_through: bootstrap from since_run (1), not last_run (100), so unverified #99 is still reconciled.
    const unverified = applyRun(legacy, [], makeRun(101, { reconciliation: { verified: false } }));
    expect(unverified.state.last_run).toBe(101);
    expect(unverified.state.reconciled_through).toBe(1);
    expect(unverified.state.unresolved_runs).toEqual([]);

    const upgraded = applyRun(
      legacy,
      [],
      makeRun(101, {
        listing: [{ id: "9099", number: 99, conclusion: "failure", agent_ran: true }],
        reconciliation: { verified: true, unresolved: [], resolved: [] },
      })
    );
    expect(upgraded.lines.map(line => ({ number: line.number, note: line.note }))).toEqual([
      { number: 99, note: "unsaved" },
      { number: 101, note: null },
    ]);
    expect(upgraded.state.reconciled_through).toBe(101);
    expect(upgraded.state.unexplained).toBe(1);

    // When a previously unresolved run saves itself during an unverified listing, it still clears itself.
    const withUnresolved = { ...unverified.state, reconciled_through: 60, unresolved_runs: [55, 58] };
    const second = applyRun(withUnresolved, [], makeRun(58, { reconciliation: { verified: false } }));
    expect(second.state.reconciled_through).toBe(60);
    expect(second.state.unresolved_runs).toEqual([55]);
  });

  it("clamps reconciled_through before charging so runs above the final ceiling are deferred, and preserves runs at the ceiling", () => {
    const state = {
      ...emptyState("triage"),
      generation: { hash: GEN1, since_run: 1 },
      last_run: 100,
      reconciled_through: 100,
      unresolved_runs: [95],
    };
    // Consecutive active runs #101 and #102 at the cap: clamping to cutoff - 1 = 101 keeps #101 in unresolved_runs
    // and defers failed run #103 (above the final clamped ceiling 101) so eviction cannot double-charge it.
    const result = applyRun(
      state,
      [],
      makeRun(105, {
        listing: [{ id: "9103", number: 103, conclusion: "failure", agent_ran: true }],
        reconciliation: { verified: true, unresolved: [101, 102], resolved: [] },
      }),
      { bounds: { unresolved_runs: 2 } }
    );
    expect(result.state.reconciled_through).toBe(101);
    expect(result.state.unresolved_runs).toEqual([95, 101]);
    expect(result.lines.map(line => ({ number: line.number, note: line.note }))).toEqual([{ number: 105, note: null }]);
    expect(result.state.unexplained).toBe(0);

    // A later run charges #101 (at the ceiling), #102, and #103 once the ceiling advances past them.
    const next = applyRun(
      result.state,
      result.lines,
      makeRun(106, {
        listing: [
          { id: "9101", number: 101, conclusion: "failure", agent_ran: true },
          { id: "9102", number: 102, conclusion: "failure", agent_ran: true },
          { id: "9103", number: 103, conclusion: "failure", agent_ran: true },
        ],
        reconciliation: { verified: true, unresolved: [], resolved: [95, 101] },
      }),
      { bounds: { unresolved_runs: 2 } }
    );
    expect(next.lines.map(line => ({ number: line.number, note: line.note }))).toEqual([
      { number: 101, note: "unsaved" },
      { number: 102, note: "unsaved" },
      { number: 103, note: "unsaved" },
      { number: 106, note: null },
    ]);
    expect(next.state.reconciled_through).toBe(106);
    expect(next.state.unresolved_runs).toEqual([]);
  });

  it("records a confirmed older-definition failed save as older_definition without incrementing unexplained", () => {
    const state = {
      ...emptyState("triage"),
      generation: { hash: GEN2, since_run: 101 },
      last_run: 101,
      reconciled_through: 101,
      unresolved_runs: [99],
      unexplained: 0,
    };
    const result = applyRun(
      state,
      [],
      makeRun(102, {
        generation: GEN2,
        listing: [{ id: "9099", number: 99, conclusion: "failure", agent_ran: true, generation: GEN1 }],
        reconciliation: { verified: true, unresolved: [], resolved: [99] },
      })
    );
    expect(result.lines.map(line => ({ number: line.number, note: line.note, generation: line.generation }))).toEqual([
      { number: 99, note: "older_definition", generation: GEN1 },
      { number: 102, note: null, generation: GEN2 },
    ]);
    expect(result.state.unexplained).toBe(0);
    expect(result.state.reconciled_through).toBe(102);
    expect(result.state.unresolved_runs).toEqual([]);
  });

  it("defers runs above reconciled_through on unverified scans and never lets an older writer consume a newer run", () => {
    const state = {
      ...emptyState("triage"),
      generation: { hash: GEN1, since_run: 1 },
      last_run: 98,
      reconciled_through: 98,
      unresolved_runs: [95],
    };
    // Unverified scan at #160: resolves #95 (in unresolved_runs), defers #100 (above reconciled_through).
    const unverified = applyRun(
      state,
      [],
      makeRun(160, {
        listing: [
          { id: "9095", number: 95, conclusion: "failure", agent_ran: true },
          { id: "9100", number: 100, conclusion: "failure", agent_ran: true },
        ],
        reconciliation: { verified: false, unresolved: [], resolved: [95] },
      })
    );
    expect(unverified.lines.map(line => ({ number: line.number, note: line.note }))).toEqual([
      { number: 95, note: "unsaved" },
      { number: 160, note: null },
    ]);
    expect(unverified.state.reconciled_through).toBe(98);
    expect(unverified.state.unresolved_runs).toEqual([]);

    // Older writer #99 (GEN1) ignores newer failed run #100 (GEN2), leaving #100 for GEN2's #101.
    const olderWriter = applyRun(
      { ...state, unresolved_runs: [] },
      [],
      makeRun(99, {
        generation: GEN1,
        listing: [{ id: "9100", number: 100, conclusion: "failure", agent_ran: true }],
        reconciliation: { verified: true, unresolved: [], resolved: [] },
      })
    );
    expect(olderWriter.lines.map(line => ({ number: line.number, note: line.note }))).toEqual([{ number: 99, note: null }]);
    expect(olderWriter.state.unexplained).toBe(0);
    expect(olderWriter.state.reconciled_through).toBe(99);
  });
});
