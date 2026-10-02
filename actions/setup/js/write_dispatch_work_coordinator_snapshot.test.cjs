// @ts-check
import { afterEach, describe, expect, it } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { main, resolveWorkerAssignment } from "./write_dispatch_work_coordinator_snapshot.cjs";
import { replayTransactions } from "./dispatch_work_coordinator_replay.cjs";

const tempDirectories = [];

afterEach(() => {
  for (const directory of tempDirectories.splice(0)) fs.rmSync(directory, { recursive: true, force: true });
});

describe("write dispatch coordinator activation snapshot", () => {
  it("writes the Git-backed log and head in an artifact-ready envelope", async () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "dispatch-coordinator-activation-"));
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
    expect(messages).toEqual(["Captured dispatch coordinator snapshot (0 transactions)"]);
    expect(fs.statSync(snapshotPath).mode & 0o777).toBe(0o444);
  });

  it("snapshots a legacy log without requiring write access", async () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "dispatch-coordinator-activation-"));
    tempDirectories.push(directory);
    const snapshotPath = path.join(directory, "snapshot.json");
    const legacy = { kind: "Work", work: "w", claim: null, attempt: null };
    const githubClient = {
      rest: {
        git: {
          getRef: async () => ({ data: { object: { sha: "legacy-head" } } }),
          getCommit: async () => ({ data: { tree: { sha: "tree" } } }),
          getTree: async () => ({ data: { tree: [{ path: "dispatch-work-coordinator.jsonl", type: "blob", sha: "blob" }] } }),
          getBlob: async () => ({ data: { encoding: "base64", content: Buffer.from(`${JSON.stringify(legacy)}\n`).toString("base64") } }),
        },
      },
    };
    await main({ githubClient, context: { repo: { owner: "owner", repo: "repo" } }, snapshotPath, core: { info: () => {} } });
    const snapshot = JSON.parse(fs.readFileSync(snapshotPath, "utf8"));
    expect(snapshot.sha).toBe("legacy-head");
    expect(snapshot.transactionLog).toBe(`${JSON.stringify({ version: 1, ...legacy })}\n`);
  });

  it("admits only a trusted inbound assignment that is the current effective claim", () => {
    const transactions = [
      { version: 1, kind: "Work", work: "w", claim: null, attempt: null },
      { version: 1, kind: "Claim", work: "w", claim: "claim-b", attempt: null },
      { version: 1, kind: "Claim", work: "w", claim: "claim-a", attempt: null },
    ];
    const payload = {
      inputs: {
        aw_context: JSON.stringify({
          dispatch_work_coordinator: { work_id: "w", claim_id: "claim-a", work: { input: "trusted" } },
        }),
      },
    };

    expect(resolveWorkerAssignment(payload, transactions)).toEqual({ work_id: "w", claim_id: "claim-a" });
    expect(() => resolveWorkerAssignment({ ...payload, inputs: { aw_context: JSON.stringify({ dispatch_work_coordinator: { work_id: "w", claim_id: "claim-b", work: {} } }) } }, transactions)).toThrow(/not currently effective/);
    expect(replayTransactions(transactions).winner.w).toBe("claim-a");
  });
});
