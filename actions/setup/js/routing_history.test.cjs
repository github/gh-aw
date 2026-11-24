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
