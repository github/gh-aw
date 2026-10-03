// @ts-check
import { afterEach, describe, expect, it } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { createDispatchCoordinatorClaimNextTool, createDispatchCoordinatorFinishTool, createDispatchCoordinatorStateTool, loadDispatchCoordinatorSnapshot, readDispatchCoordinatorState } from "./dispatch_work_coordinator_mcp_server.cjs";
import { serializeTransactionLog } from "./dispatch_work_coordinator_replay.cjs";

const work = id => ({ kind: "Work", sequence: 1, version: 3, work: { legacy_work_id: id }, work_id: id });
const claim = (workId, id) => ({ claim_id: id, kind: "Claim", run_id: `legacy:${id}`, version: 3, work_id: workId });

const tempFiles = [];

function writeSnapshot(snapshot) {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "dispatch-coordinator-"));
  const snapshotPath = path.join(directory, "snapshot.json");
  fs.writeFileSync(snapshotPath, JSON.stringify(snapshot));
  tempFiles.push(directory);
  return snapshotPath;
}

afterEach(() => {
  for (const directory of tempFiles.splice(0)) fs.rmSync(directory, { recursive: true, force: true });
});

describe("dispatch work coordinator MCP snapshot", () => {
  it("reads and replays an activation snapshot without a Git client", () => {
    const snapshotPath = writeSnapshot({
      version: 2,
      sha: "activation-head",
      transactionLog: serializeTransactionLog([work("w"), claim("w", "c")]),
      worker: null,
    });
    const snapshot = loadDispatchCoordinatorSnapshot(snapshotPath);

    expect(readDispatchCoordinatorState(snapshot)).toEqual({
      snapshot_sha: "activation-head",
      works: [{ id: "w", work: { legacy_work_id: "w" }, sequence: 1, state: "claimed", winner: "c", claims: [{ id: "c", state: "effective" }] }],
    });
    expect(createDispatchCoordinatorStateTool(snapshot).handler({ work: "missing" })).toEqual({
      snapshot_sha: "activation-head",
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

    expect(readDispatchCoordinatorState(loadDispatchCoordinatorSnapshot(snapshotPath), { work: "constructor" })).toEqual({
      snapshot_sha: null,
      works: [{ id: "constructor", work: { legacy_work_id: "constructor" }, sequence: 1, state: "claimed", winner: "toString", claims: [{ id: "toString", state: "effective" }] }],
    });
  });

  it("rejects snapshots with an unsupported shape or invalid transaction log", () => {
    expect(() => loadDispatchCoordinatorSnapshot(writeSnapshot({ version: 1, sha: null, transactionLog: "" }))).toThrow(/invalid shape/);
    expect(() => loadDispatchCoordinatorSnapshot(writeSnapshot({ version: 2, sha: null, transactionLog: "{}\n", worker: null }))).toThrow(/invalid transaction log/);
  });

  it("records a finish intent without exposing authority parameters", () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "dispatch-coordinator-finish-"));
    tempFiles.push(directory);
    const finishPath = path.join(directory, "finish.jsonl");
    const tool = createDispatchCoordinatorFinishTool({ finishIntentPath: finishPath });

    expect(Object.keys(tool.inputSchema.properties)).toEqual(["outcome"]);
    expect(tool.inputSchema.additionalProperties).toBe(false);
    expect(tool.handler({ outcome: "completed", work_id: "untrusted" })).toEqual({ recorded: true, outcome: "completed" });
    expect(fs.readFileSync(finishPath, "utf8")).toBe('{"outcome":"completed"}\n');
  });

  it("stages FIFO claims, tracks local reservations across restarts, and enforces group limits", () => {
    const transactions = [
      { version: 3, kind: "Work", work_id: "z", work: { repo: "a", priority: 1 }, sequence: 1 },
      { version: 3, kind: "Work", work_id: "a", work: { repo: "a", priority: 2 }, sequence: 2 },
      { version: 3, kind: "Work", work_id: "b", work: { repo: "b", priority: 3 }, sequence: 3 },
    ];
    const snapshotPath = writeSnapshot({ version: 2, sha: "head", worker: null, transactionLog: serializeTransactionLog(transactions) });
    const snapshot = loadDispatchCoordinatorSnapshot(snapshotPath);
    const claimIntentPath = path.join(path.dirname(snapshotPath), "claims.jsonl");
    const tool = createDispatchCoordinatorClaimNextTool(snapshot, { claimIntentPath });
    const first = tool.handler({});
    expect(first).toMatchObject({ work_id: "z", pending: true, work: { repo: "a", priority: 1 } });
    expect(first.claim_id).toBeTruthy();
    const restarted = createDispatchCoordinatorClaimNextTool(snapshot, { claimIntentPath });
    expect(restarted.handler({ selection: { group: { fields: ["/repo"] } } })).toMatchObject({ work_id: "b", pending: true });
    expect(restarted.handler({})).toMatchObject({ work_id: "a", pending: true });
    expect(restarted.handler({})).toEqual({ snapshot_sha: "head", work: null, pending: false });
    expect(fs.readFileSync(claimIntentPath, "utf8").trim().split("\n")).toHaveLength(3);
    expect(() => tool.handler({ work_id: "untrusted" })).toThrow("only selection");
    expect(Object.keys(tool.inputSchema.properties)).toEqual(["selection"]);
    expect(snapshot.projection.work).toEqual({ a: "available", b: "available", z: "available" });
  });
});
