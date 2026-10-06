// @ts-check
import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import path from "path";
import { randomUUID } from "crypto";
import { main, renderSummary } from "./work_queue_summary.cjs";
import { serializeTransactionLog } from "./work_queue_replay.cjs";
import { queueFixture } from "./work_queue_lifecycle.test_helpers.cjs";

const directories = [];
afterEach(() => {
  for (const directory of directories.splice(0)) fs.rmSync(directory, { recursive: true, force: true });
});
function snapshotFile(fixture) {
  const directory = path.join(process.cwd(), `.queue-summary-test-${randomUUID()}`);
  fs.mkdirSync(directory);
  directories.push(directory);
  const filename = path.join(directory, "snapshot.json");
  fs.writeFileSync(filename, JSON.stringify({ version: 3, sha: "activation", worker: fixture.assignment, origin: fixture.dispatcher, captured_at: fixture.at, transactionLog: serializeTransactionLog(fixture.transactions) }));
  return filename;
}
function finish(fixture, handle, outcome) {
  fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: handle, outcome }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: handle });
}

describe("fair DAG queue lifecycle summaries", () => {
  it("summarizes genuine observer absence without calling an empty authoritative ledger valid", async () => {
    const fixture = queueFixture({ granted: false });
    const snapshotPath = snapshotFile(fixture);
    fs.writeFileSync(snapshotPath, JSON.stringify({ version: 3, sha: null, worker: null, origin: fixture.dispatcher, role: "observer", captured_at: fixture.at, transactionLog: "" }));
    const addRaw = vi.fn(() => ({ write: async () => {} }));
    await main({ snapshotPath, githubClient: fixture.githubClient, context: fixture.dispatcherContext, readWorkQueueLog: async () => ({ sha: null, transactions: [] }), core: { summary: { addRaw }, warning: vi.fn() } });
    expect(addRaw).toHaveBeenCalledWith(expect.stringContaining("0 Work nodes; 0 new checked commits"));
    expect(addRaw).toHaveBeenCalledWith(expect.stringContaining("read-only queue observer"));
    await expect(
      main({ snapshotPath, githubClient: fixture.githubClient, context: fixture.dispatcherContext, readWorkQueueLog: async () => ({ sha: "existing", transactions: [] }), core: { summary: { addRaw }, warning: vi.fn() } })
    ).rejects.toThrow(/summarize/);
  });

  it("distinguishes observer reports from unassigned queue-control authority", () => {
    const fixture = queueFixture({ granted: false });
    const summary = renderSummary({ projection: fixture.state, worker: null, role: "observer" }, fixture.state);
    expect(summary).toContain("read-only queue observer");
    expect(summary).toContain("Queue mutations and worker Claim effects are unavailable");
    expect(summary).toContain("ordinary configured outputs retain their normal authorization");
    expect(summary).not.toContain("Queue-control intents");
  });

  it("distinguishes independent mixed outcomes, pending delivery and retained native reservation", () => {
    const fixture = queueFixture({ bound: true });
    const snapshot = { projection: fixture.state, worker: fixture.assignment };
    finish(fixture, "h1", "completed");
    finish(fixture, "h2", "cancelled");
    finish(fixture, "h3", "completed");
    const summary = renderSummary(snapshot, fixture.state);
    expect(summary).toContain("completed_with_cancellations");
    expect(summary).toContain("2 completed, 1 cancelled, 0 unsettled");
    expect(summary).toContain("Native reservation: **retained**");
    expect(summary).toContain("Completion does not imply verified delivery");
    expect(summary).toContain("| Completion | 2 |");
    expect(summary).toContain("| ClaimCancellation | 1 |");
    expect(summary).not.toContain("stored task");
    expect(summary).not.toContain(fixture.assignment.claims[0].work_id);
  });

  it("labels started/unbound reservations as unresolved, never stopped or empty", () => {
    const fixture = queueFixture({ started: true });
    const summary = renderSummary({ projection: fixture.state, worker: fixture.assignment }, fixture.state);
    expect(summary).toContain("1 outstanding native reservations (1 unbound or unresolved)");
    expect(summary).toContain("Assigned group: **pending**");
  });

  it("writes a read-only progressive summary from the current canonical ledger", async () => {
    const fixture = queueFixture({ bound: true });
    const snapshotPath = snapshotFile(fixture);
    finish(fixture, "h1", "completed");
    const addRaw = vi.fn(() => ({ write: async () => {} }));
    await main({ snapshotPath, githubClient: fixture.githubClient, context: fixture.workerContext, readWorkQueueLog: fixture.readWorkQueueLog, core: { summary: { addRaw }, warning: vi.fn() } });
    expect(addRaw).toHaveBeenCalledWith(expect.stringContaining("<details>"));
    expect(addRaw).toHaveBeenCalledWith(expect.stringContaining("1 new checked commits"));
  });

  it("reports ledger validation failure without inventing a successful state", async () => {
    const fixture = queueFixture({ bound: true });
    const snapshotPath = snapshotFile(fixture);
    const addRaw = vi.fn(() => ({ write: async () => {} }));
    await expect(
      main({
        snapshotPath,
        context: fixture.workerContext,
        githubClient: fixture.githubClient,
        readWorkQueueLog: async () => {
          throw new Error("private data");
        },
        core: { warning: vi.fn(), summary: { addRaw } },
      })
    ).rejects.toThrow(/summarize/);
    expect(addRaw).toHaveBeenCalledWith(expect.stringContaining("no successful queue state is inferred"));
    expect(addRaw.mock.calls[0][0]).not.toContain("private data");
  });
});
