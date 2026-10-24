// @ts-check
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { main, maintenance } from "./reconcile_work_queue_run.cjs";
import { createRequire } from "node:module";
import { reconcileQueue } from "./work_queue_reconciler.cjs";
import { queueFixture, REF, REPOSITORY, WORKFLOW } from "./work_queue_lifecycle.test_helpers.cjs";

const require = createRequire(import.meta.url);

function setup(fixtureOptions = {}) {
  const fixture = queueFixture({ bound: true, count: 2, ...fixtureOptions });
  const nativeRun = fixture.nativeRun();
  const publisher = structuredClone(nativeRun);
  const jobs = [
    { id: "201", run_id: "42", run_attempt: 1, name: "activation", status: "completed", conclusion: "success" },
    { id: "202", run_id: "42", run_attempt: 1, name: "agent", status: "completed", conclusion: "success" },
    { id: "203", run_id: "42", run_attempt: 1, name: "conclusion", status: "in_progress", conclusion: null },
  ];
  fixture.githubClient.rest.actions.getWorkflowRun = vi.fn().mockResolvedValue({ status: 200, data: publisher });
  fixture.githubClient.rest.actions.getWorkflowRunAttempt = vi.fn().mockResolvedValue({ status: 200, data: nativeRun });
  fixture.githubClient.rest.actions.listJobsForWorkflowRunAttempt = vi.fn().mockImplementation(async () => ({ status: 200, data: { jobs, total_count: jobs.length } }));
  const options = {
    context: { ...fixture.workerContext, job: "conclusion" },
    githubClient: fixture.githubClient,
    workflowRef: `${REPOSITORY}/${WORKFLOW}@${REF}`,
    readWorkQueueLog: vi.fn(fixture.readWorkQueueLog),
    publishWorkQueueRequest: vi.fn(fixture.publishWorkQueueRequest),
    core: { info: vi.fn() },
    jobNames: { activation: "activation", agent: "agent", conclusion: "conclusion" },
    jobResults: { activation: { result: "success" }, agent: { result: "success" } },
    sleepFn: vi.fn(async () => {}),
    now: fixture.at,
  };
  return { fixture, nativeRun, publisher, jobs, options };
}

describe("inline trusted worker termination", () => {
  it("prefixes invalid worker job context with a stable error code", async () => {
    const { options } = setup();
    options.jobNames = {};

    await expect(main(options)).rejects.toMatchObject({
      message: "ERR_VALIDATION: work_queue_worker_job_context_invalid",
      code: "work_queue_worker_job_context_invalid",
      category: "ERR_WORK_QUEUE_RECONCILIATION",
    });
  });

  it.each(["success", "failure", "cancelled", "skipped"])("releases after predecessor jobs end with %s while the run is still active", async conclusion => {
    const { fixture, jobs, nativeRun, options } = setup();
    jobs[1].conclusion = conclusion;
    options.jobResults.agent.result = conclusion;
    const attempts = [...fixture.state.works.values()].map(work => work.attempts);
    expect(await main(options)).toMatchObject({ released: true });
    expect(nativeRun.status).toBe("in_progress");
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).released).toBe(true);
    expect([...fixture.state.claims.values()].every(claim => claim.state === "cancelled")).toBe(true);
    expect([...fixture.state.works.values()].map(work => work.attempts)).toEqual(attempts);
    expect([...fixture.state.works.values()].every(work => work.state === "available")).toBe(true);
    const release = fixture.transactions.at(-1).operations.find(operation => operation.kind === "Release");
    expect(release.evidence).toMatchObject({ kind: "worker_termination", source: "github_api", run_id: "42", run_attempt: 1 });
    expect(release.evidence).not.toHaveProperty("status");
    expect(release.evidence).not.toHaveProperty("conclusion");
    const count = fixture.transactions.length;
    await expect(main(options)).resolves.toMatchObject({ released: true });
    expect(fixture.transactions).toHaveLength(count);
  });

  it("preserves completed siblings and pending delivery without manufacturing Result", async () => {
    const { fixture, options } = setup();
    const handle = fixture.assignment.claims[0].handle;
    fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: handle, outcome: "completed" }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: handle });
    await main(options);
    expect(fixture.state.works.get(fixture.assignment.claims[0].work_id)).toMatchObject({ state: "completed", barrier: "pending" });
    expect(fixture.transactions.flatMap(commit => commit.operations).some(operation => ["Result", "DeliveryFailure"].includes(operation.kind))).toBe(false);
  });

  it("recovers a socket failure in the final release authentication without replaying effects or creating Result", async () => {
    const { fixture, options } = setup();
    const member = fixture.assignment.claims[0];
    fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: member.handle, outcome: "completed" }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: member.handle });
    const get = options.githubClient.rest.repos.get;
    const read = vi.fn(async parameters => {
      if (read.mock.calls.length === 3) throw Object.assign(new Error("other side closed"), { cause: { cause: { code: "UND_ERR_SOCKET" } } });
      return get(parameters);
    });
    options.githubClient.rest.repos.get = read;
    await expect(main(options)).resolves.toMatchObject({ released: true });
    expect(read).toHaveBeenCalledTimes(4);
    expect(options.sleepFn).toHaveBeenCalledExactlyOnceWith(1000);
    const operations = fixture.transactions.flatMap(commit => commit.operations);
    expect(operations.filter(operation => operation.kind === "Release")).toHaveLength(1);
    expect(operations.some(operation => ["Result", "DeliveryFailure"].includes(operation.kind))).toBe(false);
    expect(fixture.state.works.get(member.work_id)).toMatchObject({ state: "completed", barrier: "pending" });
  });

  it("retries only a failed job-inventory page before releasing the original reservation", async () => {
    const { fixture, options } = setup();
    const read = options.githubClient.rest.actions.listJobsForWorkflowRunAttempt;
    read.mockRejectedValueOnce({ status: 503 });
    await expect(main(options)).resolves.toMatchObject({ released: true });
    expect(read).toHaveBeenCalledTimes(2);
    expect(read.mock.calls[0][0]).toEqual(read.mock.calls[1][0]);
    expect(read.mock.calls[0][0]).toMatchObject({ run_id: "42", attempt_number: 1, page: 1, request: { retries: 0, retryCount: 0, timeout: 15000 } });
    expect(options.sleepFn).toHaveBeenCalledExactlyOnceWith(1000);
    expect(fixture.transactions.flatMap(commit => commit.operations).filter(operation => operation.kind === "Release")).toHaveLength(1);
  });

  it.each(["UND_ERR_SOCKET", "ECONNRESET", "ETIMEDOUT"])("recovers %s at the original-attempt boundary before checking quiescence", async code => {
    const { fixture, options } = setup();
    const member = fixture.assignment.claims[0];
    fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: member.handle, outcome: "completed" }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: member.handle });
    const read = options.githubClient.rest.actions.getWorkflowRunAttempt;
    read.mockRejectedValueOnce({ cause: { cause: { code } } }).mockRejectedValueOnce({ status: 503 });
    const jobs = options.githubClient.rest.actions.listJobsForWorkflowRunAttempt;
    const list = jobs.getMockImplementation();
    jobs.mockImplementation(async parameters => {
      expect(read).toHaveBeenCalledTimes(3);
      expect(options.publishWorkQueueRequest).not.toHaveBeenCalled();
      return list(parameters);
    });
    await expect(main(options)).resolves.toMatchObject({ released: true });
    for (const [parameters] of read.mock.calls) {
      expect(parameters).toEqual(read.mock.calls[0][0]);
      expect(parameters).toMatchObject({ run_id: "42", attempt_number: 1, request: { retries: 0, retryCount: 0, timeout: 15000 } });
    }
    expect(options.sleepFn.mock.calls).toEqual([[1000], [1000]]);
    expect(jobs).toHaveBeenCalledTimes(1);
    const operations = fixture.transactions.flatMap(commit => commit.operations);
    expect(operations.filter(operation => operation.kind === "Release")).toHaveLength(1);
    expect(operations.some(operation => ["Result", "DeliveryFailure"].includes(operation.kind))).toBe(false);
    expect(fixture.state.works.get(member.work_id)).toMatchObject({ state: "completed", barrier: "pending" });
  });

  it.each(["repository", "publisher", "attempt", "jobs"])("retains the reservation and pending delivery after three failed %s reads", async endpoint => {
    const { fixture, options } = setup();
    const member = fixture.assignment.claims[0];
    fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: member.handle, outcome: "completed" }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: member.handle });
    const read = vi.fn().mockRejectedValue({ status: 503 });
    if (endpoint === "repository") options.githubClient.rest.repos.get = read;
    else if (endpoint === "publisher") options.githubClient.rest.actions.getWorkflowRun = read;
    else if (endpoint === "attempt") options.githubClient.rest.actions.getWorkflowRunAttempt = read;
    else options.githubClient.rest.actions.listJobsForWorkflowRunAttempt = read;
    await expect(main(options)).rejects.toMatchObject({ status: 503 });
    expect(read).toHaveBeenCalledTimes(3);
    expect(options.sleepFn.mock.calls).toEqual([[1000], [1000]]);
    expect(options.publishWorkQueueRequest).not.toHaveBeenCalled();
    if (endpoint !== "jobs") expect(options.githubClient.rest.actions.listJobsForWorkflowRunAttempt).not.toHaveBeenCalled();
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).released).toBe(false);
    expect(fixture.state.works.get(member.work_id)).toMatchObject({ state: "completed", barrier: "pending" });
  });

  it("uses the authenticated bound principal for logical AW profiles without a stored principal", async () => {
    const { fixture, options } = setup({
      workerPrincipal: "22",
      configurePolicy: policy => {
        policy.authorization = "aw";
        policy.producers = {};
        delete policy.pools.default.profiles.default.principal;
        policy.pools.default.profiles.default.logical_contract = "b".repeat(64);
      },
    });
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).profile).not.toHaveProperty("principal");
    await expect(main(options)).resolves.toMatchObject({ released: true });
    const release = fixture.transactions.at(-1).operations.find(operation => operation.kind === "Release");
    expect(release.evidence.principal).toBe("22");
    expect(JSON.parse(JSON.stringify(release.evidence))).toEqual(release.evidence);
  });

  it("enforces retry exhaustion", async () => {
    const { fixture, options } = setup({
      configurePolicy: policy => {
        policy.pools.default.retry.max_attempts = 1;
      },
    });
    await main(options);
    expect([...fixture.state.works.values()].every(work => work.state === "cancelled" && work.cancellation_reason === "attempts_exhausted")).toBe(true);
  });

  it.each(["issues", "workflow_run", "schedule", "fork"])("ignores %s without queue reads", async kind => {
    const { options } = setup();
    if (kind === "fork") options.context.payload.repository.fork = true;
    else options.context.eventName = kind;
    expect(await main(options)).toEqual({ status: "ignored" });
    expect(options.readWorkQueueLog).not.toHaveBeenCalled();
  });

  it.each(["rerun", "wrong-job"])("rejects %s before accessing the ledger", async kind => {
    const { options } = setup();
    if (kind === "rerun") options.context.runAttempt = 2;
    else options.context.job = "safe_outputs";
    await expect(main(options)).rejects.toThrow("ERR_VALIDATION: work_queue_worker_finalizer_context_invalid");
    expect(options.readWorkQueueLog).not.toHaveBeenCalled();
  });

  it("previews staged cleanup without reads or writes", async () => {
    const { options } = setup();
    options.staged = true;
    expect(await main(options)).toEqual({ status: "staged_preview" });
    expect(options.readWorkQueueLog).not.toHaveBeenCalled();
  });

  it("does not borrow another run's reservation", async () => {
    const { options, fixture } = setup();
    options.context.runId = "100";
    expect(await main(options)).toEqual({ status: "unbound" });
    expect(options.publishWorkQueueRequest).not.toHaveBeenCalled();
    expect(options.githubClient.rest.actions.getWorkflowRunAttempt).not.toHaveBeenCalled();
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).released).toBe(false);
  });

  it.each(["revision", "principal", "repository", "correlation", "attempt", "unavailable", "publisher"])("retains capacity on independent %s mismatch", async kind => {
    const { fixture, options, nativeRun, publisher } = setup();
    if (kind === "revision") nativeRun.head_sha = "b".repeat(40);
    if (kind === "principal") nativeRun.actor = { id: "22" };
    if (kind === "repository") nativeRun.repository.id = 8;
    if (kind === "correlation") nativeRun.display_title = "unrelated";
    if (kind === "attempt") nativeRun.run_attempt = 2;
    if (kind === "publisher") publisher.path = ".github/workflows/untrusted.yml";
    if (kind === "unavailable") options.githubClient.rest.actions.getWorkflowRunAttempt.mockRejectedValue(new Error("API unavailable"));
    await expect(main(options)).rejects.toThrow();
    expect(options.publishWorkQueueRequest).not.toHaveBeenCalled();
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).released).toBe(false);
  });

  it.each(["active", "queued", "missing", "duplicate-finalizer", "wrong-attempt", "wrong-run", "duplicate-id", "missing-conclusion", "missing-metadata", "unfinished-needs", "missing-finalizer", "unavailable"])(
    "retains capacity for %s job evidence",
    async kind => {
      const { fixture, options, jobs } = setup();
      if (kind === "active") jobs[1].status = "in_progress";
      if (kind === "queued") jobs[1].status = "queued";
      if (kind === "missing") jobs.splice(1, 1);
      if (kind === "duplicate-finalizer") jobs.push({ ...jobs[2], id: "204" });
      if (kind === "wrong-attempt") jobs[0].run_attempt = 2;
      if (kind === "wrong-run") jobs[0].run_id = "99";
      if (kind === "duplicate-id") jobs[0].id = jobs[1].id;
      if (kind === "missing-conclusion") jobs[0].conclusion = null;
      if (kind === "missing-metadata") options.jobResults = {};
      if (kind === "unfinished-needs") options.jobResults.agent.result = "in_progress";
      if (kind === "missing-finalizer") jobs.pop();
      if (kind === "unavailable") options.githubClient.rest.actions.listJobsForWorkflowRunAttempt.mockRejectedValue(new Error("API unavailable"));
      await expect(main(options)).rejects.toThrow();
      expect(options.publishWorkQueueRequest).not.toHaveBeenCalled();
      expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).released).toBe(false);
    }
  );

  it("accepts custom job names and matrix members", async () => {
    const { options, jobs } = setup();
    options.jobNames.agent = "Execute";
    options.jobNames.conclusion = "Wrap up";
    jobs[1].name = "Execute (node, linux)";
    jobs[2].name = "Wrap up";
    await expect(main(options)).resolves.toMatchObject({ released: true });
  });

  it("uses the attempt-specific endpoint when optional job attempt fields are absent", async () => {
    const { options, jobs } = setup();
    for (const job of jobs) delete job.run_attempt;
    await expect(main(options)).resolves.toMatchObject({ released: true });
    expect(options.githubClient.rest.actions.listJobsForWorkflowRunAttempt).toHaveBeenCalledWith(expect.objectContaining({ run_id: "42", attempt_number: 1 }));
  });

  it("verifies complete paginated inventories", async () => {
    const { options, jobs } = setup();
    const completed = Array.from({ length: 99 }, (_, index) => ({ ...jobs[0], id: String(1000 + index) }));
    const inventory = [...completed, jobs[1], jobs[2]];
    options.githubClient.rest.actions.listJobsForWorkflowRunAttempt.mockImplementation(async ({ page }) => ({
      status: 200,
      data: { jobs: inventory.slice((page - 1) * 100, page * 100), total_count: inventory.length },
    }));
    await expect(main(options)).resolves.toMatchObject({ released: true });
    expect(options.githubClient.rest.actions.listJobsForWorkflowRunAttempt).toHaveBeenCalledTimes(2);
  });

  it("fails closed when a paginated job listing is incomplete or exceeds its bound", async () => {
    for (const total of [101, 500]) {
      const { options, jobs } = setup();
      const page = Array.from({ length: 100 }, (_, index) => ({ ...jobs[0], id: String(1000 + index) }));
      options.githubClient.rest.actions.listJobsForWorkflowRunAttempt.mockResolvedValue({ status: 200, data: { total_count: total, jobs: total === 101 ? jobs : page } });
      await expect(main(options)).rejects.toThrow(total === 101 ? "work_queue_worker_jobs_incomplete" : "work_queue_worker_jobs_limit");
      expect(options.publishWorkQueueRequest).not.toHaveBeenCalled();
    }
  });
});

describe("scheduled work queue recovery", () => {
  it.each([
    { pending: false, deferIssueFailure: false },
    { pending: true, deferIssueFailure: false },
    { pending: true, deferIssueFailure: true },
  ])("projects only this maintenance run's committed terminal requests without rollback: %j", async ({ pending, deferIssueFailure }) => {
    const { fixture, options, nativeRun } = setup({ count: 1 });
    const member = fixture.assignment.claims[0];
    fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: member.handle, outcome: "completed" }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: member.handle });
    await main(options);
    options.context = { ...fixture.dispatcherContext, eventName: "schedule", job: "work_queue_compaction" };
    const publisher = { ...fixture.nativeRun(fixture.dispatcherContext), event: "schedule", path: ".github/workflows/agentics-maintenance.yml" };
    options.workflowRef = `${REPOSITORY}/${publisher.path}@${REF}`;
    options.githubClient.rest.actions.getWorkflowRun.mockResolvedValue({ status: 200, data: publisher });
    options.now = fixture.at + fixture.policy.pools.default.reconciliation.deadline_ms;
    options.publishWorkQueueRequest = vi.fn(async args => fixture.append(args.request.kind, args.request.parameters, args.actor, args.request.id, options.now));
    nativeRun.status = "completed";
    nativeRun.conclusion = "failure";
    const configuration = vi.spyOn(require("./cancel_work_queue_closed_issue.cjs"), "readIssuesConfig").mockResolvedValue({ label: "work", title_prefix: "[work queue]" });
    const projection = vi.spyOn(require("./work_queue_issues.cjs"), "main").mockResolvedValue({ pending: pending ? [{ reason: "unavailable" }] : [] });
    try {
      if (pending && !deferIssueFailure) {
        await expect(maintenance({ ...options, projectIssues: true, deferIssueFailure })).rejects.toMatchObject({
          message: expect.stringMatching(/^ERR_VALIDATION: work_queue_issue_synchronization_pending; queue transitions are committed/),
          code: "work_queue_issue_synchronization_pending",
          category: "ERR_WORK_QUEUE_RECONCILIATION",
        });
      } else expect(await maintenance({ ...options, projectIssues: true, deferIssueFailure })).toMatchObject({ issue_sync_pending: pending ? [{ reason: "unavailable" }] : [] });
      expect(projection).toHaveBeenCalledOnce();
      const args = projection.mock.calls[0][0];
      expect(args.trustedContext).toMatchObject({ role: "reconciler", workflow: publisher.path, event: "schedule" });
      expect(args.transitionRequestIds).toHaveLength(1);
      const receipt = fixture.state.requests.get(args.transitionRequestIds[0]);
      expect(receipt.operations.some(operation => operation.kind === "DeliveryFailure" && operation.work_id === member.work_id)).toBe(true);
      expect(receipt.actor.run_id).toBe(String(publisher.id));
      expect(receipt.actor.run_attempt).toBe(publisher.run_attempt);
    } finally {
      configuration.mockRestore();
      projection.mockRestore();
    }
  });

  it.each(["pending", "throw"])("attempts later projection batches after an earlier %s batch and collects diagnostics", async mode => {
    const { fixture, options, nativeRun } = setup({ count: 1 });
    options.context = { ...fixture.dispatcherContext, job: "work_queue_compaction" };
    const publisher = fixture.nativeRun(fixture.dispatcherContext);
    options.workflowRef = `${REPOSITORY}/${publisher.path}@${REF}`;
    options.githubClient.rest.actions.getWorkflowRun.mockResolvedValue({ status: 200, data: publisher });
    nativeRun.status = "completed";
    nativeRun.conclusion = "failure";
    const actor = { role: "reconciler", principal: "11", repository: REPOSITORY, workflow: publisher.path, run_id: String(publisher.id), run_attempt: 1 };
    const workId = fixture.assignment.claims[0].work_id;
    for (let index = 0; index < 17; index++) fixture.append("cancel_work", { operations: [{ kind: "WorkCancellation", work_id: workId, reason: "attempts_exhausted" }] }, actor, `terminal-${index}`);
    const configuration = vi.spyOn(require("./cancel_work_queue_closed_issue.cjs"), "readIssuesConfig").mockResolvedValue({ label: "work" });
    const projection = vi
      .spyOn(require("./work_queue_issues.cjs"), "main")
      .mockImplementationOnce(async () => {
        if (mode === "throw") throw new Error("secret-batch-failure");
        return { pending: [{ work_id: workId, reason: "first batch pending" }] };
      })
      .mockResolvedValue({ pending: [{ work_id: workId, reason: "second batch pending" }] });
    try {
      const result = await maintenance({ ...options, projectIssues: true, deferIssueFailure: true });
      expect(projection).toHaveBeenCalledTimes(2);
      expect(projection.mock.calls.map(([args]) => args.transitionRequestIds.length)).toEqual([16, 1]);
      expect(result.issue_sync_pending).toHaveLength(2);
      expect(result.issue_sync_pending[1].reason).toBe("second batch pending");
      expect(JSON.stringify(result.issue_sync_pending)).not.toContain("secret-batch-failure");
      expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).released).toBe(true);
    } finally {
      configuration.mockRestore();
      projection.mockRestore();
    }
  });

  it.each([false, true])("settles an expired pending delivery after inline release with keep_open=%s without replay or Result", async keepOpen => {
    const { fixture, options, nativeRun } = setup({ count: 1 });
    const member = fixture.assignment.claims[0];
    fixture.append(
      "finish",
      { dispatch_id: fixture.assignment.dispatch_id, claim_handle: member.handle, outcome: "completed", keep_open: keepOpen },
      { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: member.handle }
    );
    await main(options);
    expect(fixture.state.works.get(member.work_id).barrier).toBe("pending");
    options.context = { ...fixture.dispatcherContext, job: "work_queue_compaction" };
    const publisher = fixture.nativeRun(fixture.dispatcherContext);
    options.workflowRef = `${REPOSITORY}/${publisher.path}@${REF}`;
    options.githubClient.rest.actions.getWorkflowRun.mockResolvedValue({ status: 200, data: publisher });
    options.now = fixture.at + fixture.policy.pools.default.reconciliation.deadline_ms;
    options.publishWorkQueueRequest = vi.fn(async args => fixture.append(args.request.kind, args.request.parameters, args.actor, args.request.id, options.now));
    nativeRun.status = "completed";
    nativeRun.conclusion = "failure";
    const result = await maintenance(options);
    expect(result).toMatchObject({ reconciled: 1, results: [{ released: true, delivery: { claims: { h1: { state: "delivery_failed", effects: "unknown" } } } }] });
    const operations = fixture.transactions.flatMap(commit => commit.operations);
    expect(operations.filter(operation => operation.kind === "DeliveryFailure")).toHaveLength(1);
    expect(operations.some(operation => operation.kind === "Result")).toBe(false);
    expect(fixture.state.works.get(member.work_id)).toMatchObject({ barrier: "failed", disposition: "unknown" });
    const transactions = fixture.transactions.length;
    expect(await maintenance(options)).toMatchObject({ reconciled: 0 });
    expect(fixture.transactions).toHaveLength(transactions);
    expect(options.githubClient.rest.actions.listJobsForWorkflowRunAttempt).toHaveBeenCalledTimes(1);
  });

  it("preserves released capacity and reports unresolved delivery when finalization throws", async () => {
    const { fixture, options } = setup({ count: 1 });
    const member = fixture.assignment.claims[0];
    fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: member.handle, outcome: "completed" }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: member.handle });
    await main(options);
    options.context = { ...fixture.dispatcherContext, job: "work_queue_compaction" };
    const publisher = fixture.nativeRun(fixture.dispatcherContext);
    options.workflowRef = `${REPOSITORY}/${publisher.path}@${REF}`;
    options.githubClient.rest.actions.getWorkflowRun.mockResolvedValue({ status: 200, data: publisher });

    const finalizer = require("./finish_work_queue_claim.cjs");
    const finalize = vi.spyOn(finalizer, "finalizeWorkerResults").mockRejectedValue(new Error("ledger unavailable"));
    try {
      const result = await reconcileQueue({ ...options, expireUnverified: true });
      expect(result.results[0]).toMatchObject({
        state: "released",
        released: true,
        delivery: { claims: { h1: { state: "pending", reason: "verification_unresolved" } } },
      });
      const extraWork = { ...fixture.transactions[1].operations[0], graph_id: "extra", node_key: "extra", work_id: require("./work_queue_graph.cjs").nodeId("extra", "extra") };
      fixture.append("submit", { nodes: [extraWork] }, { role: "producer", principal: "11", repository: REPOSITORY });
      fixture.append(
        "cancel_work",
        { operations: [{ kind: "WorkCancellation", work_id: extraWork.work_id, reason: "attempts_exhausted" }] },
        {
          role: "reconciler",
          principal: "11",
          repository: REPOSITORY,
          workflow: publisher.path,
          run_id: String(publisher.id),
          run_attempt: 1,
        }
      );
      const configuration = vi.spyOn(require("./cancel_work_queue_closed_issue.cjs"), "readIssuesConfig").mockResolvedValue({ label: "work" });
      const projection = vi.spyOn(require("./work_queue_issues.cjs"), "main").mockResolvedValue({ pending: [{ reason: "mirror pending" }] });
      try {
        await expect(maintenance({ ...options, projectIssues: true, deferIssueFailure: true })).rejects.toThrow("work_queue_maintenance_reconciliation_unresolved");
        expect(projection).toHaveBeenCalledOnce();
      } finally {
        configuration.mockRestore();
        projection.mockRestore();
      }
      expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).released).toBe(true);
    } finally {
      finalize.mockRestore();
    }
  });

  it.each(["active", "deadline", "unavailable", "principal", "revision"])("keeps a released pending delivery unresolved for %s evidence", async kind => {
    const { fixture, options, nativeRun } = setup({ count: 1 });
    const member = fixture.assignment.claims[0];
    fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: member.handle, outcome: "completed" }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: member.handle });
    await main(options);
    options.context = { ...fixture.dispatcherContext, job: "work_queue_compaction" };
    const publisher = fixture.nativeRun(fixture.dispatcherContext);
    options.workflowRef = `${REPOSITORY}/${publisher.path}@${REF}`;
    options.githubClient.rest.actions.getWorkflowRun.mockResolvedValue({ status: 200, data: publisher });
    options.now = fixture.at + fixture.policy.pools.default.reconciliation.deadline_ms - (kind === "deadline" ? 2 : 0);
    if (kind !== "active") {
      nativeRun.status = "completed";
      nativeRun.conclusion = "success";
    }
    if (kind === "principal") nativeRun.actor = { id: "99" };
    if (kind === "revision") nativeRun.head_sha = "b".repeat(40);
    if (kind === "unavailable") options.githubClient.rest.actions.getWorkflowRunAttempt.mockRejectedValue(new Error("Unavailable"));
    if (["unavailable", "principal", "revision"].includes(kind)) await expect(maintenance(options)).rejects.toThrow("work_queue_maintenance_reconciliation_unresolved");
    else expect(await maintenance(options)).toMatchObject({ reconciled: 1, results: [{ released: true, delivery: { claims: { h1: { state: "pending" } } } }] });
    expect(fixture.state.works.get(member.work_id)).toMatchObject({ barrier: "pending" });
    expect(fixture.transactions.flatMap(commit => commit.operations).some(operation => ["Result", "DeliveryFailure"].includes(operation.kind))).toBe(false);
  });

  it("does not read native runs when no reservations are held", async () => {
    const { options } = setup({ granted: false, bound: false });
    await expect(maintenance(options)).resolves.toMatchObject({ reconciled: 0, results: [] });
    expect(options.githubClient.rest.actions.getWorkflowRun).not.toHaveBeenCalled();
    expect(options.githubClient.rest.actions.listJobsForWorkflowRunAttempt).not.toHaveBeenCalled();
    expect(options.publishWorkQueueRequest).not.toHaveBeenCalled();
  });

  it.each([true, false])("releases hard-cancelled reservations only when terminal=%s", async terminal => {
    const { fixture, options, nativeRun } = setup();
    options.context = { ...fixture.dispatcherContext, job: "work_queue_compaction" };
    const publisher = fixture.nativeRun(fixture.dispatcherContext);
    options.workflowRef = `${REPOSITORY}/${publisher.path}@${REF}`;
    options.githubClient.rest.actions.getWorkflowRun.mockResolvedValue({ status: 200, data: publisher });
    if (terminal) {
      nativeRun.status = "completed";
      nativeRun.conclusion = "cancelled";
    }
    const result = await maintenance(options);
    expect(result.results[0].released).toBe(terminal);
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).released).toBe(terminal);
    expect(options.githubClient.rest.actions.listJobsForWorkflowRunAttempt).not.toHaveBeenCalled();
  });

  it.each([true, false])("recovers a renamed worker's missed binding without releasing active execution when terminal=%s", async terminal => {
    const { fixture, options, nativeRun } = setup({ bound: false, started: true });
    options.context = { ...fixture.dispatcherContext, job: "work_queue_compaction" };
    const publisher = fixture.nativeRun(fixture.dispatcherContext);
    options.workflowRef = `${REPOSITORY}/${publisher.path}@${REF}`;
    options.githubClient.rest.actions.getWorkflowRun.mockResolvedValue({ status: 200, data: publisher });
    nativeRun.name = "Renamed worker * + [special]";
    if (terminal) {
      nativeRun.status = "completed";
      nativeRun.conclusion = "cancelled";
    }
    options.githubClient.rest.actions.listWorkflowRuns = vi.fn().mockResolvedValue({ status: 200, data: { workflow_runs: [nativeRun] } });
    const result = await maintenance(options);
    expect(result.results[0].released).toBe(terminal);
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id)).toMatchObject({ run: { run_id: "42", run_attempt: 1 }, released: terminal });
    expect(options.githubClient.rest.actions.listWorkflowRuns).toHaveBeenCalledWith(expect.objectContaining({ workflow_id: WORKFLOW, event: "workflow_dispatch", head_sha: REF }));
    expect(options.githubClient.rest.actions.getWorkflowRunAttempt).toHaveBeenCalledWith(expect.objectContaining({ run_id: "42", attempt_number: 1 }));
    expect(options.githubClient.rest.actions.listJobsForWorkflowRunAttempt).not.toHaveBeenCalled();
  });

  it("reports unresolved native reads without releasing or cancelling the run", async () => {
    const { options } = setup();
    options.githubClient.rest.actions.getWorkflowRunAttempt.mockRejectedValue(new Error("private API error"));
    await expect(maintenance(options)).rejects.toThrow("work_queue_maintenance_reconciliation_unresolved");
    expect(options.publishWorkQueueRequest).not.toHaveBeenCalled();
  });

  it("reports ambiguous run discovery instead of silently retaining capacity", async () => {
    const { fixture, options, nativeRun } = setup({ bound: false, started: true });
    options.context = { ...fixture.dispatcherContext, job: "work_queue_compaction" };
    const publisher = fixture.nativeRun(fixture.dispatcherContext);
    options.workflowRef = `${REPOSITORY}/${publisher.path}@${REF}`;
    options.githubClient.rest.actions.getWorkflowRun.mockResolvedValue({ status: 200, data: publisher });
    options.githubClient.rest.actions.getWorkflowRunAttempt.mockImplementation(async ({ run_id }) => ({ status: 200, data: { ...nativeRun, id: run_id } }));
    options.githubClient.rest.actions.listWorkflowRuns = vi.fn().mockResolvedValue({ status: 200, data: { workflow_runs: [nativeRun, { ...nativeRun, id: "43" }] } });
    await expect(maintenance(options)).rejects.toThrow("work_queue_maintenance_reconciliation_unresolved");
    expect(options.publishWorkQueueRequest).not.toHaveBeenCalled();
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).released).toBe(false);
  });
});

describe("privacy-preserving termination logging", () => {
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

  it.each(["", "work-queue:other", "*,-work-queue:run"])("honors DEBUG=%s", async pattern => {
    vi.stubEnv("DEBUG", pattern);
    const { options } = setup();
    if (pattern.startsWith("*")) options.context.eventName = "issues";
    await main(options);
    expect(output()).not.toContain("[work-queue:run]");
    if (!pattern) expect(output()).toBe("");
  });

  it("emits lifecycle counts and flags without payloads or identities", async () => {
    const canary = "private-payload-token-should-not-be-logged";
    const { fixture, nativeRun, jobs, options } = setup({
      configureWork: work => {
        work.payload.plan = canary;
      },
    });
    options.context.payload.inputs.token = canary;
    nativeRun.display_title += ` ${canary}`;
    jobs[1].name = options.jobNames.agent = canary;
    await main(options);
    expect(output()).toContain("run.binding.checked candidates=1");
    expect(output()).toContain("run.jobs.quiescent jobs=3");
    expect(output()).toContain("run.reconcile.complete released=true claims=2");
    for (const sensitive of [canary, fixture.assignment.dispatch_id, fixture.assignment.claims[0].work_id, fixture.assignment.claims[0].handle, REF, REPOSITORY]) expect(output()).not.toContain(sensitive);
  });

  it("rejects duplicate bindings without exposing native identities", async () => {
    const { fixture, options } = setup();
    const state = fixture.state;
    state.dispatches.set("private-duplicate-binding", structuredClone(state.dispatches.get(fixture.assignment.dispatch_id)));
    options.readWorkQueueLog.mockResolvedValue({ sha: "head", transactions: fixture.transactions, state });
    await expect(main(options)).rejects.toThrow("work_queue_run_binding_ambiguous");
    expect(output()).toContain("run.binding.checked candidates=2");
    expect(output()).toContain("run.binding.ambiguous");
    expect(output()).not.toContain("private-duplicate-binding");
    expect(output()).not.toContain(fixture.assignment.dispatch_id);
    expect(options.publishWorkQueueRequest).not.toHaveBeenCalled();
  });

  it("sanitizes API errors without swallowing the failure", async () => {
    const canary = "private-error-body-token-and-stack";
    const error = Object.assign(new Error(canary, { cause: new Error(canary) }), { status: 503, response: { data: { message: canary }, headers: { authorization: canary } } });
    const { options } = setup();
    options.githubClient.rest.actions.listJobsForWorkflowRunAttempt.mockRejectedValue(error);
    await expect(main(options)).rejects.toBe(error);
    expect(output()).toContain("run.reconcile.failed failed=true http_status=503 response_present=true response_headers_present=true github_request_id_present=false queue_control_github_api_unavailable=true");
    expect(output()).not.toContain(canary);
    expect(output()).not.toContain("Error:");
  });
});
