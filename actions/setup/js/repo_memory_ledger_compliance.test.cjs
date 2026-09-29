import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { randomUUID } from "node:crypto";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { Ledger, canonicalJSON, sha256 } = require("./ledger_store.cjs");
let root;

beforeEach(() => {
  root = fs.mkdtempSync(path.join(os.tmpdir(), "repo-memory-ledger-compliance-"));
});

afterEach(() => fs.rmSync(root, { recursive: true, force: true }));

describe("repo-memory ledger compliance", () => {
  it("persists canonical hash-verified records and rebuilds its disposable projection", () => {
    const memoryDir = path.join(root, "memory");
    fs.mkdirSync(memoryDir);
    const ledger = new Ledger({ memoryDir });
    const record = ledger.append("build", { result: "passed" });
    const shard = path.join(ledger.shardDir, `${ledger.writerId}.jsonl`);

    expect(fs.readFileSync(shard, "utf8")).toBe(`${canonicalJSON(record)}\n`);
    expect(record.sha).toBe(
      sha256({
        version: record.version,
        id: record.id,
        type: record.type,
        timestamp: record.timestamp,
        parents: record.parents,
        payload: record.payload,
      })
    );
    ledger.query();
    ledger.close();
    expect(fs.readdirSync(memoryDir)).toEqual(["ledger"]);

    const restored = new Ledger({ memoryDir });
    expect(restored.get(record.sha)).toEqual(record);
    expect(restored.status()).toMatchObject({ records: 1, invalidRecords: 0, incompleteRecords: 0 });
  });

  it("converges independently written branches and links their heads on the next append", () => {
    const baseDir = path.join(root, "base");
    fs.mkdirSync(baseDir);
    const base = new Ledger({ memoryDir: baseDir });
    const parent = base.append("seed", { value: 1 });
    const leftDir = path.join(root, "left");
    const rightDir = path.join(root, "right");
    const mergedDir = path.join(root, "merged");
    fs.cpSync(baseDir, leftDir, { recursive: true });
    fs.cpSync(baseDir, rightDir, { recursive: true });

    const left = new Ledger({ memoryDir: leftDir });
    const leftRecord = left.append("result", { writer: "left" });
    const right = new Ledger({ memoryDir: rightDir });
    const rightRecord = right.append("result", { writer: "right" });
    fs.cpSync(baseDir, mergedDir, { recursive: true });

    const mergedShardDir = path.join(mergedDir, "ledger", "shards");
    for (const branchDir of [leftDir, rightDir]) {
      const shardDir = path.join(branchDir, "ledger", "shards");
      for (const shard of fs.readdirSync(shardDir)) {
        fs.copyFileSync(path.join(shardDir, shard), path.join(mergedShardDir, shard));
      }
    }

    const merged = new Ledger({ memoryDir: mergedDir });
    expect(merged.reconstruct().heads).toEqual([leftRecord.sha, rightRecord.sha].sort());
    expect(merged.get(parent.id)).toEqual(parent);
    expect(
      merged
        .query({ type: "result" })
        .rows.map(record => record.payload.writer)
        .sort()
    ).toEqual(["left", "right"]);
    const join = merged.append("join", {});
    expect(join.parents).toEqual([leftRecord.sha, rightRecord.sha].sort());
  });

  it("isolates malformed shard lines without hiding valid records", () => {
    const memoryDir = path.join(root, "memory");
    fs.mkdirSync(memoryDir);
    const ledger = new Ledger({ memoryDir });
    const record = ledger.append("result", { valid: true });
    fs.writeFileSync(path.join(ledger.shardDir, `${randomUUID()}.jsonl`), "{bad}\n");

    expect(ledger.get(record.id)).toEqual(record);
    expect(ledger.query({ type: "result" }).rows).toEqual([record]);
    expect(ledger.status()).toMatchObject({ records: 2, validRecords: 1, invalidRecords: 1 });
    expect(ledger.status().diagnostics.map(entry => entry.code)).toEqual(["invalid-json"]);
  });
});
