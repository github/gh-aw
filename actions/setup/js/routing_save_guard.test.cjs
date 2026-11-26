import { describe, expect, it } from "vitest";

const req = require;
const { checkRoutingSaveCoverage } = req("./routing_save_guard.cjs");

const run = (run_number, overrides = {}) => ({ run_number, event: "push", head_branch: "main", status: "completed", conclusion: "success", ...overrides });
const running = { status: "in_progress", conclusion: null };
const failure = { conclusion: "failure" };
const through = (n, unresolved_runs = []) => ({ last_run: n, reconciled_through: n, reset_after_run: 0, unresolved_runs });
const check = (state, runs, extra = {}) => checkRoutingSaveCoverage({ state, runNumber: 11, defaultBranch: "main", runs, reachesStart: false, ...extra });
const clear = { hasUnsaved: false, listingIncomplete: false };

describe("live routing save coverage", () => {
  const delayed = [run(12, failure), run(11, running), run(10)];

  it("checks a higher-numbered run that finished before this decision, until it is reconciled", () => {
    expect(check(through(10), delayed)).toEqual({ ...clear, hasUnsaved: true });
    expect(check(through(12, [11]), delayed)).toEqual(clear);
    expect(check(through(12, [12]), delayed)).toEqual({ ...clear, hasUnsaved: true });
  });

  it("requires the tracked run itself, not just an older page boundary", () => {
    const state = through(12, [9]);
    expect(check(state, [run(12), run(8)], { reachesStart: true })).toEqual({ ...clear, listingIncomplete: true });
    expect(check({ ...state, reset_after_run: 9 }, [run(12), run(8)])).toEqual(clear);
    expect(check(state, [run(13, failure), run(8)])).toEqual({ hasUnsaved: true, listingIncomplete: true });
  });

  it("does not use legacy last_run as verified coverage or mistake it for new history", () => {
    expect(check({ last_run: 12, reset_after_run: 0 }, delayed, { reachesStart: true })).toEqual({ ...clear, listingIncomplete: true });
    expect(check(null, [run(11, running)], { reachesStart: true })).toEqual(clear);
    expect(check({ ...through(0), last_run: 12 }, [run(11, running)])).toEqual({ ...clear, listingIncomplete: true });
  });

  it.each([
    ["failure", true],
    [null, true],
    ["success", false],
  ])("checks attempt one (%s) while a re-run is active, on either listing", (first_attempt_conclusion, hasUnsaved) => {
    const rerun = run(10, { ...running, run_attempt: 2, first_attempt_conclusion });
    expect(check(through(9), [rerun, run(9)])).toEqual({ ...clear, hasUnsaved });
    expect(check(through(9), [run(9)], { activeRuns: [rerun] })).toEqual({ ...clear, hasUnsaved });
  });

  it("ignores an active first attempt and ineligible failures, but checks pull_request_target on a topic branch", () => {
    const runs = [run(14, running), run(13, { ...failure, event: "pull_request" }), run(12, { ...failure, head_branch: "topic" }), run(10)];
    expect(check(through(10), runs)).toEqual(clear);
    expect(check(through(10), [run(15, { ...failure, event: "pull_request_target", head_branch: "topic" }), ...runs])).toEqual({ ...clear, hasUnsaved: true });
  });
});
