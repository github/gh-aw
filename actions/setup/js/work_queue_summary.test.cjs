// @ts-check
import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import path from "path";
import os from "node:os";
import { createRequire } from "node:module";
import { main, renderSummary } from "./work_queue_summary.cjs";

const require = createRequire(import.meta.url);
const h = require("./work_queue_test_helpers.cjs");
const { replayTransactions } = require("./work_queue_replay.cjs");
const { assignmentForDispatch } = require("./work_queue_scheduler.cjs");

const directories = [];
afterEach(() => {
  for (const directory of directories.splice(0)) fs.rmSync(directory, { recursive: true, force: true });
});

/** A bound worker dispatch of Work "a" plus available Work "b". */
function boundLedger() {
  const planned = h.dispatchNext(h.submit([], [h.work("a"), h.work("b")]), { max_claims: 1, max_dispatches: 1 });
  const dispatchId = planned.decision.dispatches[0];
  const log = h.bind(h.launch(planned.log, dispatchId), dispatchId, h.workerActor("200", 1));
  return { log, dispatchId, assignment: assignmentForDispatch(replayTransactions(log), dispatchId) };
}

function snapshotFile(log, assignment) {
  const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-queue-summary-"));
  directories.push(directory);
  const filename = path.join(directory, "snapshot.json");
  fs.writeFileSync(filename, JSON.stringify({ version: 4, sha: "activation", transactionLog: h.ledgerText(log), origin: h.workerActor("200", 1), role: "worker", worker: assignment }));
  return filename;
}

const context = { repo: { owner: "owner", repo: "repo" } };

describe("work queue activity summary", () => {
  it("describes observers and settled worker assignments", () => {
    const { log } = boundLedger();
    const state = replayTransactions(log);
    expect(renderSummary({ projection: state, worker: null, role: "observer" }, state)).toContain("read-only queue observer");
    expect(renderSummary({ projection: state, worker: null, role: "worker" }, state)).toContain("already settled at activation");
  });

  it("counts operations committed since activation and the assigned Claim outcomes", () => {
    const { log, dispatchId, assignment } = boundLedger();
    const before = replayTransactions(log);
    const after = replayTransactions(h.finish(log, dispatchId, h.workerActor("200", 1)));
    const summary = renderSummary({ projection: before, worker: assignment, role: "worker" }, after);
    expect(summary).toContain("2 Work nodes; 1 new commits since activation; 0 open dispatches");
    expect(summary).toContain("| Completion | 1 |");
    expect(summary).toContain("Assigned dispatch: 1 completed, 0 cancelled, 0 open Claims; dispatch **closed**.");
    expect(summary).not.toContain("task");
    expect(summary).toMatch(/<\/details>\n\n\n$/);
  });

  it("writes the summary from the current ledger", async () => {
    const { log, dispatchId, assignment } = boundLedger();
    const snapshotPath = snapshotFile(log, assignment);
    const fake = h.fakeQueueGitHub(h.ledgerText(h.finish(log, dispatchId, h.workerActor("200", 1))));
    const addRaw = vi.fn(() => ({ write: async () => {} }));
    await main({ snapshotPath, githubClient: fake.githubClient, context, core: { summary: { addRaw }, warning: vi.fn() } });
    expect(addRaw.mock.calls[0][0]).toContain("1 new commits since activation");
    expect(addRaw.mock.calls[0][0]).toContain("</details>\n\n\n<details>");
  });

  it("reports ledger read failure without inventing a state", async () => {
    const { log, assignment } = boundLedger();
    const addRaw = vi.fn(() => ({ write: async () => {} }));
    const readWorkQueueLog = async () => {
      throw new Error("private data");
    };
    await expect(main({ snapshotPath: snapshotFile(log, assignment), context, githubClient: {}, readWorkQueueLog, core: { warning: vi.fn(), summary: { addRaw } } })).rejects.toThrow(/summarize/);
    expect(addRaw.mock.calls[0][0]).toContain("no queue state is inferred");
    expect(addRaw.mock.calls[0][0]).not.toContain("private data");
  });
});
