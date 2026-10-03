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
        repos: { get: async () => ({ data: {} }) },
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
    expect(messages).toEqual(["Dispatch coordinator: reading queue branch", "Dispatch coordinator: queue branch does not exist", "Dispatch coordinator: worker assignment absent", "Captured dispatch coordinator snapshot (0 transactions)"]);
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
          getTree: async () => ({ data: { tree: [{ path: "dispatch-work-coordinator.jsonl", mode: "100644", type: "blob", sha: "blob" }] } }),
          getBlob: async () => ({ data: { encoding: "base64", content: Buffer.from(`${JSON.stringify(legacy)}\n`).toString("base64") } }),
        },
      },
    };
    await main({ githubClient, context: { repo: { owner: "owner", repo: "repo" } }, snapshotPath, core: { info: () => {} } });
    const snapshot = JSON.parse(fs.readFileSync(snapshotPath, "utf8"));
    expect(snapshot.sha).toBe("legacy-head");
    expect(JSON.parse(snapshot.transactionLog)).toEqual({ version: 3, kind: "Work", work_id: "w", work: { legacy_work_id: "w" }, sequence: 1 });
  });

  it("admits only a trusted inbound assignment that is the current effective claim", () => {
    const transactions = [
      { version: 3, kind: "Work", work_id: "w", work: { input: "trusted" }, sequence: 1 },
      { version: 3, kind: "Claim", work_id: "w", claim_id: "claim-b", run_id: "run-b" },
      { version: 3, kind: "Claim", work_id: "w", claim_id: "claim-a", run_id: "run-a" },
    ];
    const payload = {
      inputs: {
        aw_context: JSON.stringify({
          work_claim: { work_id: "w", claim_id: "claim-a", work: { input: "trusted" } },
        }),
      },
    };

    expect(resolveWorkerAssignment(payload, transactions)).toEqual({ work_id: "w", claim_id: "claim-a" });
    expect(() => resolveWorkerAssignment({ ...payload, inputs: { aw_context: JSON.stringify({ work_claim: { work_id: "w", claim_id: "claim-a", work: { input: "tampered" } } }) } }, transactions)).toThrow("payload does not match");
    expect(() => resolveWorkerAssignment({ ...payload, inputs: { aw_context: JSON.stringify({ work_claim: { work_id: "w", claim_id: "claim-b", work: {} } }) } }, transactions)).toThrow(/not currently effective/);
    expect(replayTransactions(transactions).winner.w).toBe("claim-a");
  });
});
