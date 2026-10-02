// @ts-check
import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { applyTransactions } from "./dispatch_work_coordinator_replay.cjs";
import { main, readFinishIntent, reconcileWorkerClaim, renderSummary } from "./finish_dispatch_work_claim.cjs";

const worker = { work_id: "w", claim_id: "claim-a" };
const initialTransactions = [
  { version: 1, kind: "Work", work: "w", claim: null, attempt: null },
  { version: 1, kind: "Claim", work: "w", claim: "claim-a", attempt: null },
];

let tempDirectory;

function setup(transactions = initialTransactions) {
  tempDirectory = fs.mkdtempSync(path.join(os.tmpdir(), "dispatch-claim-finish-"));
  const finishIntentPath = path.join(tempDirectory, "finish.jsonl");
  let currentTransactions = [...transactions];
  const readCoordinatorLog = async () => ({ sha: "head", transactions: currentTransactions });
  const applyAndPublish = async ({ intents }) => {
    const result = applyTransactions(currentTransactions, intents);
    currentTransactions = result.transactions;
    return result;
  };
  return {
    finishIntentPath,
    readCoordinatorLog,
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

describe("dispatch work claim reconciliation", () => {
  it("publishes and verifies Completion before authorizing safe outputs", async () => {
    const fake = setup();
    fs.writeFileSync(fake.finishIntentPath, '{"outcome":"completed"}\n');
    vi.stubEnv("GITHUB_RUN_ATTEMPT", "2");
    vi.stubEnv("GITHUB_WORKFLOW_REF", "owner/repo/.github/workflows/worker.yml@refs/heads/main");

    const result = await reconcileWorkerClaim({
      worker,
      finishIntentPath: fake.finishIntentPath,
      readCoordinatorLog: fake.readCoordinatorLog,
      applyAndPublish: fake.applyAndPublish,
      context: { repo: { owner: "owner", repo: "repo" }, runId: 123 },
    });

    expect(result).toEqual({ authorized: true, status: "completed" });
    expect(fake.transactions).toContainEqual({
      version: 1,
      kind: "Completion",
      work: "w",
      claim: "claim-a",
      attempt: "123-2:owner/repo/.github/workflows/worker.yml@refs/heads/main",
    });
  });

  it("cancels an effective claim when no finish intent exists and blocks effects", async () => {
    const fake = setup();
    const result = await reconcileWorkerClaim({
      worker,
      finishIntentPath: fake.finishIntentPath,
      readCoordinatorLog: fake.readCoordinatorLog,
      applyAndPublish: fake.applyAndPublish,
      context: { repo: { owner: "owner", repo: "repo" }, runId: 123 },
    });

    expect(result).toEqual({ authorized: false, status: "cancelled" });
    expect(fake.transactions).toContainEqual({ version: 1, kind: "ClaimCancellation", work: "w", claim: "claim-a", attempt: null });
  });

  it("does not publish or authorize a superseded claim", async () => {
    const fake = setup([...initialTransactions, { version: 1, kind: "Claim", work: "w", claim: "claim-0", attempt: null }]);
    const publish = vi.fn(fake.applyAndPublish);
    const result = await reconcileWorkerClaim({
      worker,
      finishIntentPath: fake.finishIntentPath,
      readCoordinatorLog: fake.readCoordinatorLog,
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
      readCoordinatorLog: fake.readCoordinatorLog,
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
        readCoordinatorLog: fake.readCoordinatorLog,
        applyAndPublish: fake.applyAndPublish,
        context: { repo: { owner: "owner", repo: "repo" }, runId: 123 },
        core,
      })
    ).rejects.toThrow(/ordinary safe outputs are blocked/);
    expect(core.setOutput).toHaveBeenCalledWith("authorized", "false");
    expect(summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("<details>"));
  });
});
