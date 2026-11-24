import { describe, expect, it } from "vitest";

const req = require;
const vectors = req("./fixtures/routing-history/aw2-reducer.json");
const { applyRun, emptyState, trimRuns } = req("./routing_history.cjs");

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
