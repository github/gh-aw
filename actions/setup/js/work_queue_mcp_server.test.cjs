// @ts-check
import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { createWorkQueueFinishTool, createWorkQueueStateTool, loadWorkQueueSnapshot, readWorkQueueState } from "./work_queue_mcp_server.cjs";
import { createWorkTransaction, serializeTransactionLog } from "./work_queue_replay.cjs";
import { createServer, handleMessage, registerTool } from "./mcp_server_core.cjs";

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
    expect(JSON.parse(tool.handler({ work: "missing" }).content[0].text)).toEqual({
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

  it("sorts available Work with field and length expressions while retaining default ties", () => {
    const snapshot = loadWorkQueueSnapshot(
      writeSnapshot({
        version: 2,
        sha: "head",
        transactionLog: serializeTransactionLog([createWorkTransaction("z", 10), createWorkTransaction("aaa", 20), createWorkTransaction("bb", 20), createWorkTransaction("claimed", 30), claim("claimed", "c")]),
        worker: null,
      })
    );
    const newestFirst = [{ key: "enqueued", direction: "desc" }];
    expect(readWorkQueueState(snapshot, { sort: newestFirst }).works.map(item => item.id)).toEqual(["aaa", "bb", "z", "claimed"]);
    expect(readWorkQueueState(snapshot, { sort: newestFirst }).next_work).toBe("aaa");
    expect(readWorkQueueState(snapshot, { work: "z", sort: newestFirst }).next_work).toBe("aaa");

    const byLength = [
      { key: "id_length", direction: "asc" },
      { key: "id", direction: "desc" },
    ];
    expect(readWorkQueueState(snapshot, { sort: byLength }).works.map(item => item.id)).toEqual(["z", "bb", "aaa", "claimed"]);
    expect(readWorkQueueState(snapshot, { sort: [{ key: "id", direction: "desc" }] }).next_work).toBe("z");
    expect(readWorkQueueState(snapshot).next_work).toBe("z");
  });

  it("sorts IDs by Unicode code point length", () => {
    const snapshot = loadWorkQueueSnapshot(
      writeSnapshot({
        version: 2,
        sha: "head",
        transactionLog: serializeTransactionLog([createWorkTransaction("😀", 10), createWorkTransaction("ab", 10)]),
        worker: null,
      })
    );

    expect(readWorkQueueState(snapshot, { sort: [{ key: "id_length", direction: "asc" }] }).works.map(item => item.id)).toEqual(["😀", "ab"]);
  });

  it("rejects unsupported and unbounded sort operators", () => {
    const snapshot = loadWorkQueueSnapshot(writeSnapshot({ version: 2, sha: null, transactionLog: "", worker: null }));
    const term = { key: "id", direction: "asc" };
    for (const sort of [[], [term, term, term, term, term], "id", [{ ...term, direction: "random" }], [{ ...term, extra: true }], [{ ...term, key: "state" }]]) {
      expect(() => readWorkQueueState(snapshot, { sort })).toThrow(TypeError);
    }
    const sortSchema = createWorkQueueStateTool(snapshot).inputSchema.properties.sort;
    expect(sortSchema.minItems).toBe(1);
    expect(sortSchema.maxItems).toBe(4);
    expect(sortSchema.items.required).toEqual(["key", "direction"]);
    expect(sortSchema.items.properties.key.enum).toEqual(["id", "enqueued", "id_length"]);
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
    expect(JSON.parse(tool.handler({ outcome: "completed", work_id: "untrusted" }).content[0].text)).toEqual({ recorded: true, outcome: "completed" });
    expect(fs.readFileSync(finishPath, "utf8")).toBe('{"outcome":"completed"}\n');
    expect(fs.statSync(finishPath).mode & 0o777).toBe(0o644);
  });

  it("only records per-claim intents for inbound claims in a batch", () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "work-queue-batch-"));
    tempFiles.push(directory);
    const finishPath = path.join(directory, "finish.jsonl");
    const tool = createWorkQueueFinishTool({
      finishIntentPath: finishPath,
      worker: [
        { claim_id: "a", work_id: "one" },
        { claim_id: "b", work_id: "two" },
      ],
    });
    expect(tool.inputSchema.required).toEqual(["claim_id"]);
    expect(() => tool.handler({ claim_id: "unknown", outcome: "completed" })).toThrow(/trusted inbound/);
    expect(() => tool.handler({ outcome: "completed" })).toThrow(/trusted inbound/);
    tool.handler({ claim_id: "a", outcome: "completed" });
    tool.handler({ claim_id: "b", outcome: "cancelled" });
    expect(fs.readFileSync(finishPath, "utf8")).toBe('{"claim_id":"a","outcome":"completed"}\n{"claim_id":"b","outcome":"cancelled"}\n');
    const arrayOfOne = createWorkQueueFinishTool({ finishIntentPath: finishPath, worker: [{ claim_id: "a", work_id: "one" }] });
    expect(arrayOfOne.inputSchema.required).toEqual(["claim_id"]);
  });

  it("returns queue state and finish confirmation through the stdio MCP transport", async () => {
    const snapshotPath = writeSnapshot({ version: 2, sha: null, transactionLog: "", worker: null });
    const finishPath = path.join(path.dirname(snapshotPath), "finish.jsonl");
    const server = createServer({ name: "work-queue", version: "1.0.0" });
    server.debug = vi.fn();
    server.writeMessage = vi.fn();
    registerTool(server, createWorkQueueStateTool(loadWorkQueueSnapshot(snapshotPath)));
    registerTool(server, createWorkQueueFinishTool({ finishIntentPath: finishPath }));

    for (const [id, name, args, expected] of [
      [1, "work_queue_read", { work: "missing" }, { snapshot_sha: null, next_work: null, works: [{ id: "missing", state: "absent", winner: null, claims: [] }] }],
      [2, "work_queue_claim_finish", { outcome: "completed" }, { recorded: true, outcome: "completed" }],
    ]) {
      await handleMessage(server, { jsonrpc: "2.0", id, method: "tools/call", params: { name, arguments: args } });
      expect(server.writeMessage).toHaveBeenCalledWith({
        jsonrpc: "2.0",
        id,
        result: { content: [{ type: "text", text: JSON.stringify(expected) }], isError: false },
      });
    }
    expect(fs.readFileSync(finishPath, "utf8")).toBe('{"outcome":"completed"}\n');
  });

  it("makes an existing owner-only intent readable for runner artifact collection", () => {
    const snapshotPath = writeSnapshot({ version: 2, sha: null, transactionLog: "", worker: null });
    const finishPath = path.join(path.dirname(snapshotPath), "finish.jsonl");
    fs.writeFileSync(finishPath, "", { mode: 0o600 });
    const tool = createWorkQueueFinishTool({ finishIntentPath: finishPath });

    tool.handler({});

    expect(fs.statSync(finishPath).mode & 0o777).toBe(0o644);
    expect(fs.readFileSync(finishPath, "utf8")).toBe('{"outcome":"completed"}\n');
  });

  it("reports filesystem failures instead of confirming a recorded finish intent", () => {
    const snapshotPath = writeSnapshot({ version: 2, sha: null, transactionLog: "", worker: null });
    const tool = createWorkQueueFinishTool({ finishIntentPath: path.join(path.dirname(snapshotPath), "finish.jsonl") });
    const error = new Error("permission denied");
    const append = vi.spyOn(fs, "appendFileSync").mockImplementation(() => {
      throw error;
    });
    try {
      expect(() => tool.handler({ outcome: "completed" })).toThrow(expect.objectContaining({ message: "Failed to record work queue finish intent", cause: error }));
    } finally {
      append.mockRestore();
    }
  });
});
