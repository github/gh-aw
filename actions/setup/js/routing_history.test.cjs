import { describe, expect, it } from "vitest";

const req = require;
const vectors = req("./fixtures/routing-history/aw2-reducer.json");
const { applyRun, emptyState, resetState, trimRuns } = req("./routing_history.cjs");

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

  it("resets as gh aw routing reset does", () => {
    const vector = vectors.cases.find(candidate => candidate.name === "reset");
    expectSame(resetState(vector.state, vector.reset.latest_run, vector.reset.at), vector.expect.state);
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
