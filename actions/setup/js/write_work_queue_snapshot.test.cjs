// @ts-check
import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import os from "node:os";
import path from "path";
import { main } from "./write_work_queue_snapshot.cjs";
import { readClaimScopeContext } from "./work_queue_claim_scope.cjs";
import { readWorkQueueLog } from "./work_queue_store.cjs";
import { dispatcher, fakeQueueGitHub, ledgerText, release } from "./work_queue_test_helpers.cjs";
import { workerContext, workerQueue } from "./work_queue_worker.test_helpers.cjs";

const directories = [];

function snapshotPath() {
  const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-queue-snapshot-"));
  directories.push(directory);
  return path.join(directory, "snapshot.json");
}

function core() {
  return { info: vi.fn(), warning: vi.fn(), setOutput: vi.fn() };
}

function worker(queueOptions = {}) {
  const queue = workerQueue({ bound: false, ...queueOptions });
  vi.stubEnv("GITHUB_WORKFLOW_REF", queue.workflowRef);
  vi.stubEnv("GH_AW_WORK_QUEUE_ROLE", "worker");
  return queue;
}

async function ledger(githubClient) {
  return readWorkQueueLog({ githubClient, owner: "owner", repo: "repo" });
}

afterEach(() => {
  vi.unstubAllEnvs();
  for (const directory of directories.splice(0)) fs.rmSync(directory, { recursive: true, force: true });
});

describe("worker activation snapshot", () => {
  it("binds the Dispatch once and a rerun of the same attempt is a no-op", async () => {
    const queue = worker();
    const options = { githubClient: queue.githubClient, context: queue.context, core: core(), role: "worker", snapshotPath: snapshotPath() };
    const snapshot = await main(options);
    const bound = await ledger(queue.githubClient);
    const binds = bound.transactions.filter(commit => commit.request.kind === "bind");
    expect(binds).toHaveLength(1);
    expect(binds[0].operations).toEqual([{ kind: "Bound", dispatch_id: queue.dispatchId, run_id: "200", run_attempt: 1 }]);
    expect(bound.state.dispatches.get(queue.dispatchId)).toMatchObject({ state: "bound", run: { run_id: "200", run_attempt: 1 } });
    expect(snapshot).toMatchObject({ version: 4, sha: bound.sha, role: "worker", worker: queue.assignment, origin: { role: "worker", run_id: "200", run_attempt: 1 } });
    expect(snapshot).not.toHaveProperty("captured_at");
    expect(options.core.setOutput).toHaveBeenCalledWith("work_queue_assignment_settled", "false");

    await main({ ...options, snapshotPath: snapshotPath() });
    const rerun = await ledger(queue.githubClient);
    expect(rerun.sha).toBe(bound.sha);
    expect(rerun.transactions.filter(commit => commit.request.kind === "bind")).toHaveLength(1);
  });

  it("rebinds a later attempt of the same run", async () => {
    const queue = worker({ bound: true });
    await main({ githubClient: queue.githubClient, context: workerContext(queue.dispatchId, "200", 2), core: core(), role: "worker", snapshotPath: snapshotPath() });
    expect((await ledger(queue.githubClient)).state.dispatches.get(queue.dispatchId).run).toEqual({ run_id: "200", run_attempt: 2 });
  });

  it("writes worker:null and the settled output when every Claim is already closed", async () => {
    const queue = worker();
    queue.fake.install(ledgerText(release(queue.log, queue.dispatchId, "run_missing")));
    const coreApi = core();
    const snapshot = await main({ githubClient: queue.githubClient, context: queue.context, core: coreApi, role: "worker", snapshotPath: snapshotPath() });
    expect(snapshot.worker).toBeNull();
    expect(coreApi.setOutput).toHaveBeenCalledWith("work_queue_assignment_settled", "true");
    expect((await ledger(queue.githubClient)).transactions.some(commit => commit.request.kind === "bind")).toBe(false);
  });

  it("fails for a Dispatch that is not in the ledger", async () => {
    const queue = worker();
    await expect(main({ githubClient: queue.githubClient, context: workerContext("d_unknown"), core: core(), role: "worker", snapshotPath: snapshotPath() })).rejects.toThrow(/work_queue_assignment_unknown/);
  });

  it("produces an envelope the claim scope reader accepts and checks against the run's dispatch", async () => {
    const queue = worker();
    const file = snapshotPath();
    await main({ githubClient: queue.githubClient, context: queue.context, core: core(), role: "worker", snapshotPath: file });
    vi.stubEnv("GH_AW_WORK_QUEUE_ENABLED", "true");
    vi.stubEnv("GH_AW_WORK_QUEUE_SNAPSHOT", file);
    const previous = global.context;
    try {
      global.context = queue.context;
      expect(readClaimScopeContext()?.assignment).toEqual(queue.assignment);
      global.context = workerContext("d_other");
      expect(() => readClaimScopeContext()).toThrow(/does not match/);
    } finally {
      global.context = previous;
    }
  });
});

describe("nonworker activation snapshot", () => {
  it("captures a read-only dispatcher snapshot without writing", async () => {
    const queue = workerQueue({ bound: false });
    vi.stubEnv("GITHUB_WORKFLOW_REF", `owner/repo/${dispatcher.workflow}@refs/heads/main`);
    const before = await ledger(queue.githubClient);
    const snapshot = await main({ githubClient: queue.githubClient, context: { repo: { owner: "owner", repo: "repo" }, runId: 100, runAttempt: 1, payload: {} }, core: core(), snapshotPath: snapshotPath() });
    expect(snapshot).toMatchObject({ version: 4, sha: before.sha, role: "dispatcher", worker: null, origin: { role: "dispatcher" } });
    expect((await ledger(queue.githubClient)).sha).toBe(before.sha);
  });

  it("captures an absent queue for an observer", async () => {
    const fake = fakeQueueGitHub();
    const snapshot = await main({ githubClient: fake.githubClient, context: { repo: { owner: "owner", repo: "repo" }, payload: {} }, core: core(), role: "observer", snapshotPath: snapshotPath() });
    expect(snapshot).toMatchObject({ version: 4, sha: null, transactionLog: "", role: "observer", worker: null });
  });

  it("rejects an observer that was handed an assignment", async () => {
    const queue = workerQueue({ bound: false });
    await expect(main({ githubClient: queue.githubClient, context: queue.context, core: core(), role: "observer", snapshotPath: snapshotPath() })).rejects.toThrow(/observer_assignment_forbidden/);
  });
});
