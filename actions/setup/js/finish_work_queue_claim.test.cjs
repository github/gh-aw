// @ts-check
import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { applyTransactions } from "./work_queue_replay.cjs";
import { main, readFinishIntent, reconcileWorkerClaim, renderSummary } from "./finish_work_queue_claim.cjs";
import { main as writeSnapshot } from "./write_work_queue_snapshot.cjs";
import { serializeTransactionLog } from "./work_queue_replay.cjs";

const worker = { work_id: "w", claim_id: "claim-a" };
const initialTransactions = [
  { version: 3, kind: "Work", work_id: "w", work: { task: "test" }, sequence: 1 },
  { version: 3, kind: "Claim", work_id: "w", claim_id: "claim-a", run_id: "run-a" },
];

let tempDirectory;

function setup(transactions = initialTransactions) {
  tempDirectory = fs.mkdtempSync(path.join(os.tmpdir(), "work-queue-claim-finish-"));
  const finishIntentPath = path.join(tempDirectory, "finish.jsonl");
  let currentTransactions = [...transactions];
  const readWorkQueueLog = async () => ({ sha: "head", transactions: currentTransactions });
  const applyAndPublish = async ({ intents }) => {
    const result = applyTransactions(currentTransactions, intents);
    currentTransactions = result.transactions;
    return result;
  };
  return {
    finishIntentPath,
    readWorkQueueLog,
    applyAndPublish,
    get transactions() {
      return currentTransactions;
    },
  };
}

afterEach(() => {
  vi.unstubAllEnvs();
  if (tempDirectory) fs.rmSync(tempDirectory, { recursive: true, force: true });
});

describe("work queue claim reconciliation", () => {
  it("keeps an in-flight legacy assignment bound through activation and reconciliation", async () => {
    const fake = setup();
    const snapshotPath = path.join(tempDirectory, "snapshot.json");
    const githubClient = {
      rest: {
        git: {
          getRef: async ({ ref }) => {
            if (ref !== "heads/dispatch-coordinator") throw Object.assign(new Error("Not Found"), { status: 404 });
            return { data: { object: { sha: "legacy-head" } } };
          },
          getCommit: async () => ({ data: { tree: { sha: "tree" } } }),
          getTree: async () => ({ data: { tree: [{ path: "work-queue.jsonl", mode: "100644", type: "blob", sha: "blob" }] } }),
          getBlob: async () => ({ data: { encoding: "base64", content: Buffer.from(serializeTransactionLog(initialTransactions)).toString("base64") } }),
        },
      },
    };
    const context = {
      repo: { owner: "owner", repo: "repo" },
      runId: 123,
      payload: { inputs: { aw_context: JSON.stringify({ work_claim: { ...worker, work: { task: "test" } } }) } },
    };
    await writeSnapshot({ githubClient, context, snapshotPath, core: { info: vi.fn() } });
    expect(JSON.parse(fs.readFileSync(snapshotPath, "utf8")).worker).toEqual(worker);
    const result = await reconcileWorkerClaim({
      snapshotPath,
      finishIntentPath: fake.finishIntentPath,
      readWorkQueueLog: fake.readWorkQueueLog,
      applyAndPublish: fake.applyAndPublish,
      context,
    });
    expect(result).toEqual({ authorized: false, status: "cancelled" });
    expect(fake.transactions).toContainEqual({ version: 3, kind: "ClaimCancellation", work_id: "w", claim_id: "claim-a" });
  });

  it("publishes and verifies Completion before authorizing safe outputs", async () => {
    const fake = setup();
    fs.writeFileSync(fake.finishIntentPath, '{"outcome":"completed"}\n');
    vi.stubEnv("GITHUB_RUN_ATTEMPT", "2");
    vi.stubEnv("GITHUB_WORKFLOW_REF", "owner/repo/.github/workflows/worker.yml@refs/heads/main");

    const result = await reconcileWorkerClaim({
      worker,
      finishIntentPath: fake.finishIntentPath,
      readWorkQueueLog: fake.readWorkQueueLog,
      applyAndPublish: fake.applyAndPublish,
      context: { repo: { owner: "owner", repo: "repo" }, runId: 123 },
    });

    expect(result).toEqual({ authorized: true, status: "completed" });
    expect(fake.transactions).toContainEqual({
      version: 3,
      kind: "Completion",
      work_id: "w",
      claim_id: "claim-a",
      attempt_id: "123-2:owner/repo/.github/workflows/worker.yml@refs/heads/main",
    });
  });

  it("cancels an effective claim when no finish intent exists and blocks effects", async () => {
    const fake = setup();
    const result = await reconcileWorkerClaim({
      worker,
      finishIntentPath: fake.finishIntentPath,
      readWorkQueueLog: fake.readWorkQueueLog,
      applyAndPublish: fake.applyAndPublish,
      context: { repo: { owner: "owner", repo: "repo" }, runId: 123 },
    });

    expect(result).toEqual({ authorized: false, status: "cancelled" });
    expect(fake.transactions).toContainEqual({ version: 3, kind: "ClaimCancellation", work_id: "w", claim_id: "claim-a" });
  });

  it("does not publish or authorize a superseded claim", async () => {
    const fake = setup([...initialTransactions, { version: 3, kind: "Claim", work_id: "w", claim_id: "claim-0", run_id: "run-0" }]);
    const publish = vi.fn(fake.applyAndPublish);
    const result = await reconcileWorkerClaim({
      worker,
      finishIntentPath: fake.finishIntentPath,
      readWorkQueueLog: fake.readWorkQueueLog,
      applyAndPublish: publish,
      context: { repo: { owner: "owner", repo: "repo" }, runId: 123 },
    });

    expect(result).toEqual({ authorized: false, status: "superseded" });
    expect(publish).not.toHaveBeenCalled();
    expect(fake.transactions).toHaveLength(3);
  });

  it("allows safe outputs for workflows without a worker assignment", async () => {
    const fake = setup();
    const core = { info: vi.fn() };
    const result = await reconcileWorkerClaim({
      worker: null,
      core,
      readWorkQueueLog: fake.readWorkQueueLog,
      applyAndPublish: fake.applyAndPublish,
      context: { repo: { owner: "owner", repo: "repo" }, runId: 123 },
    });

    expect(result).toEqual({ authorized: true, status: "unassigned" });
    expect(core.info).toHaveBeenCalledWith(expect.stringContaining("no inbound worker claim"));
    expect(fake.transactions).toEqual(initialTransactions);
  });

  it("rejects conflicting finish intents and emits a redacted progressive summary", async () => {
    const fake = setup();
    fs.writeFileSync(fake.finishIntentPath, '{"outcome":"completed"}\n{"outcome":"cancelled"}\n');
    expect(() => readFinishIntent(fake.finishIntentPath)).toThrow(/conflict/);
    fs.writeFileSync(fake.finishIntentPath, '{"outcome":"completed","claim_id":"untrusted"}\n');
    expect(() => readFinishIntent(fake.finishIntentPath)).toThrow(/invalid/);
    fs.writeFileSync(fake.finishIntentPath, '{"outcome":"completed"}\n{"outcome":"cancelled"}\n');
    expect(renderSummary("completed")).toContain("<details>");

    const summary = { addRaw: vi.fn(() => ({ write: vi.fn() })) };
    const core = { setOutput: vi.fn(), info: vi.fn(), summary };
    await expect(
      main({
        worker,
        finishIntentPath: fake.finishIntentPath,
        readWorkQueueLog: fake.readWorkQueueLog,
        applyAndPublish: fake.applyAndPublish,
        context: { repo: { owner: "owner", repo: "repo" }, runId: 123 },
        core,
      })
    ).rejects.toThrow(/ordinary safe outputs are blocked/);
    expect(core.setOutput).toHaveBeenCalledWith("authorized", "false");
    expect(summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("<details>"));
  });

  it("does not publish queue mutations in staged mode", async () => {
    const fake = setup();
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_STAGED", "true");
    fs.writeFileSync(fake.finishIntentPath, '{"outcome":"completed"}\n');
    const publish = vi.fn(fake.applyAndPublish);
    const core = { setOutput: vi.fn(), info: vi.fn(), summary: { addRaw: vi.fn(() => ({ write: vi.fn() })) } };
    expect(
      await main({
        worker,
        finishIntentPath: fake.finishIntentPath,
        applyAndPublish: publish,
        readCoordinatorLog: fake.readCoordinatorLog,
        context: { repo: { owner: "owner", repo: "repo" }, runId: 123 },
        core,
      })
    ).toEqual({ authorized: true, status: "staged" });
    expect(publish).not.toHaveBeenCalled();
    expect(fake.transactions).toEqual(initialTransactions);
  });
});
