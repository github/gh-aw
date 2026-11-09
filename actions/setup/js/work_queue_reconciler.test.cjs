// @ts-check
import { describe, expect, it, vi } from "vitest";
import { discoverRuns, reconcileDispatch } from "./work_queue_reconciler.cjs";
import { queueFixture, DISPATCHER, REF, REPOSITORY, WORKFLOW } from "./work_queue_lifecycle.test_helpers.cjs";

function setup(configurePolicy = undefined) {
  const fixture = queueFixture({ count: 1, started: true, configurePolicy });
  const post = vi.fn();
  const list = vi.fn().mockResolvedValue({ status: 200, data: { workflow_runs: [fixture.nativeRun()] } });
  fixture.githubClient.rest.actions.createWorkflowDispatch = post;
  fixture.githubClient.rest.actions.listWorkflowRuns = list;
  const options = {
    assignment: fixture.assignment,
    githubClient: fixture.githubClient,
    context: fixture.dispatcherContext,
    workflowRef: `${REPOSITORY}/${DISPATCHER}@${REF}`,
    readWorkQueueLog: fixture.readWorkQueueLog,
    publishWorkQueueRequest: fixture.publishWorkQueueRequest,
    now: fixture.at,
    sleepFn: async () => {},
  };
  const expected = { repository: REPOSITORY, repository_id: 7, workflow: WORKFLOW, ref: REF, principal_id: "11", dispatch_id: fixture.assignment.dispatch_id };
  return { fixture, options, expected, post, list };
}

function expectFenced(fixture, post) {
  const dispatch = fixture.state.dispatches.get(fixture.assignment.dispatch_id);
  expect(dispatch.run).toBeUndefined();
  expect(dispatch.released).toBe(false);
  expect([...fixture.state.claims.values()].every(claim => claim.state === "open")).toBe(true);
  expect(post).not.toHaveBeenCalled();
  expect(fixture.transactions.flatMap(commit => commit.operations).some(operation => ["Release", "Result", "DeliveryFailure"].includes(operation.kind))).toBe(false);
}

describe("native discovery with delayed attempt correlation", () => {
  it.each([404, 500])("retains the returned run hint after a transient HTTP %s outside the discovery window", async status => {
    const { fixture, options, post, list } = setup(policy => {
      policy.pools.default.reconciliation.max_attempts = 2;
    });
    const read = vi
      .fn()
      .mockRejectedValueOnce(Object.assign(new Error("native read unavailable"), { status }))
      .mockResolvedValue({ status: 200, data: fixture.nativeRun() });
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = read;
    list.mockResolvedValue({ status: 200, data: { workflow_runs: [] } });
    const sleepFn = vi.fn(async () => expectFenced(fixture, post));

    expect(await reconcileDispatch({ ...options, returnedRunId: "42", sleepFn })).toMatchObject({ state: "bound", run_id: "42", released: false });
    expect(sleepFn).toHaveBeenCalledExactlyOnceWith(50);
    expect(read).toHaveBeenCalledTimes(3);
    expect(read.mock.calls.every(([parameters]) => parameters.run_id === "42" && parameters.attempt_number === 1 && parameters.request.retries === 0)).toBe(true);
    expect(list).toHaveBeenCalledTimes(1);
    expect(fixture.transactions.flatMap(commit => commit.operations).filter(operation => operation.kind === "Dispatch" && operation.state === "bound")).toHaveLength(1);
    expect(post).not.toHaveBeenCalled();
  });

  it("retains a discovered pending run hint across a subsequent transient failure", async () => {
    const { fixture, options, post, list } = setup(policy => {
      policy.pools.default.reconciliation.max_attempts = 3;
    });
    const read = vi
      .fn()
      .mockResolvedValueOnce({ status: 200, data: { ...fixture.nativeRun(), display_title: "worker" } })
      .mockRejectedValueOnce(Object.assign(new Error("native read unavailable"), { status: 500 }))
      .mockResolvedValue({ status: 200, data: fixture.nativeRun() });
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = read;
    list.mockResolvedValueOnce({ status: 200, data: { workflow_runs: [fixture.nativeRun()] } }).mockResolvedValue({ status: 200, data: { workflow_runs: [] } });
    const sleepFn = vi.fn(async () => expectFenced(fixture, post));

    expect(await reconcileDispatch({ ...options, sleepFn })).toMatchObject({ state: "bound", run_id: "42", released: false });
    expect(sleepFn.mock.calls).toEqual([[1000], [100]]);
    expect(read).toHaveBeenCalledTimes(4);
    expect(read.mock.calls.every(([parameters]) => parameters.run_id === "42" && parameters.attempt_number === 1 && parameters.request.retries === 0)).toBe(true);
    expect(list).toHaveBeenCalledTimes(2);
    expect(post).not.toHaveBeenCalled();
  });

  it("keeps an unhinted started launch unresolved when discovery cannot find its run", async () => {
    const { fixture, options, post, list } = setup(policy => {
      policy.pools.default.reconciliation.max_attempts = 2;
    });
    const read = vi.fn();
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = read;
    list.mockResolvedValue({ status: 200, data: { workflow_runs: [] } });

    expect(await reconcileDispatch(options)).toMatchObject({ state: "launch_unresolved", released: false, attempts: 2 });
    expect(list).toHaveBeenCalledTimes(2);
    expect(read).not.toHaveBeenCalled();
    expectFenced(fixture, post);
  });

  it("keeps an exact valid hint fenced when two full discovery pages overflow", async () => {
    const { fixture, options, post, list } = setup();
    const read = vi.fn().mockResolvedValue({ status: 200, data: fixture.nativeRun() });
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = read;
    list.mockImplementation(async ({ page }) => ({
      status: 200,
      data: { workflow_runs: Array.from({ length: 50 }, (_, index) => ({ ...fixture.nativeRun(), id: String(100 + (page - 1) * 50 + index), display_title: "unrelated dispatch" })) },
    }));

    expect(await reconcileDispatch({ ...options, returnedRunId: "42" })).toEqual({ state: "run_binding_conflict", released: false });
    expect(read).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ run_id: "42", attempt_number: 1 }));
    expect(list).toHaveBeenCalledTimes(2);
    expect(list.mock.calls.map(([parameters]) => ({ page: parameters.page, per_page: parameters.per_page }))).toEqual([
      { page: 1, per_page: 50 },
      { page: 2, per_page: 50 },
    ]);
    expectFenced(fixture, post);
  });

  it.each([{ status: 404 }, { status: 500 }, { head_sha: "b".repeat(40) }, { actor: { id: "99" } }, { run_attempt: 2 }])("keeps a repeatedly unavailable or invalid hinted run fenced within the retry budget: %j", async override => {
    const { fixture, options, post, list } = setup(policy => {
      policy.pools.default.reconciliation.max_attempts = 2;
    });
    const read = override.status ? vi.fn().mockRejectedValue(Object.assign(new Error("native read unavailable"), { status: override.status })) : vi.fn().mockResolvedValue({ status: 200, data: { ...fixture.nativeRun(), ...override } });
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = read;

    expect(await reconcileDispatch({ ...options, returnedRunId: "42" })).toMatchObject({ state: "launch_unresolved", released: false, attempts: 2 });
    expect(read).toHaveBeenCalledTimes(2);
    expect(list).not.toHaveBeenCalled();
    expectFenced(fixture, post);
  });

  it("returns a title-pending run separately from validated candidates and conflicts", async () => {
    const { fixture, options, expected } = setup();
    const read = vi.fn().mockResolvedValue({ ...fixture.nativeRun(), display_title: "worker" });
    expect(await discoverRuns(options, expected, read)).toEqual({ candidates: [], correlationPending: ["42"], conflict: false });
    expect(read).toHaveBeenCalledExactlyOnceWith("42", 1);
  });

  it.each([false, true])("retries the discovered hint when its attempt title is delayed with returnedHint=%s", async returnedHint => {
    const { fixture, options, post, list } = setup();
    const clock = vi.spyOn(Date, "now").mockReturnValue(0);
    const read = vi.fn(async () => ({
      status: 200,
      data: { ...fixture.nativeRun(), ...(Date.now() < 10000 && (!returnedHint || read.mock.calls.length > 1) ? { display_title: "worker" } : {}) },
    }));
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = read;
    list.mockResolvedValueOnce({ status: 200, data: { workflow_runs: [fixture.nativeRun()] } }).mockResolvedValue({ status: 200, data: { workflow_runs: [] } });
    const sleepFn = vi.fn(async delay => {
      expectFenced(fixture, post);
      clock.mockReturnValue(Date.now() + delay);
    });
    try {
      expect(await reconcileDispatch({ ...options, sleepFn, ...(returnedHint ? { returnedRunId: "42" } : {}) })).toMatchObject({ state: "bound", run_id: "42", released: false });
      expect(sleepFn.mock.calls).toEqual([[1000], [2000], [4000], [8000]]);
      expect(read).toHaveBeenCalledTimes(returnedHint ? 7 : 6);
      expect(read.mock.calls.every(([parameters]) => parameters.run_id === "42" && parameters.attempt_number === 1 && parameters.request.retries === 0)).toBe(true);
      expect(list).toHaveBeenCalledTimes(2);
      expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id)).toMatchObject({ state: "bound", released: false, run: { run_id: "42", run_attempt: 1 } });
      expect(fixture.transactions.flatMap(commit => commit.operations).filter(operation => operation.kind === "Dispatch" && operation.state === "bound")).toHaveLength(1);
      expect(post).not.toHaveBeenCalled();
      expect(fixture.transactions.flatMap(commit => commit.operations).some(operation => ["Release", "Result", "DeliveryFailure"].includes(operation.kind))).toBe(false);
    } finally {
      clock.mockRestore();
    }
  });

  it.each([
    { deadline: 60000, maxAttempts: 3, sleeps: [1000, 2000], reads: 3 },
    { deadline: 2500, maxAttempts: 5, sleeps: [1000, 1500], reads: 2 },
  ])("keeps pending discovery fenced within deadline=$deadline and maxAttempts=$maxAttempts", async ({ deadline, maxAttempts, sleeps, reads }) => {
    const { fixture, options, post, list } = setup(policy => {
      policy.pools.default.reconciliation.deadline_ms = deadline;
      policy.pools.default.reconciliation.max_attempts = maxAttempts;
    });
    const read = vi.fn().mockResolvedValue({ status: 200, data: { ...fixture.nativeRun(), display_title: "worker" } });
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = read;
    const clock = vi.spyOn(Date, "now").mockReturnValue(0);
    const sleepFn = vi.fn(async delay => clock.mockReturnValue(Date.now() + delay));
    try {
      expect(await reconcileDispatch({ ...options, sleepFn })).toMatchObject({ state: "launch_unresolved", released: false, attempts: reads });
      expect(sleepFn.mock.calls).toEqual(sleeps.map(delay => [delay]));
      expect(read).toHaveBeenCalledTimes(reads);
      expect(list).toHaveBeenCalledTimes(1);
      expectFenced(fixture, post);
    } finally {
      clock.mockRestore();
    }
  });

  it.each(["both", "first", "second"])("still rejects distinct correlated runs when pending titles belong to %s", async pending => {
    const { fixture, options, post, list } = setup();
    list.mockResolvedValue({ status: 200, data: { workflow_runs: [fixture.nativeRun(), { ...fixture.nativeRun(), id: "43" }] } });
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = vi.fn(async ({ run_id }) => ({
      status: 200,
      data: { ...fixture.nativeRun(), id: run_id, ...(pending === "both" || run_id === (pending === "first" ? "42" : "43") ? { display_title: "worker" } : {}) },
    }));
    expect(await reconcileDispatch(options)).toEqual({ state: "run_binding_conflict", released: false });
    expectFenced(fixture, post);
  });

  it("rejects a pending discovered run competing with an independently validated returned hint", async () => {
    const { fixture, options, post, list } = setup();
    list.mockResolvedValue({ status: 200, data: { workflow_runs: [{ ...fixture.nativeRun(), id: "43" }] } });
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = vi.fn(async ({ run_id }) => ({
      status: 200,
      data: { ...fixture.nativeRun(), id: run_id, ...(run_id === "43" ? { display_title: "worker" } : {}) },
    }));
    expect(await reconcileDispatch({ ...options, returnedRunId: "42" })).toEqual({ state: "run_binding_conflict", released: false });
    expectFenced(fixture, post);
  });

  it.each([
    { id: "43" },
    { run_attempt: 2 },
    { repository: { full_name: "foreign/repo", id: 7 } },
    { repository: { full_name: REPOSITORY, id: 8 } },
    { event: "push" },
    { head_sha: "b".repeat(40) },
    { path: ".github/workflows/foreign.lock.yml" },
    { actor: { id: "99" } },
    { triggering_actor: { id: "99" } },
  ])("preserves immutable identity conflicts even with a pending title: %j", async override => {
    const { fixture, options, expected } = setup();
    const read = vi.fn().mockResolvedValue({ ...fixture.nativeRun(), display_title: "worker", ...override });
    expect(await discoverRuns(options, expected, read)).toEqual({ candidates: [], correlationPending: [], conflict: true });
  });

  it("keeps a numeric workflow identity mismatch distinct from a pending title", async () => {
    const { fixture, options, expected } = setup();
    const read = vi.fn().mockResolvedValue({ ...fixture.nativeRun(), workflow_id: 102, display_title: "worker" });
    expect(await discoverRuns(options, { ...expected, workflow_id: 101 }, read)).toEqual({ candidates: [], correlationPending: [], conflict: true });
  });

  it("deduplicates pending run IDs without weakening the bounded discovery overflow fence", async () => {
    const { fixture, options, expected, list } = setup();
    list.mockResolvedValue({ status: 200, data: { workflow_runs: Array.from({ length: 50 }, () => fixture.nativeRun()) } });
    const read = vi.fn().mockResolvedValue({ ...fixture.nativeRun(), display_title: "worker" });
    expect(await discoverRuns(options, expected, read)).toEqual({ candidates: [], correlationPending: ["42"], conflict: true });
    expect(list).toHaveBeenCalledTimes(2);
    expect(read).toHaveBeenCalledExactlyOnceWith("42", 1);
  });
});
