// @ts-check
import { afterEach, describe, expect, it } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { createDispatchCoordinatorFinishTool, createDispatchCoordinatorStateTool, loadDispatchCoordinatorSnapshot, readDispatchCoordinatorState } from "./dispatch_work_coordinator_mcp_server.cjs";
import { serializeTransactionLog } from "./dispatch_work_coordinator_replay.cjs";

const work = id => ({ kind: "Work", work: id, claim: null, attempt: null });
const claim = (workId, id) => ({ kind: "Claim", work: workId, claim: id, attempt: null });

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
      works: [{ id: "w", state: "claimed", winner: "c", claims: [{ id: "c", state: "effective" }] }],
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
      works: [{ id: "constructor", state: "claimed", winner: "toString", claims: [{ id: "toString", state: "effective" }] }],
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
});
