// @ts-check
import { afterEach, describe, expect, it } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { createWorkQueueFinishTool, createWorkQueueStateTool, loadWorkQueueSnapshot, readWorkQueueState } from "./work_queue_mcp_server.cjs";
import { createWorkTransaction, serializeTransactionLog } from "./work_queue_replay.cjs";

const work = id => ({ version: 2, kind: "Work", work: id, claim: null, attempt: null });
const claim = (workId, id) => ({ version: 2, kind: "Claim", work: workId, claim: id, attempt: null });

const tempFiles = [];

function writeSnapshot(snapshot) {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "work-queue-"));
  const snapshotPath = path.join(directory, "snapshot.json");
  fs.writeFileSync(snapshotPath, JSON.stringify(snapshot));
  tempFiles.push(directory);
  return snapshotPath;
}

afterEach(() => {
  for (const directory of tempFiles.splice(0)) fs.rmSync(directory, { recursive: true, force: true });
});

describe("work queue MCP snapshot", () => {
  it("reads and replays an activation snapshot without a Git client", () => {
    const snapshotPath = writeSnapshot({
      version: 2,
      sha: "activation-head",
      transactionLog: serializeTransactionLog([work("w"), claim("w", "c")]),
      worker: null,
    });
    const snapshot = loadWorkQueueSnapshot(snapshotPath);

    expect(readWorkQueueState(snapshot)).toEqual({
      snapshot_sha: "activation-head",
      next_work: null,
      works: [{ id: "w", state: "claimed", enqueued: 0, winner: "c", claims: [{ id: "c", state: "effective" }] }],
    });
    const tool = createWorkQueueStateTool(snapshot);
    expect(tool.name).toBe("work_queue_read");
    expect(tool.handler({ work: "missing" })).toEqual({
      snapshot_sha: "activation-head",
      next_work: null,
      works: [{ id: "missing", state: "absent", winner: null, claims: [] }],
    });
  });

  it("treats prototype-named identifiers as ordinary identifiers", () => {
    const snapshotPath = writeSnapshot({
      version: 2,
      sha: null,
      transactionLog: serializeTransactionLog([work("constructor"), claim("constructor", "toString")]),
      worker: null,
    });

    expect(readWorkQueueState(loadWorkQueueSnapshot(snapshotPath), { work: "constructor" })).toEqual({
      snapshot_sha: null,
      next_work: null,
      works: [{ id: "constructor", state: "claimed", enqueued: 0, winner: "toString", claims: [{ id: "toString", state: "effective" }] }],
    });
  });

  it("recommends and lists available work oldest-first from the immutable snapshot", () => {
    const snapshotPath = writeSnapshot({
      version: 2,
      sha: "old-head",
      transactionLog: serializeTransactionLog([createWorkTransaction("a-new", 20), createWorkTransaction("z-old", 10), createWorkTransaction("claimed", 1), claim("claimed", "c")]),
      worker: null,
    });
    const snapshot = loadWorkQueueSnapshot(snapshotPath);
    const result = readWorkQueueState(snapshot);
    expect(result.next_work).toBe("z-old");
    expect(result.works.map(item => item.id)).toEqual(["z-old", "a-new", "claimed"]);
    expect(result.works.map(item => item.enqueued)).toEqual([10, 20, 1]);
    expect(readWorkQueueState(snapshot, { work: "a-new" }).next_work).toBe("z-old");
  });

  it("loads v1 Work at age zero without changing the snapshot envelope", () => {
    const snapshotPath = writeSnapshot({
      version: 2,
      sha: "historical-head",
      transactionLog: `${JSON.stringify(createWorkTransaction("new", 100))}\n${JSON.stringify({ ...work("legacy"), version: 1 })}\n`,
      worker: null,
    });
    const snapshot = loadWorkQueueSnapshot(snapshotPath);
    expect(JSON.parse(fs.readFileSync(snapshotPath, "utf8")).version).toBe(2);
    expect(snapshot.projection.transactions).toContainEqual(work("legacy"));
    const result = readWorkQueueState(snapshot);
    expect(result.next_work).toBe("legacy");
    expect(result.works.map(item => [item.id, item.enqueued])).toEqual([
      ["legacy", 0],
      ["new", 100],
    ]);
  });

  it("rejects snapshots with an unsupported shape or invalid transaction log", () => {
    expect(() => loadWorkQueueSnapshot(writeSnapshot({ version: 1, sha: null, transactionLog: "" }))).toThrow(/invalid shape/);
    expect(() => loadWorkQueueSnapshot(writeSnapshot({ version: 2, sha: null, transactionLog: "{}\n", worker: null }))).toThrow(/invalid transaction log/);
  });

  it("records a finish intent without exposing authority parameters", () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "work-queue-finish-"));
    tempFiles.push(directory);
    const finishPath = path.join(directory, "finish.jsonl");
    const tool = createWorkQueueFinishTool({ finishIntentPath: finishPath });

    expect(tool.name).toBe("work_queue_claim_finish");
    expect(Object.keys(tool.inputSchema.properties)).toEqual(["outcome"]);
    expect(tool.inputSchema.additionalProperties).toBe(false);
    expect(tool.handler({ outcome: "completed", work_id: "untrusted" })).toEqual({ recorded: true, outcome: "completed" });
    expect(fs.readFileSync(finishPath, "utf8")).toBe('{"outcome":"completed"}\n');
  });
});
