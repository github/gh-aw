import { describe, it, expect, beforeEach, afterEach } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { createRequire } from "module";

const require = createRequire(import.meta.url);
const { normalizeEntry, readAuditEntries, main } = require("./merge_ledger_transactions.cjs");

const RECORD = {
  id: "ldg-11111111-2222-4333-8444-555555555555",
  type: "note",
  timestamp: "2026-01-01T00:00:00.000Z",
  parents: [],
  sha: `sha256:${"a".repeat(64)}`,
  payload_sha: `sha256:${"b".repeat(64)}`,
};

const ENTRY = {
  type: "ledger_mutation",
  operation: "append",
  timestamp: "2026-01-01T00:00:00.000Z",
  record: RECORD,
};

describe("merge_ledger_transactions", () => {
  let dir;
  let warnings;
  let infos;

  beforeEach(() => {
    dir = fs.mkdtempSync(path.join(os.tmpdir(), "ledger-merge-"));
    warnings = [];
    infos = [];
    global.core = {
      warning: message => warnings.push(message),
      info: message => infos.push(message),
    };
  });

  afterEach(() => {
    fs.rmSync(dir, { recursive: true, force: true });
    delete global.core;
    delete process.env.GH_AW_SAFE_OUTPUTS;
    delete process.env.GH_AW_LEDGER_TRANSACTION_LOG;
  });

  it("normalizes a well-formed append entry", () => {
    expect(normalizeEntry(ENTRY)).toEqual(ENTRY);
  });

  it("rejects entries with forged or malformed metadata", () => {
    expect(normalizeEntry(null)).toBeNull();
    expect(normalizeEntry({ ...ENTRY, type: "create_issue" })).toBeNull();
    expect(normalizeEntry({ ...ENTRY, operation: "retire" })).toBeNull();
    expect(normalizeEntry({ ...ENTRY, record: { ...RECORD, id: "../../etc/passwd" } })).toBeNull();
    expect(normalizeEntry({ ...ENTRY, record: { ...RECORD, sha: "not-a-hash" } })).toBeNull();
    expect(normalizeEntry({ ...ENTRY, record: { ...RECORD, parents: ["nope"] } })).toBeNull();
    expect(normalizeEntry({ ...ENTRY, record: { ...RECORD, timestamp: "x".repeat(65) } })).toBeNull();
  });

  it("drops extra record fields so only redacted metadata is merged", () => {
    const normalized = normalizeEntry({ ...ENTRY, record: { ...RECORD, payload: { secret: "value" } } });
    expect(normalized.record).toEqual(RECORD);
  });

  it("skips malformed lines and deduplicates by record sha", () => {
    const logPath = path.join(dir, "ledger-transactions.jsonl");
    fs.writeFileSync(logPath, ["not json", JSON.stringify(ENTRY), JSON.stringify(ENTRY), JSON.stringify({ type: "create_issue" }), ""].join("\n"));
    const entries = readAuditEntries(logPath);
    expect(entries).toHaveLength(1);
    expect(warnings.join(" ")).toContain("Dropped 2");
  });

  it("returns nothing when the log is missing", () => {
    expect(readAuditEntries(path.join(dir, "absent.jsonl"))).toEqual([]);
  });

  it("appends validated entries to the safe-output file", async () => {
    const logPath = path.join(dir, "ledger-transactions.jsonl");
    const outputsPath = path.join(dir, "outputs", "outputs.jsonl");
    fs.writeFileSync(logPath, `${JSON.stringify(ENTRY)}\n`);
    process.env.GH_AW_LEDGER_TRANSACTION_LOG = logPath;
    process.env.GH_AW_SAFE_OUTPUTS = outputsPath;
    await main();
    const lines = fs.readFileSync(outputsPath, "utf8").trim().split("\n");
    expect(lines).toHaveLength(1);
    expect(JSON.parse(lines[0])).toEqual(ENTRY);
  });

  it("never throws when the safe-output path is unwritable", async () => {
    const logPath = path.join(dir, "ledger-transactions.jsonl");
    fs.writeFileSync(logPath, `${JSON.stringify(ENTRY)}\n`);
    process.env.GH_AW_LEDGER_TRANSACTION_LOG = logPath;
    process.env.GH_AW_SAFE_OUTPUTS = path.join(logPath, "nested", "outputs.jsonl");
    await expect(main()).resolves.toBeUndefined();
    expect(warnings.join(" ")).toContain("Failed to merge ledger audit entries");
  });
});
