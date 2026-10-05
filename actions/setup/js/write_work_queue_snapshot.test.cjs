// @ts-check
import { afterEach, describe, expect, it } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { main, resolveWorkerAssignment } from "./write_work_queue_snapshot.cjs";
import { replayTransactions } from "./work_queue_replay.cjs";

const tempDirectories = [];

afterEach(() => {
  for (const directory of tempDirectories.splice(0)) fs.rmSync(directory, { recursive: true, force: true });
});

describe("write work queue activation snapshot", () => {
  it("writes the Git-backed log and head in an artifact-ready envelope", async () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "work-queue-activation-"));
    tempDirectories.push(directory);
    const snapshotPath = path.join(directory, "snapshot.json");
    const githubClient = {
      rest: {
        git: {
          getRef: async () => {
            throw Object.assign(new Error("Not Found"), { status: 404 });
          },
        },
      },
    };
    const messages = [];

    await main({
      githubClient,
      context: { repo: { owner: "owner", repo: "repo" } },
      snapshotPath,
      core: { info: message => messages.push(message) },
    });

    expect(JSON.parse(fs.readFileSync(snapshotPath, "utf8"))).toEqual({
      version: 2,
      sha: null,
      transactionLog: "",
      worker: null,
    });
    expect(messages).toEqual(["Work queue: reading queue branch", "Work queue: queue branch does not exist", "Work queue: worker assignment absent", "Captured work queue snapshot (0 transactions)"]);
    expect(fs.statSync(snapshotPath).mode & 0o777).toBe(0o444);
  });

  it("snapshots a legacy log without requiring write access", async () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "work-queue-activation-"));
    tempDirectories.push(directory);
    const snapshotPath = path.join(directory, "snapshot.json");
    const legacy = { kind: "Work", work: "w", claim: null, attempt: null };
    const githubClient = {
      rest: {
        git: {
          getRef: async ({ ref }) => {
            if (ref !== "heads/work-queue") throw Object.assign(new Error("Not Found"), { status: 404 });
            return { data: { object: { sha: "legacy-head" } } };
          },
          getCommit: async () => ({ data: { tree: { sha: "tree" } } }),
          getTree: async () => ({ data: { tree: [{ path: "work-queue.jsonl", type: "blob", sha: "blob" }] } }),
          getBlob: async () => ({ data: { encoding: "base64", content: Buffer.from(`${JSON.stringify(legacy)}\n`).toString("base64") } }),
        },
      },
    };
    await main({ githubClient, context: { repo: { owner: "owner", repo: "repo" } }, snapshotPath, core: { info: () => {} } });
    const snapshot = JSON.parse(fs.readFileSync(snapshotPath, "utf8"));
    expect(snapshot.sha).toBe("legacy-head");
    expect(snapshot.version).toBe(2);
    expect(snapshot.transactionLog).toBe(`${JSON.stringify({ version: 2, ...legacy })}\n`);
  });

  it("admits only a trusted work_queue_claim input that is the current effective claim", () => {
    const transactions = [
      { version: 2, kind: "Work", work: "w", claim: null, attempt: null },
      { version: 2, kind: "Claim", work: "w", claim: "claim-b", attempt: null },
      { version: 2, kind: "Claim", work: "w", claim: "claim-a", attempt: null },
    ];
    const payload = {
      inputs: {
        aw_context: JSON.stringify({ repo: "owner/repo", run_id: "1", workflow_id: "dispatcher" }),
        work_queue_claim: JSON.stringify({ work_id: "w", claim_id: "claim-a", work: { input: "trusted" } }),
      },
    };

    expect(resolveWorkerAssignment(payload, transactions)).toEqual({ work_id: "w", claim_id: "claim-a" });
    expect(() => resolveWorkerAssignment({ ...payload, inputs: { work_queue_claim: JSON.stringify({ work_id: "w", claim_id: "claim-b", work: {} }) } }, transactions)).toThrow(/not currently effective/);
    expect(replayTransactions(transactions).winner.w).toBe("claim-a");
  });

  it("reads work_queue from repository_dispatch context and rejects malformed assignments", () => {
    const transactions = [
      { version: 2, kind: "Work", work: "w", claim: null, attempt: null },
      { version: 2, kind: "Claim", work: "w", claim: "c", attempt: null },
    ];
    const assignment = { work_id: "w", claim_id: "c", work: { task: "test" } };
    expect(resolveWorkerAssignment({ client_payload: { aw_context: { work_queue: assignment } } }, transactions)).toEqual({ work_id: "w", claim_id: "c" });
    expect(resolveWorkerAssignment({ inputs: { work_queue_claim: JSON.stringify(assignment) } }, transactions)).toEqual({ work_id: "w", claim_id: "c" });
    expect(resolveWorkerAssignment({ inputs: { aw_context: "{}" } }, transactions)).toBeNull();
    for (const work_queue of [null, [], "claim", { ...assignment, work_id: "" }, { ...assignment, claim_id: "" }, { ...assignment, work: [] }, { ...assignment, extra: true }]) {
      expect(() => resolveWorkerAssignment({ client_payload: { aw_context: { work_queue } } }, transactions)).toThrow(/invalid shape/);
    }
    expect(() => resolveWorkerAssignment({ client_payload: { aw_context: { work_queue: assignment, work_claim: assignment } } }, transactions)).toThrow(/cannot contain both/);
  });
});
