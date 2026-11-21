// @ts-check
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { authorizeWorkerClaim, main, readClaimEffects } from "./finish_work_queue_claim.cjs";
import { readWorkQueueLog } from "./work_queue_store.cjs";
import { workerContext, workerPolicy, workerQueue } from "./work_queue_worker.test_helpers.cjs";
import { ledgerText, release } from "./work_queue_test_helpers.cjs";

const directories = [];

function tempFile(name) {
  const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-queue-finish-"));
  directories.push(directory);
  return path.join(directory, name);
}

function core() {
  return { info: vi.fn(), warning: vi.fn(), setOutput: vi.fn(), summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue(undefined) } };
}

function setup(queueOptions = {}) {
  const queue = workerQueue(queueOptions);
  vi.stubEnv("GITHUB_WORKFLOW_REF", queue.workflowRef);
  const intents = tempFile("finish.jsonl");
  const effects = tempFile("effects.jsonl");
  const stage = (handle, outcome) => fs.appendFileSync(intents, `${JSON.stringify({ version: 4, claim_handle: handle, outcome })}\n`);
  return { queue, intents, effects, stage, core: core() };
}

async function ledger(queue) {
  return readWorkQueueLog({ githubClient: queue.githubClient, owner: "owner", repo: "repo" });
}

function lastCommit(current) {
  return current.transactions[current.transactions.length - 1];
}

beforeEach(() => {
  vi.stubEnv("GH_AW_PROMPTS_DIR", path.resolve(import.meta.dirname, "../md"));
  vi.stubEnv("GH_AW_SAFE_OUTPUTS_STAGED", "");
});

afterEach(() => {
  vi.unstubAllEnvs();
  for (const directory of directories.splice(0)) fs.rmSync(directory, { recursive: true, force: true });
});

describe("finish step", () => {
  it("publishes one Completion carrying the recorded effects for a completed intent", async () => {
    const { queue, intents, effects, stage, core: coreApi } = setup();
    stage("h1", "completed");
    fs.writeFileSync(effects, `${JSON.stringify({ claim_handle: "h1", kind: "add_comment", resource: { repository: "owner/repo", number: 7, id: "99" } })}\n`);
    const result = await main({ githubClient: queue.githubClient, context: queue.context, core: coreApi, policy: queue.policy, finishIntentPath: intents, effectsPath: effects });
    expect(result).toMatchObject({ dispatch_id: queue.dispatchId, outcome: "completed", committed: true });
    const current = await ledger(queue);
    const commit = lastCommit(current);
    expect(commit.request).toMatchObject({ kind: "finish", parameters: { dispatch_id: queue.dispatchId } });
    expect(commit.actor).toMatchObject({ role: "worker", run_id: "200", run_attempt: 1 });
    expect(commit.operations).toEqual([
      {
        kind: "Completion",
        work_id: queue.assignment.claims[0].work_id,
        claim_id: queue.assignment.claims[0].claim_id,
        dispatch_id: queue.dispatchId,
        claim_handle: "h1",
        effects: [{ kind: "add_comment", resource: { repository: "owner/repo", number: 7, id: "99" } }],
      },
    ]);
    expect(current.state.works.get(queue.assignment.claims[0].work_id).state).toBe("completed");
    expect(current.state.dispatches.get(queue.dispatchId).state).toBe("closed");
    expect(coreApi.setOutput).toHaveBeenCalledWith("work_queue_outcome", "completed");
  });

  it("cancels a Claim without a finish intent and returns its Work to the pool", async () => {
    const { queue, intents, effects, core: coreApi } = setup();
    await main({ githubClient: queue.githubClient, context: queue.context, core: coreApi, policy: queue.policy, finishIntentPath: intents, effectsPath: effects });
    const current = await ledger(queue);
    expect(lastCommit(current).operations).toEqual([{ kind: "ClaimCancellation", work_id: queue.assignment.claims[0].work_id, claim_id: queue.assignment.claims[0].claim_id, reason: "worker_failed" }]);
    expect(current.state.works.get(queue.assignment.claims[0].work_id)).toMatchObject({ state: "available", attempts: 1 });
    expect(coreApi.setOutput).toHaveBeenCalledWith("work_queue_outcome", "failed");
  });

  it("cancels the Work after a failed intent on its last attempt", async () => {
    const { queue, intents, effects, stage, core: coreApi } = setup();
    stage("h1", "failed");
    await main({ githubClient: queue.githubClient, context: queue.context, core: coreApi, policy: workerPolicy({ maxAttempts: 1 }), finishIntentPath: intents, effectsPath: effects });
    const current = await ledger(queue);
    const claim = queue.assignment.claims[0];
    expect(lastCommit(current).operations).toEqual([
      { kind: "ClaimCancellation", work_id: claim.work_id, claim_id: claim.claim_id, reason: "worker_failed" },
      { kind: "WorkCancellation", work_id: claim.work_id, reason: "attempts_exhausted", claim_id: claim.claim_id },
    ]);
    expect(current.state.works.get(claim.work_id)).toMatchObject({ state: "cancelled", cancellation_reason: "attempts_exhausted" });
    expect(coreApi.setOutput).toHaveBeenCalledWith("work_queue_outcome", "failed");
  });

  it("drops effects above the Completion bound with a warning", async () => {
    const { queue, intents, effects, stage, core: coreApi } = setup();
    stage("h1", "completed");
    const line = `${JSON.stringify({ claim_handle: "h1", kind: "add_comment", resource: { repository: "owner/repo", body: "x".repeat(5000) } })}\n`;
    fs.writeFileSync(effects, line);
    await main({ githubClient: queue.githubClient, context: queue.context, core: coreApi, policy: queue.policy, finishIntentPath: intents, effectsPath: effects });
    expect(lastCommit(await ledger(queue)).operations[0]).toMatchObject({ kind: "Completion", effects: [] });
    expect(coreApi.warning).toHaveBeenCalledWith(expect.stringContaining("exceed 4096 bytes"));
  });

  it("commits nothing when maintenance already released the Dispatch", async () => {
    const { queue, intents, effects, stage, core: coreApi } = setup();
    queue.fake.install(ledgerText(release(queue.log, queue.dispatchId, "run_missing")));
    stage("h1", "completed");
    const result = await main({ githubClient: queue.githubClient, context: queue.context, core: coreApi, policy: queue.policy, finishIntentPath: intents, effectsPath: effects });
    expect(result).toMatchObject({ outcome: "failed", committed: false });
    expect(lastCommit(await ledger(queue)).request.kind).toBe("release");
  });

  it("skips runs without an assignment", async () => {
    const coreApi = core();
    expect(await main({ githubClient: {}, context: { repo: { owner: "owner", repo: "repo" }, payload: {} }, core: coreApi })).toBeNull();
    expect(coreApi.setOutput).not.toHaveBeenCalled();
  });

  it("groups effects-file lines by Claim handle", () => {
    const file = tempFile("effects.jsonl");
    fs.writeFileSync(file, ['{"claim_handle":"h1","kind":"create_issue","resource":{"repository":"owner/repo","number":1}}', "", '{"claim_handle":"h2","kind":"add_comment","resource":{"repository":"owner/repo"}}'].join("\n"));
    expect(Object.fromEntries(readClaimEffects(file))).toEqual({
      h1: [{ kind: "create_issue", resource: { repository: "owner/repo", number: 1 } }],
      h2: [{ kind: "add_comment", resource: { repository: "owner/repo" } }],
    });
    expect(readClaimEffects(tempFile("missing.jsonl")).size).toBe(0);
  });
});

describe("authorizeWorkerClaim", () => {
  function authorize(queue, overrides = {}) {
    return authorizeWorkerClaim({ githubClient: queue.githubClient, context: queue.context, assignment: queue.assignment, claim_handle: "h1", policy: queue.policy, finishIntents: new Map([["h1", "completed"]]), ...overrides });
  }

  it("authorizes an open Claim bound to this run attempt with a completed intent", async () => {
    const { queue } = setup();
    await expect(authorize(queue, { resource: { repository: "owner/repo", number: "7" } })).resolves.toMatchObject({ claim_handle: "h1", authorized: true, state: "open", effect_scope: "owner/repo" });
  });

  it("suppresses outputs when the finish intent is missing or failed", async () => {
    const { queue } = setup();
    await expect(authorize(queue, { finishIntents: new Map() })).resolves.toMatchObject({ authorized: false, suppressed: true, state: "failed" });
    await expect(authorize(queue, { finishIntents: new Map([["h1", "failed"]]) })).resolves.toMatchObject({ authorized: false, suppressed: true, state: "failed" });
    await expect(authorize(queue, { finishIntents: new Map(), requireCompletion: false })).resolves.toMatchObject({ authorized: true });
  });

  it("rejects another run attempt of the bound run", async () => {
    const { queue } = setup();
    await expect(authorize(queue, { context: workerContext(queue.dispatchId, "200", 2) })).resolves.toMatchObject({ authorized: false, state: "unbound" });
    await expect(authorize(queue, { context: workerContext(queue.dispatchId, "201", 1) })).resolves.toMatchObject({ authorized: false, state: "unbound" });
  });

  it("rejects an unbound Dispatch", async () => {
    const { queue } = setup({ bound: false });
    await expect(authorize(queue)).resolves.toMatchObject({ authorized: false, state: "unbound" });
  });

  it("rejects a foreign repository with E004", async () => {
    const { queue } = setup();
    await expect(authorize(queue, { resource: { repository: "other/repo" } })).rejects.toMatchObject({ code: "E004" });
    await expect(authorize(queue, { message: { type: "add_comment", claim_handle: "h1", repo: "other/repo" } })).rejects.toMatchObject({ code: "E004" });
    await expect(authorize(queue, { policy: workerPolicy({ effectScope: "other/repo" }), resource: { repository: "owner/repo" } })).rejects.toMatchObject({ code: "E004" });
  });

  it("suppresses a Claim that is no longer open", async () => {
    const { queue } = setup();
    queue.fake.install(ledgerText(release(queue.log, queue.dispatchId, "run_missing")));
    await expect(authorize(queue)).resolves.toMatchObject({ authorized: false, suppressed: true, state: "cancelled" });
  });
});
