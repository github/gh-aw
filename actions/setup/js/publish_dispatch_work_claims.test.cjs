// @ts-check
import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { applyTransactions } from "./dispatch_work_coordinator_replay.cjs";
import { publishDispatcherClaims, readClaimIntents, readPublishedAssignment } from "./publish_dispatch_work_claims.cjs";

const directories = [];
afterEach(() => {
  for (const directory of directories.splice(0)) fs.rmSync(directory, { recursive: true, force: true });
});

function setup(intents) {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "publish-dispatch-claims-"));
  directories.push(directory);
  const claimIntentPath = path.join(directory, "intents.jsonl");
  const publishedClaimsPath = path.join(directory, "published.json");
  fs.writeFileSync(claimIntentPath, intents.map(intent => JSON.stringify(intent)).join("\n") + "\n");
  let current = [
    { version: 3, kind: "Work", work_id: "old", work: { repo: "a", priority: 1 }, sequence: 1 },
    { version: 3, kind: "Work", work_id: "new", work: { repo: "b", priority: 2 }, sequence: 2 },
  ];
  const options = {
    claimIntentPath,
    publishedClaimsPath,
    context: { repo: { owner: "owner", repo: "repo" }, runId: 123 },
    applyAndPublish: async ({ deriveIntents }) => {
      const result = applyTransactions(current, deriveIntents(current));
      current = result.transactions;
      return result;
    },
    readCoordinatorLog: async () => ({ sha: "head", transactions: current }),
  };
  return {
    options,
    get transactions() {
      return current;
    },
    replace(transactions) {
      current = transactions;
    },
  };
}

describe("trusted dispatcher claim publication", () => {
  it("derives owning run provenance from trusted context and verifies claims before dispatch", async () => {
    const fake = setup([
      { work_id: "old", claim_id: "c1", selection: {} },
      { work_id: "new", claim_id: "c2", selection: {} },
    ]);
    const assignments = await publishDispatcherClaims(fake.options);
    expect(assignments).toEqual([
      { work_id: "old", claim_id: "c1", work: { repo: "a", priority: 1 } },
      { work_id: "new", claim_id: "c2", work: { repo: "b", priority: 2 } },
    ]);
    expect(fake.transactions.filter(transaction => transaction.kind === "Claim").every(transaction => transaction.run_id === "123")).toBe(true);
    expect(readPublishedAssignment("c1", fake.options.publishedClaimsPath)).toEqual(assignments[0]);
    expect(() => readPublishedAssignment("arbitrary", fake.options.publishedClaimsPath)).toThrow("not published");
    expect(await publishDispatcherClaims(fake.options)).toEqual(assignments);
  });

  it("fails closed instead of silently selecting a different Work on conflict retry", async () => {
    const fake = setup([{ work_id: "old", claim_id: "pending", selection: {} }]);
    const publish = vi.fn(async ({ deriveIntents }) => {
      expect(deriveIntents(fake.transactions)).toHaveLength(1);
      const competing = { version: 3, kind: "Claim", work_id: "old", claim_id: "remote", run_id: "remote-run" };
      fake.replace(applyTransactions(fake.transactions, [competing]).transactions);
      return applyTransactions(fake.transactions, deriveIntents(fake.transactions));
    });
    await expect(publishDispatcherClaims({ ...fake.options, applyAndPublish: publish })).rejects.toThrow("stale");
    expect(fake.transactions.some(transaction => transaction.claim_id === "pending")).toBe(false);
    expect(JSON.parse(fs.readFileSync(fake.options.publishedClaimsPath, "utf8"))).toEqual([]);
  });

  it("counts new group occupancy on every latest-state derivation", async () => {
    const fake = setup([{ work_id: "new", claim_id: "pending", selection: { group: { fields: ["/repo"] }, sort: [{ field: "/priority", direction: "desc" }] } }]);
    fake.replace([...fake.transactions, { version: 3, kind: "Work", work_id: "remote", work: { repo: "b" }, sequence: 3 }, { version: 3, kind: "Claim", work_id: "remote", claim_id: "remote-claim", run_id: "remote-run" }]);
    await expect(publishDispatcherClaims(fake.options)).rejects.toThrow("stale");
    expect(fake.transactions.some(transaction => transaction.claim_id === "pending")).toBe(false);
  });

  it("rejects malformed, duplicate, or authority-bearing intents before publication", async () => {
    for (const intents of [
      [{ work_id: "old", claim_id: "c", selection: {}, run_id: "spoofed" }],
      [{ work_id: "old", claim_id: "c", selection: null }],
      [
        { work_id: "old", claim_id: "c", selection: {} },
        { work_id: "new", claim_id: "c", selection: {} },
      ],
    ]) {
      const fake = setup(intents);
      expect(() => readClaimIntents(fake.options.claimIntentPath)).toThrow();
      const publish = vi.fn(fake.options.applyAndPublish);
      await expect(publishDispatcherClaims({ ...fake.options, applyAndPublish: publish })).rejects.toThrow();
      expect(publish).not.toHaveBeenCalled();
    }
  });
});
