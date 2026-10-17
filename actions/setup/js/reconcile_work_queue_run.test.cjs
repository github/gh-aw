// @ts-check
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { main } from "./reconcile_work_queue_run.cjs";
import { queueFixture, REF, REPOSITORY } from "./work_queue_lifecycle.test_helpers.cjs";

function setup(fixtureOptions = {}) {
  const fixture = queueFixture({ bound: true, count: 2, ...fixtureOptions });
  const nativeRun = { ...fixture.nativeRun(), status: "completed", conclusion: "failure" };
  const context = {
    ...fixture.dispatcherContext,
    runId: "99",
    eventName: "workflow_run",
    payload: { action: "completed", repository: { id: 7 }, workflow_run: structuredClone(nativeRun) },
  };
  const publisher = {
    ...fixture.nativeRun(fixture.dispatcherContext),
    id: "99",
    event: "workflow_run",
    path: ".github/workflows/agentic-workqueue.yml",
  };
  fixture.githubClient.rest.actions.getWorkflowRun = vi.fn().mockResolvedValue({ status: 200, data: publisher });
  fixture.githubClient.rest.actions.getWorkflowRunAttempt = vi.fn().mockResolvedValue({ status: 200, data: nativeRun });
  const options = {
    context,
    githubClient: fixture.githubClient,
    workflowRef: `${REPOSITORY}/.github/workflows/agentic-workqueue.yml@${REF}`,
    readWorkQueueLog: vi.fn(fixture.readWorkQueueLog),
    publishWorkQueueRequest: vi.fn(fixture.publishWorkQueueRequest),
    core: { info: vi.fn() },
    now: fixture.at,
  };
  return { fixture, nativeRun, publisher, options };
}

describe("trusted worker termination reconciliation", () => {
  it.each(["success", "failure", "cancelled"])("releases only the exact terminal binding after native %s and preserves retry accounting", async conclusion => {
    const { fixture, options, nativeRun } = setup();
    nativeRun.conclusion = conclusion;
    const attempts = [...fixture.state.works.values()].map(work => work.attempts);
    expect(await main(options)).toMatchObject({ released: true });
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).released).toBe(true);
    expect([...fixture.state.claims.values()].every(claim => claim.state === "cancelled")).toBe(true);
    expect([...fixture.state.works.values()].map(work => work.attempts)).toEqual(attempts);
    expect([...fixture.state.works.values()].every(work => work.state === "available")).toBe(true);
    expect(options.githubClient.rest.actions.getWorkflowRunAttempt).toHaveBeenCalledWith(expect.objectContaining({ run_id: "42", attempt_number: 1 }));
    const commits = fixture.transactions.length;
    expect(await main(options)).toMatchObject({ released: true });
    expect(fixture.transactions).toHaveLength(commits);
  });

  describe("privacy-preserving run reconciliation logging", () => {
    let stderr;
    const output = () => stderr.mock.calls.map(call => call[0]).join("");

    beforeEach(() => {
      vi.stubEnv("DEBUG", "work-queue:run");
      vi.stubEnv("ACTIONS_RUNNER_DEBUG", "");
      vi.stubEnv("RUNNER_DEBUG", "");
      stderr = vi.spyOn(process.stderr, "write").mockReturnValue(true);
    });
    afterEach(() => {
      vi.restoreAllMocks();
      vi.unstubAllEnvs();
    });

    it.each(["", "work-queue:other", "*,-work-queue:run"])("respects disabled or excluded logging with DEBUG=%s", async pattern => {
      vi.stubEnv("DEBUG", pattern);
      const { options } = setup();
      if (pattern.startsWith("*")) {
        options.context.eventName = "issues";
      }
      await main(options);
      expect(output()).not.toContain("[work-queue:run]");
    });

    it.each([true, false])("records released=%s without exposing event, task, or assignment data", async terminal => {
      const canary = "private-payload-token-should-not-be-logged";
      const { fixture, nativeRun, options } = setup({
        configureWork: work => {
          work.payload.plan = canary;
        },
      });
      Object.assign(options.context.payload.workflow_run, { name: canary, head_branch: canary, inputs: { token: canary } });
      nativeRun.display_title += ` ${canary}`;
      if (!terminal) {
        nativeRun.status = "in_progress";
        nativeRun.conclusion = null;
      }
      expect(await main(options)).toMatchObject({ released: terminal });
      expect(output()).toContain("[work-queue:run] run.reconcile.start");
      expect(output()).toContain("run.binding.checked candidates=1");
      expect(output()).toContain(`run.reconcile.complete released=${terminal} claims=2`);
      for (const sensitive of [canary, fixture.assignment.dispatch_id, fixture.assignment.claims[0].work_id, fixture.assignment.claims[0].handle, REF, REPOSITORY]) {
        expect(output()).not.toContain(sensitive);
      }
      expect(options.core.info.mock.calls.flat().join("")).not.toContain(canary);
    });

    it.each([
      ["event", "run.reconcile.ignored unsupported_event=true"],
      ["run", "run.reconcile.ignored unsupported_run=true"],
      ["staged", "run.reconcile.staged"],
      ["unbound", "run.reconcile.unbound"],
    ])("records the %s early exit without private metadata", async (kind, event) => {
      const { options } = setup();
      if (kind === "event") options.context.eventName = "private-event-name";
      if (kind === "run") options.context.payload.workflow_run.event = "private-event-name";
      if (kind === "staged") options.staged = true;
      if (kind === "unbound") options.context.payload.workflow_run.id = "100";
      await main(options);
      expect(output()).toContain(event);
      expect(output()).not.toContain("private-event-name");
      expect(output()).not.toContain("run.reconcile.complete");
      expect(options.publishWorkQueueRequest).not.toHaveBeenCalled();
    });

    it("logs bounded API failure metadata while preserving the original rejection", async () => {
      const canary = "private-error-body-token-and-stack";
      const error = Object.assign(new Error(canary, { cause: new Error(canary) }), {
        status: 503,
        response: { data: { message: canary }, headers: { authorization: canary } },
      });
      const { fixture, options } = setup();
      options.readWorkQueueLog.mockRejectedValue(error);
      await expect(main(options)).rejects.toBe(error);
      expect(output()).toContain("run.reconcile.failed failed=true http_status=503 queue_control_github_api_unavailable=true");
      expect(output()).not.toContain(canary);
      expect(output()).not.toContain("Error:");
      expect(output()).not.toContain("run.reconcile.complete");
      expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).released).toBe(false);
    });

    it("records ambiguous binding counts without exposing native identities", async () => {
      const { fixture, options } = setup();
      const state = fixture.state;
      state.dispatches.set("private-duplicate-binding", structuredClone(state.dispatches.get(fixture.assignment.dispatch_id)));
      options.readWorkQueueLog.mockResolvedValue({ sha: "head", transactions: fixture.transactions, state });
      await expect(main(options)).rejects.toThrow("work_queue_run_binding_ambiguous");
      expect(output()).toContain("run.binding.checked candidates=2");
      expect(output()).toContain("run.binding.ambiguous");
      expect(output()).toContain("run.reconcile.failed failed=true");
      expect(output()).not.toContain("private-duplicate-binding");
      expect(output()).not.toContain(fixture.assignment.dispatch_id);
      expect(options.publishWorkQueueRequest).not.toHaveBeenCalled();
    });
  });

  it("enforces retry exhaustion rather than silently reopening exhausted Work", async () => {
    const { fixture, options } = setup({
      configurePolicy: policy => {
        policy.pools.default.retry.max_attempts = 1;
      },
    });
    expect(await main(options)).toMatchObject({ released: true });
    expect([...fixture.state.works.values()].every(work => work.state === "cancelled" && work.cancellation_reason === "attempts_exhausted")).toBe(true);
  });

  it.each(["issues", "issue_comment", "schedule"])("ignores unrelated %s events without reading or mutating the queue", async eventName => {
    const { options } = setup();
    options.context.eventName = eventName;
    expect(await main(options)).toEqual({ status: "ignored" });
    expect(options.readWorkQueueLog).not.toHaveBeenCalled();
    expect(options.publishWorkQueueRequest).not.toHaveBeenCalled();
  });

  it.each(["rerun", "fork", "non-dispatch", "not-completed", "staged"])("does not mutate for %s events", async kind => {
    const { options } = setup();
    if (kind === "rerun") options.context.payload.workflow_run.run_attempt = 2;
    if (kind === "fork") options.context.payload.repository.fork = true;
    if (kind === "non-dispatch") options.context.payload.workflow_run.event = "pull_request";
    if (kind === "not-completed") options.context.payload.workflow_run.status = "in_progress";
    if (kind === "staged") options.staged = true;
    expect(await main(options)).toMatchObject({ status: kind === "staged" ? "staged_preview" : "ignored" });
    expect(options.readWorkQueueLog).not.toHaveBeenCalled();
    expect(options.publishWorkQueueRequest).not.toHaveBeenCalled();
  });

  it("does not reconcile another reservation when the event run is not bound", async () => {
    const { options } = setup();
    options.context.payload.workflow_run.id = "100";
    expect(await main(options)).toEqual({ status: "unbound" });
    expect(options.publishWorkQueueRequest).not.toHaveBeenCalled();
    expect(options.githubClient.rest.actions.getWorkflowRunAttempt).not.toHaveBeenCalled();
  });

  it.each(["revision", "principal", "repository", "correlation", "attempt", "unavailable", "publisher"])("retains capacity on independently read %s mismatch", async kind => {
    const { fixture, options, nativeRun, publisher } = setup();
    if (kind === "revision") nativeRun.head_sha = "b".repeat(40);
    if (kind === "principal") nativeRun.actor = { id: "22" };
    if (kind === "repository") nativeRun.repository = { ...nativeRun.repository, id: 8 };
    if (kind === "correlation") nativeRun.display_title = "unrelated";
    if (kind === "attempt") nativeRun.run_attempt = 2;
    if (kind === "publisher") publisher.path = ".github/workflows/untrusted.yml";
    if (kind === "unavailable") options.githubClient.rest.actions.getWorkflowRunAttempt.mockRejectedValue(new Error("API unavailable"));
    await expect(main(options)).rejects.toThrow();
    expect(options.publishWorkQueueRequest).not.toHaveBeenCalled();
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).released).toBe(false);
  });

  it("does not infer terminal state from the webhook alone", async () => {
    const { fixture, options, nativeRun } = setup();
    options.context.payload.workflow_run = structuredClone(nativeRun);
    nativeRun.status = "in_progress";
    nativeRun.conclusion = null;
    expect(await main(options)).toMatchObject({ released: false });
    expect(options.publishWorkQueueRequest).not.toHaveBeenCalled();
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).released).toBe(false);
  });

  it("preserves completed siblings and pending delivery without manufacturing Result", async () => {
    const { fixture, options } = setup();
    const handle = fixture.assignment.claims[0].handle;
    fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: handle, outcome: "completed" }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: handle });
    expect(await main(options)).toMatchObject({ released: true });
    const work = fixture.state.works.get(fixture.assignment.claims[0].work_id);
    expect(work).toMatchObject({ state: "completed", barrier: "pending" });
    expect(work.result).toBeUndefined();
    expect(fixture.transactions.flatMap(commit => commit.operations).some(operation => ["Result", "DeliveryFailure"].includes(operation.kind))).toBe(false);
  });
});
