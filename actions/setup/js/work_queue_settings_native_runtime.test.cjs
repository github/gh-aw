import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import { afterEach, describe, expect, it, vi } from "vitest";
import { authorizeWorkerClaim, finalizeWorkerResults } from "./finish_work_queue_claim.cjs";
import { readClaimQueueControls } from "./work_queue_control_receipts.cjs";
import { main as writeSnapshot } from "./write_work_queue_snapshot.cjs";
import { compactTransactions, replayTransactions } from "./work_queue_replay.cjs";
import { queueFixture, noWriteClaimVerifier, REF, REPOSITORY, WORKFLOW } from "./work_queue_lifecycle.test_helpers.cjs";

const require = createRequire(import.meta.url);
const directories = [];
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllEnvs();
  for (const directory of directories.splice(0)) fs.rmSync(directory, { recursive: true, force: true });
});

function awFixture() {
  const fixture = queueFixture({
    count: 1,
    batch: 1,
    workerPrincipal: "22",
    configurePolicy: policy => {
      policy.authorization = "aw";
      policy.producers = {};
      delete policy.pools.default.profiles.default.principal;
    },
  });
  fixture.append(
    "dispatch",
    {
      operations: [
        {
          kind: "Dispatch",
          dispatch_id: fixture.assignment.dispatch_id,
          state: "started",
          sender: fixture.dispatcher,
          credential_principal: "22",
        },
      ],
    },
    fixture.dispatcher
  );
  fixture.append(
    "dispatch",
    {
      operations: [
        {
          kind: "Dispatch",
          dispatch_id: fixture.assignment.dispatch_id,
          state: "bound",
          run: fixture.binding,
          evidence: {
            kind: "reconciliation",
            source: "github_api",
            repository: REPOSITORY,
            workflow: WORKFLOW,
            ref: REF,
            principal: "22",
            checked_at: fixture.at,
            run_id: "42",
            run_attempt: 1,
          },
        },
      ],
    },
    fixture.dispatcher
  );
  const options = {
    assignment: fixture.assignment,
    githubClient: fixture.githubClient,
    context: fixture.workerContext,
    workflowRef: `${REPOSITORY}/${WORKFLOW}@${REF}`,
    readWorkQueueLog: fixture.readWorkQueueLog,
    publishWorkQueueRequest: fixture.publishWorkQueueRequest,
    now: fixture.at,
    sleepFn: async () => {},
  };
  return { fixture, options };
}

function complete(fixture) {
  fixture.append(
    "finish",
    {
      dispatch_id: fixture.assignment.dispatch_id,
      claim_handle: "h1",
      outcome: "completed",
    },
    { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1" }
  );
}

describe("AW-managed runtime callers use the durable dispatch credential", () => {
  it("authorizes completed Claims and verifies delivery/readback after checkpointing without a profile principal", async () => {
    const { fixture, options } = awFixture();
    complete(fixture);
    const history = fixture.transactions;
    const checkpoint = compactTransactions(history, "c".repeat(40), fixture.administrator, fixture.at + 1);
    options.readWorkQueueLog = async () => ({ sha: "checkpoint", transactions: checkpoint, state: replayTransactions(checkpoint) });
    expect(await authorizeWorkerClaim({ ...options, claim_handle: "h1" })).toMatchObject({ authorized: true, state: "completed", run_id: "42" });
    expect(await readClaimQueueControls({ ...options, claim_handle: "h1" })).toMatchObject({ verified: true, controls: [] });
    options.readWorkQueueLog = fixture.readWorkQueueLog;
    expect((await finalizeWorkerResults({ ...options, verifyEffects: noWriteClaimVerifier(options) })).claims.h1).toMatchObject({ state: "result", effects: "none" });
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).credential_principal).toBe("22");
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).profile).not.toHaveProperty("principal");
  });

  it("rejects caller-relative native principals instead of treating an omitted profile principal as arbitrary authority", async () => {
    const { fixture, options } = awFixture();
    complete(fixture);
    fixture.workerContext.actorId = "33";
    await expect(authorizeWorkerClaim({ ...options, claim_handle: "h1" })).rejects.toThrow("run_principal_mismatch");
    await expect(readClaimQueueControls({ ...options, claim_handle: "h1" })).rejects.toThrow("run_principal_mismatch");
  });

  it("uses frozen credential authority for staged worker snapshots and rejects native-principal drift", async () => {
    const { fixture, options } = awFixture();
    const directory = fs.mkdtempSync(path.join(process.cwd(), ".gh-aw-principal-snapshot-"));
    directories.push(directory);
    const configured = {
      ...options,
      staged: true,
      snapshotPath: path.join(directory, "snapshot.json"),
      core: { info: vi.fn(), setOutput: vi.fn() },
    };
    vi.stubEnv("GH_AW_WORK_QUEUE_ISSUES", "{}");
    vi.spyOn(require("./work_queue_issues.cjs"), "main").mockResolvedValue(undefined);
    const snapshot = await writeSnapshot(configured);
    expect(snapshot.origin.principal).toBe("22");
    expect(snapshot.worker).toEqual(fixture.assignment);
    fixture.workerContext.actorId = "33";
    await expect(writeSnapshot({ ...configured, snapshotPath: path.join(directory, "rejected.json") })).rejects.toThrow("run_principal_mismatch");
    expect(fs.existsSync(path.join(directory, "rejected.json"))).toBe(false);
  });
});
