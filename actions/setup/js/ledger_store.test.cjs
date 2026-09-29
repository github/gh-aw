import fs from "node:fs";
import path from "node:path";
import { randomUUID } from "node:crypto";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const require = createRequire(import.meta.url);
const { Ledger, canonicalJSON, sha256 } = require("./ledger_store.cjs");
const testDir = path.dirname(fileURLToPath(import.meta.url));
let memoryDir;
let ledger;

beforeEach(() => {
  memoryDir = fs.mkdtempSync(path.join(testDir, ".ledger-test-"));
  ledger = new Ledger({ memoryDir, clock: () => new Date("2026-09-29T00:00:00.000Z") });
});
afterEach(() => fs.rmSync(memoryDir, { recursive: true, force: true }));

describe("Ledger", () => {
  it("writes canonical JSONL records with record hashes and automatic DAG parents", () => {
    const first = ledger.append("note", { z: 1, a: { y: 2, x: 3 } });
    expect(first).toMatchObject({ version: 1, type: "note", parents: [], sha: sha256({ version: 1, id: first.id, type: "note", timestamp: first.timestamp, parents: [], payload: first.payload }) });
    expect(first.sha).toMatch(/^sha256:[a-f0-9]{64}$/);
    expect(first.id).toMatch(/^ldg-[0-9a-f]{8}-/);
    expect(first.timestamp).toBe("2026-09-29T00:00:00.000Z");
    const second = ledger.append("metric", { count: 2 });
    expect(second.parents).toEqual([first.sha]);
    const files = fs.readdirSync(ledger.shardDir);
    expect(files).toEqual([`${ledger.writerId}.jsonl`]);
    expect(fs.readFileSync(path.join(ledger.shardDir, files[0]), "utf8")).toBe(`${canonicalJSON(first)}\n${canonicalJSON(second)}\n`);
    expect(ledger.get(first.id)).toEqual(first);
    expect(ledger.get(second.sha)).toEqual(second);
    expect(ledger.status().heads).toEqual([second.sha]);
    expect(ledger.status()).toMatchObject({ records: 2, validRecords: 2, invalidRecords: 0, incompleteRecords: 0, shards: 1 });
    expect(() => ledger.append("note", { missing: undefined })).toThrow();
  });

  it("serializes successful agent mutations to the safe-output transaction log", () => {
    const transactionLogPath = path.join(memoryDir, "safe-output-items.jsonl");
    const audited = new Ledger({ memoryDir, transactionLogPath, clock: () => new Date("2026-09-29T00:00:00.000Z") });
    const record = audited.append("note", { secret: "not copied into the audit event" });
    const entry = JSON.parse(fs.readFileSync(transactionLogPath, "utf8"));
    expect(entry).toMatchObject({
      type: "ledger_mutation",
      operation: "append",
      timestamp: "2026-09-29T00:00:00.000Z",
      record: { id: record.id, type: "note", sha: record.sha, payload_sha: sha256(record.payload) },
    });
    expect(JSON.stringify(entry)).not.toContain("not copied into the audit event");
  });

  it("reconstructs concurrent heads, preserves orphan records and quarantines corrupt lines", () => {
    const first = ledger.append("note", { branch: "base" });
    const other = new Ledger({ memoryDir });
    const branch = other.append("note", { branch: "concurrent" });
    expect(branch.parents).toEqual([first.sha]);
    const fork = { version: 1, id: `ldg-${randomUUID()}`, type: "note", timestamp: first.timestamp, parents: [first.sha], payload: { branch: "fork" } };
    const forked = { ...fork, sha: sha256(fork) };
    const file = path.join(ledger.shardDir, `${randomUUID()}.jsonl`);
    fs.writeFileSync(file, `${canonicalJSON(forked)}\n`);
    expect(ledger.reconstruct().heads).toEqual([branch.sha, forked.sha].sort());
    const join = ledger.append("merge", {});
    expect(join.parents).toEqual([branch.sha, forked.sha].sort());
    const orphanBody = { ...fork, id: `ldg-${randomUUID()}`, parents: [sha256({ nonexistent: true })] };
    const orphan = { ...orphanBody, sha: sha256(orphanBody) };
    fs.writeFileSync(path.join(ledger.shardDir, `${randomUUID()}.jsonl`), `${canonicalJSON(orphan)}\n`);
    fs.appendFileSync(file, '{"bad":}\n');
    expect(ledger.get(orphan.sha)).toEqual(orphan);
    expect(
      ledger
        .status()
        .diagnostics.map(entry => entry.code)
        .sort()
    ).toEqual(["invalid-json", "missing-parent"]);
    expect(ledger.status().heads).toContain(orphan.sha);
    expect(ledger.status()).toMatchObject({ incompleteRecords: 1, invalidRecords: 1 });
  });

  it("classifies hash mismatch, noncanonical JSONL and incomplete tails without losing other records", () => {
    const good = ledger.append("good", 1);
    const changed = ledger.append("bad", 2);
    const file = path.join(ledger.shardDir, `${ledger.writerId}.jsonl`);
    fs.appendFileSync(file, `${JSON.stringify(changed)}\n`);
    fs.appendFileSync(file, '{"incomplete":');
    expect(ledger.reconstruct().diagnostics.map(entry => entry.code)).toEqual(["truncated-record", "invalid-envelope"]);
    const content = fs.readFileSync(file, "utf8").replace(`${canonicalJSON(changed)}\n`, `${canonicalJSON({ ...changed, payload: 9 })}\n`);
    fs.writeFileSync(file, content);
    expect(ledger.get(good.id)).toEqual(good);
    expect(ledger.reconstruct().diagnostics.map(entry => entry.code)).toContain("invalid-sha");
  });

  it("isolates invalid lines and schemas while retaining SHA-addressable duplicate-ID conflicts", () => {
    const schemaPath = path.join(memoryDir, "events.schema.json");
    fs.writeFileSync(schemaPath, JSON.stringify({ type: "object", properties: { ok: { type: "boolean" } }, required: ["ok"] }));
    const validated = new Ledger({ memoryDir, schemaPath });
    const first = validated.append("note", { ok: true });
    const nextBody = { version: 1, id: `ldg-${randomUUID()}`, type: "note", timestamp: first.timestamp, parents: [first.sha], payload: { ok: true } };
    const next = { ...nextBody, sha: sha256(nextBody) };
    const conflictBody = { ...nextBody, id: first.id, payload: { ok: false } };
    const conflict = { ...conflictBody, sha: sha256(conflictBody) };
    const badSchemaBody = { ...nextBody, id: `ldg-${randomUUID()}`, payload: {} };
    const badSchema = { ...badSchemaBody, sha: sha256(badSchemaBody) };
    const unsupportedBody = { ...nextBody, id: `ldg-${randomUUID()}`, version: 2 };
    const unsupported = { ...unsupportedBody, sha: sha256(unsupportedBody) };
    const file = path.join(validated.shardDir, `${randomUUID()}.jsonl`);
    fs.writeFileSync(file, ["{bad}", canonicalJSON(next), canonicalJSON(conflict), canonicalJSON(badSchema), canonicalJSON(unsupported), canonicalJSON(next), "{}"].join("\n") + "\n");
    const state = validated.reconstruct();
    expect(state.records.map(record => record.sha).sort()).toEqual([first.sha, next.sha, conflict.sha].sort());
    expect(validated.get(conflict.sha)).toEqual(conflict);
    expect(validated.get(first.id)).toEqual(first);
    expect(state.diagnostics.map(entry => entry.code).sort()).toEqual(["duplicate-id", "duplicate-id-conflict", "invalid-envelope", "invalid-json", "invalid-schema", "unsupported-version"]);
    expect(validated.status()).toMatchObject({ invalidRecords: 6, incompleteRecords: 0 });
  });

  it("projects into disposable SQLite and bounds parameterized payload queries", () => {
    const first = ledger.append("note", { actor: "a", detail: { score: 5 } });
    const second = ledger.append("note", { actor: "b", detail: { score: 9 } });
    expect(ledger.query({ type: "note", where: { "payload.actor": { eq: "a" }, "payload.detail.score": { eq: 5 } } }).rows.map(row => row.payload.actor)).toEqual(["a"]);
    const projection = ledger.projection.db;
    expect(
      projection
        .prepare("SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name")
        .all()
        .map(row => row.name)
    ).toEqual(["diagnostics", "parents", "records", "shards"]);
    expect(projection.prepare("SELECT parent_sha FROM parents WHERE child_sha = ?").get(second.sha).parent_sha).toBe(first.sha);
    expect(projection.prepare("SELECT shard, offset FROM records WHERE sha = ?").get(second.sha)).toMatchObject({ shard: `${ledger.writerId}.jsonl`, offset: Buffer.byteLength(`${canonicalJSON(first)}\n`) });
    expect(projection.prepare("SELECT count(*) AS total FROM shards").get().total).toBe(1);
    expect(projection.prepare("SELECT count(*) AS total FROM diagnostics").get().total).toBe(0);
    expect(projection.prepare("SELECT name FROM sqlite_master WHERE type = 'index' AND name LIKE 'records_by_%'").all()).toHaveLength(3);
    expect(ledger.query({ type: "note" }).rows).toHaveLength(2);
    expect(ledger.projection.db).toBe(projection);
    expect(ledger.get(first.sha)).toEqual(first);
    expect(ledger.status().records).toBe(2);
    expect(ledger.projection.db).toBe(projection);
    expect(ledger.query({ where: { "payload.detail.score": { gte: 9 } } }).rows.map(row => row.payload.actor)).toEqual(["b"]);
    expect(ledger.query({ where: { "payload.actor": { in: ["a"] } } }).rows.map(row => row.payload.actor)).toEqual(["a"]);
    expect(ledger.query({ where: { "payload.actor": { prefix: "b" } } }).rows.map(row => row.payload.actor)).toEqual(["b"]);
    expect(ledger.query({ where: { "payload.missing": { exists: false } } }).rows).toHaveLength(2);
    expect(ledger.query({ where: { "payload.actor": { eq: "' OR 1=1 --" } } }).rows).toEqual([]);
    expect(ledger.query({ limit: 1 }).rows).toHaveLength(1);
    expect(() => ledger.query({ limit: 501 })).toThrow();
    expect(() => ledger.query({ where: { "x'); DROP TABLE ledger_records;--": 1 } })).toThrow();
    expect(() => ledger.query({ where: { actor: { eq: "a" } } })).toThrow("payload path");
    expect(() => ledger.query({ where: { "payload.actor": "a" } })).toThrow("one operator");
    expect(() => ledger.query({ where: { "payload.actor": { unknown: "a" } } })).toThrow("Unsupported query operator");
    ledger.append("note", { actor: "c" });
    expect(ledger.projection).toBeNull();
    expect(ledger.query({ type: "note" }).rows).toHaveLength(3);
    expect(Object.is(ledger.projection.db, projection)).toBe(false);
    ledger.close();
    expect(ledger.projection).toBeNull();
    expect(fs.readdirSync(path.join(memoryDir, "ledger"))).toEqual(["shards"]);
  });

  it("rebuilds the disposable projection when another writer changes a shard", () => {
    ledger.append("note", { ok: true });
    ledger.query();
    const initial = ledger.projection.db;
    fs.appendFileSync(ledger.writerPath, "{broken}\n");
    const result = ledger.query();
    expect(result.rows).toHaveLength(1);
    expect(result.diagnostics.map(entry => entry.code)).toEqual(["invalid-json"]);
    expect(Object.is(ledger.projection.db, initial)).toBe(false);
    expect(ledger.projection.db.prepare("SELECT code FROM diagnostics").get().code).toBe("invalid-json");
    ledger.close();
    expect(ledger.projection).toBeNull();
  });

  it("validates payloads with the existing simplified schema and rejects unsupported keywords", () => {
    const schemaPath = path.join(memoryDir, "schema.json");
    fs.writeFileSync(
      schemaPath,
      JSON.stringify({
        oneOf: [{ type: "object", properties: { ok: { type: "boolean" } }, required: ["ok"], additionalProperties: false }, { type: "integer" }],
      })
    );
    const validated = new Ledger({ memoryDir, schemaPath });
    expect(() => validated.append("contact", "invalid")).toThrow("does not match");
    expect(() => validated.append("contact", { other: true })).toThrow("does not match");
    expect(validated.append("contact", { ok: true }).payload).toEqual({ ok: true });
    expect(validated.append("contact", 10).payload).toBe(10);
    expect(validated.status().records).toBe(2);
    for (const keyword of ["$ref", "minimum", "format", "pattern"]) {
      fs.writeFileSync(schemaPath, JSON.stringify({ type: "string", [keyword]: keyword === "$ref" ? "#" : 1 }));
      expect(() => new Ledger({ memoryDir, schemaPath })).toThrow("Unsupported ledger schema keyword");
    }
    fs.writeFileSync(schemaPath, JSON.stringify({ type: "object", additionalProperties: true }));
    expect(() => new Ledger({ memoryDir, schemaPath })).toThrow("Only additionalProperties: false is supported");
    fs.writeFileSync(schemaPath, JSON.stringify({ type: "string" }));
    const schemaLink = path.join(memoryDir, "schema-link.json");
    fs.symlinkSync(schemaPath, schemaLink);
    expect(() => new Ledger({ memoryDir, schemaPath: schemaLink })).toThrow("not a symlink");
  });

  it("limits per-file patches and rotates before limits, including corrupted files in count", () => {
    const bounded = new Ledger({ memoryDir, maxFiles: 2, maxPatchBytes: 24000 });
    fs.mkdirSync(bounded.shardDir, { recursive: true });
    for (const id of ["00000000-0000-4000-8000-000000000000", "00000000-0000-4000-8000-000000000001"]) {
      fs.writeFileSync(path.join(bounded.shardDir, `${id}.jsonl`), "");
    }
    expect(fs.readdirSync(bounded.shardDir)).toHaveLength(2);
    expect(() => bounded.append("note", "x".repeat(7000))).toThrow("file-count limit");
    expect(() => bounded.append("note", "x".repeat(32768))).toThrow(RangeError);
    for (const file of fs.readdirSync(bounded.shardDir)) expect(fs.statSync(path.join(bounded.shardDir, file)).size).toBeLessThan(32768);
    const defaultBudget = new Ledger({ memoryDir: fs.mkdtempSync(path.join(memoryDir, "budget-")), maxPatchBytes: 8000 });
    defaultBudget.append("note", "x".repeat(7000));
    expect(() => defaultBudget.append("note", "x".repeat(1000))).toThrow("run exceeds maximum patch");
  });

  it("uses a default shard limit of 1024", () => {
    const defaults = new Ledger({ memoryDir });
    expect(defaults.maxFiles).toBe(1024);
    expect(defaults.maxSegmentBytes).toBe(100 * 1024);
    expect(defaults.maxRecordBytes).toBe(32 * 1024);
    expect(defaults.maxPatchBytes).toBe(10 * 1024);
  });

  it("applies configurable record, segment, and patch bounds", () => {
    const bounded = new Ledger({ memoryDir, maxSegmentBytes: 4096, maxRecordBytes: 1024, maxPatchBytes: 2048 });
    expect(bounded.maxSegmentBytes).toBe(4096);
    expect(bounded.maxRecordBytes).toBe(1024);
    expect(bounded.maxPatchBytes).toBe(2048);
    expect(() => new Ledger({ memoryDir, maxSegmentBytes: 1024, maxRecordBytes: 2048 })).toThrow("Invalid maxRecordBytes");
    expect(() => bounded.append("note", "x".repeat(1024))).toThrow("maximum message size");
  });

  it("compacts deterministic batches of stable closed segments and retires covered sources", async () => {
    const first = ledger.append("note", { value: 1 });
    const secondLedger = new Ledger({ memoryDir });
    secondLedger.append("note", { value: 2 });
    const compactor = new Ledger({ memoryDir });
    const segments = compactor.listSegments();
    expect(segments).toHaveLength(2);
    const result = await compactor.compact({ minSegments: 2, maxSegments: 2 });
    expect(result).toMatchObject({ changed: true, selected: 2, records: 2, retired: 2, before: 2, after: 1 });
    expect(result.replacement).toMatch(/^[0-9a-f-]{36}$/);
    expect(compactor.get(first.sha)).toEqual(first);
    expect(compactor.get(secondLedger.status().heads[0])).toMatchObject({ type: "note" });
    expect(compactor.listSegments()).toHaveLength(1);
  });

  it("deduplicates and hashes compacted records deterministically", async () => {
    const first = ledger.append("note", { value: 1 });
    const secondLedger = new Ledger({ memoryDir });
    const second = secondLedger.append("note", { value: 2 });
    const compactor = new Ledger({ memoryDir });
    const forward = compactor.createSegment([first, second]);
    const reverse = compactor.createSegment([second, first, first]);
    expect(reverse).toEqual(forward);
    expect(compactor.readRecords(forward.id)).toHaveLength(2);
    await expect(compactor.compact({ minSegments: 1 })).rejects.toThrow("Invalid minSegments");
    await expect(compactor.compact({ minSegments: 3, maxSegments: 2 })).rejects.toThrow("Invalid maxSegments");
  });

  it("does not compact below threshold or include excluded current-run segments", async () => {
    ledger.append("note", { value: 1 });
    const secondLedger = new Ledger({ memoryDir });
    secondLedger.append("note", { value: 2 });
    const currentLedger = new Ledger({ memoryDir });
    currentLedger.append("note", { value: 3 });
    const excluded = currentLedger.writerId;
    const compactor = new Ledger({ memoryDir, excludeSegments: [excluded] });
    expect(await compactor.compact({ minSegments: 3, maxSegments: 3 })).toMatchObject({
      changed: false,
      selected: 0,
      before: 2,
      after: 2,
    });
    expect(await compactor.compact({ minSegments: 2, maxSegments: 2 })).toMatchObject({
      changed: true,
      selected: 2,
      before: 2,
      after: 1,
    });
    expect(fs.existsSync(path.join(compactor.shardDir, `${excluded}.jsonl`))).toBe(true);
  });

  it("does not retire a source containing malformed ledger records", () => {
    const first = ledger.append("note", { value: 1 });
    const secondLedger = new Ledger({ memoryDir });
    secondLedger.append("note", { value: 2 });
    const compactor = new Ledger({ memoryDir });
    const sources = compactor.listSegments();
    const records = sources.flatMap(segment => compactor.readRecords(segment.id));
    const replacement = compactor.createSegment(records);
    compactor.markCovered(
      sources.map(segment => segment.id),
      replacement.id
    );
    fs.appendFileSync(path.join(compactor.shardDir, `${sources[0].id}.jsonl`), '{"forged":true}\n');
    expect(compactor.retireCovered()).toBe(0);
    expect(fs.existsSync(path.join(compactor.shardDir, `${sources[0].id}.jsonl`))).toBe(true);
    expect(compactor.get(first.sha)).toEqual(first);
  });

  it("does not retire forged, incomplete, self-referential, or current-run coverage declarations", () => {
    const first = ledger.append("note", { value: 1 });
    const secondLedger = new Ledger({ memoryDir });
    const second = secondLedger.append("note", { value: 2 });
    const compactor = new Ledger({ memoryDir });
    const sources = compactor.listSegments();
    const sourceRecords = sources.flatMap(segment => compactor.readRecords(segment.id));
    const incomplete = compactor.createSegment([sourceRecords[0]]);
    compactor.markCovered(
      sources.map(segment => segment.id),
      incomplete.id
    );
    expect(compactor.retireCovered()).toBe(0);
    expect(compactor.listSegments()).toHaveLength(3);

    const selfCoverage = new Ledger({ memoryDir });
    const stableSource = sources[0].id;
    selfCoverage.markCovered([stableSource], stableSource);
    expect(selfCoverage.retireCovered()).toBe(0);
    expect(fs.existsSync(path.join(selfCoverage.shardDir, `${stableSource}.jsonl`))).toBe(true);

    const forged = new Ledger({ memoryDir });
    const forgedDeclaration = { sources: [stableSource], replacement: incomplete.id };
    const forgedName = sha256(forgedDeclaration).slice("sha256:".length);
    fs.mkdirSync(forged.coverageDir, { recursive: true });
    fs.writeFileSync(path.join(forged.coverageDir, `${forgedName}.jsonl`), `${canonicalJSON(forgedDeclaration)}\n`);
    expect(forged.retireCovered()).toBe(0);
    expect(fs.existsSync(path.join(forged.shardDir, `${stableSource}.jsonl`))).toBe(true);

    const current = compactor.append("note", { value: 3 });
    const complete = compactor.createSegment([...sourceRecords, ...compactor.readRecords(compactor.writerId)]);
    compactor.markCovered([compactor.writerId], complete.id);
    expect(compactor.retireCovered()).toBe(0);
    expect(fs.existsSync(path.join(compactor.shardDir, `${compactor.writerId}.jsonl`))).toBe(true);
  });

  it("rejects JavaScript compactor scripts instead of running them in-process", async () => {
    await expect(ledger.runCompactor("ledger.log.constructor('return process')()")).rejects.toThrow("disabled until a least-privilege worker");
  });

  it("does not publish or project new files when file or directory fsync fails", () => {
    const original = fs.fsyncSync;
    for (const failingCall of [1, 2]) {
      let calls = 0;
      const fsync = vi.spyOn(fs, "fsyncSync").mockImplementation(fd => {
        if (++calls === failingCall) throw new Error("sync failed");
        return original(fd);
      });
      try {
        expect(() => ledger.append("note", { unpublished: true })).toThrow();
      } finally {
        fsync.mockRestore();
      }
      expect(ledger.status().records).toBe(0);
      expect(fs.readdirSync(ledger.shardDir)).toEqual([]);
    }
  });

  it("truncates a failed append to an existing JSONL shard before any projection", () => {
    const first = ledger.append("note", 1);
    ledger.query();
    const durableProjection = ledger.projection.db;
    const file = path.join(ledger.shardDir, `${ledger.writerId}.jsonl`);
    const originalContent = fs.readFileSync(file, "utf8");
    const original = fs.fsyncSync;
    let calls = 0;
    const fsync = vi.spyOn(fs, "fsyncSync").mockImplementation(fd => {
      if (++calls === 1) throw new Error("sync failed");
      return original(fd);
    });
    try {
      expect(() => ledger.append("note", 2)).toThrow("durably append");
    } finally {
      fsync.mockRestore();
    }
    expect(fs.readFileSync(file, "utf8")).toBe(originalContent);
    expect(ledger.query().rows).toEqual([first]);
    expect(ledger.projection.db).toBe(durableProjection);
  });

  it("rejects symlinked ledger directories before reads or writes", () => {
    const target = path.join(memoryDir, "target");
    fs.mkdirSync(target);
    fs.symlinkSync(target, path.join(memoryDir, "ledger"));
    expect(() => ledger.reconstruct()).toThrow("not a symlink");
    expect(() => ledger.append("note", 1)).toThrow("not a symlink");
    expect(fs.readdirSync(target)).toEqual([]);
    fs.unlinkSync(path.join(memoryDir, "ledger"));
    fs.mkdirSync(path.join(memoryDir, "ledger"));
    fs.symlinkSync(target, ledger.shardDir);
    expect(() => ledger.reconstruct()).toThrow("not a symlink");
    expect(() => ledger.append("note", 1)).toThrow("not a symlink");
    expect(fs.readdirSync(target)).toEqual([]);
  });

  it("isolates symlinked shard files and refuses a restored symlink writer", () => {
    const record = ledger.append("note", 1);
    const original = ledger.writerPath;
    const link = path.join(ledger.shardDir, `${randomUUID()}.jsonl`);
    fs.symlinkSync(original, link);
    expect(ledger.reconstruct().records).toEqual([record]);
    expect(ledger.reconstruct().diagnostics.map(entry => entry.code)).toEqual(["invalid-envelope"]);
    fs.unlinkSync(link);
    const sentinel = path.join(memoryDir, "sentinel");
    fs.writeFileSync(sentinel, "unchanged");
    fs.unlinkSync(original);
    fs.symlinkSync(sentinel, original);
    expect(() => ledger.append("note", 2)).toThrow();
    expect(fs.readFileSync(sentinel, "utf8")).toBe("unchanged");
  });
});
