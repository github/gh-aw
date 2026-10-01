import { execSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createRequire } from "node:module";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

const require = createRequire(import.meta.url);
const { Ledger, canonicalJSON } = require("./ledger_store.cjs");
const {
  PLAN_VERSION,
  RejectedPlanError,
  STATE_PATH,
  STATE_VERSION,
  applyTransitionToDirectory,
  computePlanId,
  createPlan,
  isCompactionDue,
  loadSegments,
  materializeSnapshot,
  parseCompactionConfig,
  parseTrigger,
  prepareApply,
  readPlanFile,
  readState,
  selectSources,
  validatePlan,
} = require("./ledger_compaction.cjs");

const BASE_COMMIT = "a".repeat(40);
const NOW = new Date("2026-10-01T00:00:00.000Z");

/** @param {Record<string, any>} [compaction] @param {Record<string, any>} [overrides] */
function encodeConfig(compaction = {}, overrides = {}) {
  const raw = { name: "findings", branch_name: "ledgers/findings", max_record_kb: 32, max_segment_kb: 100, max_patch_kb: 10, compaction: { schedule: "daily", min_segments: 2, max_segments: 3, ...compaction }, ...overrides };
  return JSON.stringify(raw);
}

let sourceDir;

/** Create one segment per writer, each holding a single record. */
function writeSegments(count, dir = sourceDir) {
  const records = [];
  for (let index = 0; index < count; index++) {
    const ledger = new Ledger({ memoryDir: dir });
    records.push(ledger.append("finding", { index }));
    ledger.close();
  }
  return records;
}

function shardIds(dir = sourceDir) {
  return fs
    .readdirSync(path.join(dir, "ledger", "shards"))
    .filter(name => name.endsWith(".jsonl"))
    .map(name => name.slice(0, -6))
    .sort();
}

function allRecordShas(dir, config) {
  const loaded = loadSegments(dir, config);
  return [...new Set([...loaded.segments.values()].flatMap(segment => segment.records.map(record => record.sha)))].sort();
}

function planFor(config, trigger = "scheduled") {
  const loaded = loadSegments(sourceDir, config);
  const selection = selectSources(loaded, config);
  expect(selection.sources).not.toBeNull();
  return createPlan({ loaded, sources: /** @type {string[]} */ selection.sources, config, trigger, baseCommit: BASE_COMMIT, now: NOW });
}

/** Re-sign a tampered plan so only semantic validation can reject it. */
function resign(plan) {
  plan.plan_id = computePlanId(plan);
  return plan;
}

beforeEach(() => {
  sourceDir = fs.mkdtempSync(path.join(os.tmpdir(), "ledger-compaction-"));
});
afterEach(() => fs.rmSync(sourceDir, { recursive: true, force: true }));

describe("parseCompactionConfig", () => {
  it("parses compiler-generated configuration with defaults", () => {
    const config = parseCompactionConfig(JSON.stringify({ name: "findings", branch_name: "ledgers/findings", max_record_kb: 32, max_segment_kb: 100, max_patch_kb: 10, compaction: {} }));
    expect(config).toMatchObject({ name: "findings", branch: "ledgers/findings", schedule: "daily", minSegments: 32, maxSegments: 128, maxSegmentBytes: 100 * 1024 });
  });

  it.each([
    ["missing value", undefined],
    ["non JSON", "nope"],
    ["mismatched branch", encodeConfig({}, { branch_name: "main" })],
    ["invalid name", encodeConfig({}, { name: "../x", branch_name: "ledgers/../x" })],
    ["unknown schedule", encodeConfig({ schedule: "hourly" })],
    ["unknown policy key", encodeConfig({ command: "rm -rf /" })],
    ["min above max", encodeConfig({ min_segments: 10, max_segments: 5 })],
    ["min below two", encodeConfig({ min_segments: 1 })],
    ["empty script", encodeConfig({ script: "  " })],
  ])("rejects %s", (_name, value) => {
    expect(() => parseCompactionConfig(value)).toThrow(TypeError);
  });

  it("accepts only scheduled and requested triggers", () => {
    expect(parseTrigger("")).toBe("scheduled");
    expect(parseTrigger("requested")).toBe("requested");
    expect(() => parseTrigger("agent")).toThrow(TypeError);
  });
});

describe("isCompactionDue", () => {
  const daily = parseCompactionConfig(encodeConfig());
  const manual = parseCompactionConfig(encodeConfig({ schedule: "manual" }));
  const state = last => ({ last_applied_at: last });

  it("always honors explicit requests", () => {
    expect(isCompactionDue({ config: manual, trigger: "requested", state: null, now: NOW }).due).toBe(true);
    expect(isCompactionDue({ config: daily, trigger: "requested", state: state(NOW.toISOString()), now: NOW }).due).toBe(true);
  });

  it("follows the configured schedule for scheduled maintenance", () => {
    expect(isCompactionDue({ config: manual, trigger: "scheduled", state: null, now: NOW }).due).toBe(false);
    expect(isCompactionDue({ config: daily, trigger: "scheduled", state: null, now: NOW }).due).toBe(true);
    expect(isCompactionDue({ config: daily, trigger: "scheduled", state: state("2026-09-30T20:00:00.000Z"), now: NOW }).due).toBe(false);
    expect(isCompactionDue({ config: daily, trigger: "scheduled", state: state("2026-09-30T00:00:00.000Z"), now: NOW }).due).toBe(true);
  });
});

describe("readState", () => {
  it("returns null when state file does not exist", () => {
    expect(readState(sourceDir)).toBeNull();
  });

  it("parses valid state correctly", () => {
    const valid = {
      version: STATE_VERSION,
      ledger: "findings",
      last_applied_at: "2026-10-01T00:00:00.000Z",
      last_plan_id: "sha256:" + "a".repeat(64),
      last_trigger: "scheduled",
      recent_plan_ids: ["sha256:" + "a".repeat(64)],
    };
    fs.mkdirSync(path.join(sourceDir, "ledger", "compaction"), { recursive: true });
    fs.writeFileSync(path.join(sourceDir, STATE_PATH), JSON.stringify(valid));
    expect(readState(sourceDir)).toEqual(valid);
  });

  it("rejects invalid or unparseable timestamps", () => {
    fs.mkdirSync(path.join(sourceDir, "ledger", "compaction"), { recursive: true });
    for (const invalidTimestamp of ["2026-99-99T00:00:00Z", "invalid", "2026-13-45T00:00:00Z"]) {
      fs.writeFileSync(
        path.join(sourceDir, STATE_PATH),
        JSON.stringify({
          version: STATE_VERSION,
          ledger: "findings",
          last_applied_at: invalidTimestamp,
          last_plan_id: "sha256:" + "a".repeat(64),
          last_trigger: "scheduled",
          recent_plan_ids: [],
        })
      );
      expect(readState(sourceDir)).toBeNull();
    }
  });
});

describe("materializeSnapshot", () => {
  it("creates parent directories when materializing files including state.json", () => {
    const repoDir = fs.mkdtempSync(path.join(os.tmpdir(), "ledger-repo-"));
    const targetDir = fs.mkdtempSync(path.join(os.tmpdir(), "ledger-snap-"));
    const segmentId = "00000000-0000-4000-8000-000000000001";
    try {
      execSync("git init && git config user.name test && git config user.email test@example.com", { cwd: repoDir, stdio: "pipe" });
      const shardPath = path.join(repoDir, "ledger", "shards", `${segmentId}.jsonl`);
      const stateFilePath = path.join(repoDir, STATE_PATH);
      fs.mkdirSync(path.dirname(shardPath), { recursive: true });
      fs.writeFileSync(shardPath, '{"id":"r1","subject":"s1"}\n');
      fs.mkdirSync(path.dirname(stateFilePath), { recursive: true });
      fs.writeFileSync(
        stateFilePath,
        JSON.stringify({
          version: STATE_VERSION,
          ledger: "findings",
          last_applied_at: "2026-10-01T00:00:00.000Z",
          last_plan_id: "sha256:" + "a".repeat(64),
          last_trigger: "scheduled",
          recent_plan_ids: [],
        })
      );
      execSync("git add ledger/shards ledger/compaction && git commit -m 'Initial ledger state' && git branch -M ledgers/findings", {
        cwd: repoDir,
        stdio: "pipe",
      });

      const config = parseCompactionConfig(encodeConfig());
      materializeSnapshot({ workspaceDir: repoDir, sourceDir: targetDir, refName: "ledgers/findings", config });
      expect(fs.existsSync(path.join(targetDir, STATE_PATH))).toBe(true);
      expect(fs.existsSync(path.join(targetDir, "ledger", "shards", `${segmentId}.jsonl`))).toBe(true);
    } finally {
      fs.rmSync(repoDir, { recursive: true, force: true });
      fs.rmSync(targetDir, { recursive: true, force: true });
    }
  });
});

describe("selectSources", () => {
  it("does nothing below the minimum segment count", () => {
    const config = parseCompactionConfig(encodeConfig({ min_segments: 4, max_segments: 4 }));
    writeSegments(3);
    expect(selectSources(loadSegments(sourceDir, config), config).sources).toBeNull();
  });

  it("selects at most max segments in deterministic order", () => {
    const config = parseCompactionConfig(encodeConfig());
    writeSegments(5);
    const { sources } = selectSources(loadSegments(sourceDir, config), config);
    expect(sources).toEqual(shardIds().slice(0, 3));
  });

  it("skips invalid segments instead of compacting them", () => {
    const config = parseCompactionConfig(encodeConfig());
    writeSegments(3);
    fs.appendFileSync(path.join(sourceDir, "ledger", "shards", `${shardIds()[0]}.jsonl`), '{"forged":true}\n');
    const loaded = loadSegments(sourceDir, config);
    expect(loaded.invalid).toBe(1);
    expect(selectSources(loaded, config).sources).toEqual(shardIds().slice(1));
  });

  it("runs a configured selection script in an isolated worker", () => {
    const config = parseCompactionConfig(encodeConfig({ script: "return { sources: segments.slice(-2).map(segment => segment.id).reverse() }" }));
    writeSegments(4);
    expect(selectSources(loadSegments(sourceDir, config), config).sources).toEqual(shardIds().slice(-2));
  });

  it.each([
    ["unknown segment ids", "return { sources: ['00000000-0000-4000-8000-000000000000', segments[0].id] }"],
    ["a single segment", "return { sources: [segments[0].id] }"],
    ["unexpected output keys", "return { sources: [segments[0].id, segments[1].id], command: 'push' }"],
  ])("rejects a selection script returning %s", (_name, script) => {
    const config = parseCompactionConfig(encodeConfig({ script }));
    writeSegments(3);
    expect(() => selectSources(loadSegments(sourceDir, config), config)).toThrow(TypeError);
  });

  it("denies the selection script access to process and dynamic code", () => {
    writeSegments(3);
    for (const script of ["return { sources: process.env }", "return { sources: Function('return 1')() }", "return { sources: require('fs') }"]) {
      const config = parseCompactionConfig(encodeConfig({ script }));
      expect(() => selectSources(loadSegments(sourceDir, config), config)).toThrow("Ledger compaction script failed");
    }
  });
});

describe("compaction plans", () => {
  it("creates a deterministic plan identified by immutable segment and record hashes", () => {
    const config = parseCompactionConfig(encodeConfig());
    const records = writeSegments(3);
    const plan = planFor(config);
    expect(plan).not.toBeNull();
    expect(plan.version).toBe(PLAN_VERSION);
    expect(plan.sources.map(source => source.segment)).toEqual(shardIds());
    expect(plan.sources.every(source => /^sha256:[0-9a-f]{64}$/.test(source.sha256))).toBe(true);
    expect(plan.replacement.records).toEqual(records.map(record => record.sha).sort());
    expect(validatePlan(JSON.parse(JSON.stringify(plan)), config)).toEqual(plan);
    const rescheduled = createPlan({ loaded: loadSegments(sourceDir, config), sources: shardIds(), config, trigger: "requested", baseCommit: "b".repeat(40), now: new Date() });
    expect(rescheduled.plan_id).toBe(plan.plan_id);
  });

  it.each([
    ["an unknown top-level key", plan => ({ ...plan, command: "git push --force" })],
    ["an unknown source key", plan => resign({ ...plan, sources: [{ ...plan.sources[0], path: "../../etc/passwd" }, ...plan.sources.slice(1)] })],
    ["a different ledger", plan => resign({ ...plan, ledger: "other" })],
    ["a different branch", plan => resign({ ...plan, branch: "main" })],
    ["a tampered plan_id", plan => ({ ...plan, plan_id: `sha256:${"0".repeat(64)}` })],
    ["a replacement missing a source record", plan => resign({ ...plan, replacement: { ...plan.replacement, records: plan.replacement.records.slice(1) } })],
    ["a replacement that is also a source", plan => resign({ ...plan, replacement: { ...plan.sources[0] } })],
    ["unsorted sources", plan => resign({ ...plan, sources: [...plan.sources].reverse() })],
    ["an invalid base commit", plan => resign({ ...plan, base_commit: "HEAD; rm -rf /" })],
    ["a path-like segment identity", plan => resign({ ...plan, sources: [{ ...plan.sources[0], segment: "../x" }, ...plan.sources.slice(1)] })],
    ["too many sources", plan => resign({ ...plan, sources: Array.from({ length: 4 }, () => plan.sources[0]) })],
    ["an unsupported version", plan => resign({ ...plan, version: "gh-aw/ledger-compaction-plan/v0" })],
  ])("rejects a plan with %s", (_name, tamper) => {
    const config = parseCompactionConfig(encodeConfig());
    writeSegments(3);
    expect(() => validatePlan(tamper(planFor(config)), config)).toThrow(RejectedPlanError);
  });

  it("reads plan artifacts strictly", () => {
    const file = path.join(sourceDir, "plan.json");
    const link = path.join(sourceDir, "link.json");
    fs.writeFileSync(file, "{}");
    fs.symlinkSync(file, link);
    expect(() => readPlanFile(link)).toThrow(RejectedPlanError);
    fs.writeFileSync(file, Buffer.from([0x7b, 0xff, 0x7d]));
    expect(() => readPlanFile(file)).toThrow(RejectedPlanError);
    fs.writeFileSync(file, "");
    expect(() => readPlanFile(file)).toThrow(RejectedPlanError);
  });
});

describe("prepareApply", () => {
  it("applies atomically, preserves concurrent appends, and is idempotent", () => {
    const config = parseCompactionConfig(encodeConfig());
    writeSegments(3);
    const plan = validatePlan(planFor(config), config);
    const [concurrent] = writeSegments(1);
    const before = allRecordShas(sourceDir, config);

    const prepared = prepareApply({ plan, sourceDir, config, now: NOW });
    expect(prepared.status).toBe("ready");
    expect(prepared.deletions.map(deletion => deletion.path)).toEqual(plan.sources.map(source => `ledger/shards/${source.segment}.jsonl`));
    expect(prepared.additions.map(addition => addition.path)).toEqual([`ledger/shards/${plan.replacement.segment}.jsonl`, STATE_PATH]);
    expect(prepared.stats).toMatchObject({ sourceSegments: 3, sourceRecords: 3, replacementRecords: 3, unrelatedSegments: 1 });

    applyTransitionToDirectory(sourceDir, prepared);
    expect(allRecordShas(sourceDir, config)).toEqual(before);
    expect(allRecordShas(sourceDir, config)).toContain(concurrent.sha);
    expect(readState(sourceDir)).toMatchObject({ ledger: "findings", last_plan_id: plan.plan_id, last_trigger: "scheduled", recent_plan_ids: [plan.plan_id] });

    const again = prepareApply({ plan, sourceDir, config, now: NOW });
    expect(again.status).toBe("already_applied");
    expect(isCompactionDue({ config, trigger: "scheduled", state: readState(sourceDir), now: NOW }).due).toBe(false);
  });

  it("reports a plan as stale when another compaction retired some sources", () => {
    const config = parseCompactionConfig(encodeConfig());
    writeSegments(3);
    const plan = planFor(config);
    fs.rmSync(path.join(sourceDir, "ledger", "shards", `${plan.sources[0].segment}.jsonl`));
    expect(prepareApply({ plan, sourceDir, config }).status).toBe("stale");
  });

  it("reports a conflict when a source segment changed after planning", () => {
    const config = parseCompactionConfig(encodeConfig());
    writeSegments(3);
    const plan = planFor(config);
    const shard = path.join(sourceDir, "ledger", "shards", `${plan.sources[1].segment}.jsonl`);
    const scratch = fs.mkdtempSync(path.join(os.tmpdir(), "ledger-compaction-extra-"));
    const other = new Ledger({ memoryDir: scratch });
    const extra = other.append("finding", { index: 99 });
    other.close();
    fs.rmSync(scratch, { recursive: true, force: true });
    fs.appendFileSync(shard, `${canonicalJSON(extra)}\n`);
    expect(loadSegments(sourceDir, config).invalid).toBe(0);
    expect(prepareApply({ plan, sourceDir, config }).status).toBe("conflict");
  });

  it("rejects a replacement that is not the content-addressed union of the sources", () => {
    const config = parseCompactionConfig(encodeConfig());
    writeSegments(3);
    const plan = planFor(config);
    const forged = resign({ ...plan, replacement: { ...plan.replacement, records: [...plan.replacement.records, `sha256:${"f".repeat(64)}`].sort() } });
    expect(prepareApply({ plan: validatePlan(forged, config), sourceDir, config }).status).toBe("rejected");
  });

  it("leaves the ledger untouched for rejected, stale, or conflicting plans", () => {
    const config = parseCompactionConfig(encodeConfig());
    writeSegments(3);
    const ids = shardIds();
    const plan = planFor(config);
    const forged = resign({ ...plan, replacement: { ...plan.replacement, sha256: `sha256:${"e".repeat(64)}` } });
    expect(prepareApply({ plan: forged, sourceDir, config }).status).toBe("rejected");
    expect(shardIds()).toEqual(ids);
    expect(fs.existsSync(path.join(sourceDir, STATE_PATH))).toBe(false);
  });
});

describe("ledger_compaction_apply main", () => {
  beforeEach(() => {
    const outputs = {};
    global.core = { info: () => {}, warning: () => {}, debug: () => {}, setOutput: (key, value) => (outputs[key] = value), outputs };
    global.context = { repo: { owner: "octo", repo: "repo" } };
  });

  it("rejects a hostile plan before fetching or writing anything", async () => {
    const { main } = require("./ledger_compaction_apply.cjs");
    const planFile = path.join(sourceDir, "plan.json");
    fs.writeFileSync(planFile, JSON.stringify({ version: PLAN_VERSION, ledger: "findings", command: "git push --force" }));
    const githubClient = { graphql: () => Promise.reject(new Error("must not be called")) };
    await expect(main({ config: encodeConfig(), planFile, githubClient })).rejects.toThrow(RejectedPlanError);
    expect(global.core.outputs.result).toBe("rejected");
  });

  it("marks runtime failures as failed rather than rejected", async () => {
    const { main } = require("./ledger_compaction_apply.cjs");
    const missingPlanFile = path.join(sourceDir, "does-not-exist.json");
    const githubClient = { graphql: () => Promise.reject(new Error("must not be called")) };
    await expect(main({ config: encodeConfig(), planFile: missingPlanFile, githubClient })).rejects.toThrow();
    expect(global.core.outputs.result).toBe("failed");
  });
});
