// @ts-check
"use strict";

/**
 * Ledger compaction core shared by the Agentic Maintenance plan and apply jobs.
 *
 * Trust model:
 * - The plan job is untrusted. It runs without repository write credentials, uses built-in
 *   segment selection, and emits a compaction plan artifact.
 * - The apply job is trusted. It never executes user JavaScript. It treats the plan as hostile
 *   input, validates it with a strict schema, revalidates every referenced segment against the
 *   latest ledger branch state, rebuilds the replacement segment from the branch's own records,
 *   and only then publishes the state transition atomically.
 *
 * Compaction is lossless: the replacement segment is exactly the content-addressed union of the
 * retired source segments, so the ledger history stays reconstructable.
 */

const fs = require("node:fs");
const path = require("node:path");
const cp = require("node:child_process");
const crypto = require("node:crypto");
const { Ledger, buildSegment, canonicalJSON, configuredLedgerLimits, SEGMENT_ID } = require("./ledger_store.cjs");

const PLAN_VERSION = "gh-aw/ledger-compaction-plan/v1";
const STATE_VERSION = "gh-aw/ledger-compaction-state/v1";
const STATE_PATH = "ledger/compaction/state.json";
const SHARD_PREFIX = "ledger/shards/";
const SEGMENT_METADATA = { compaction: PLAN_VERSION };
const MAX_PLAN_BYTES = 16 * 1024 * 1024;
const MAX_PLAN_SOURCES = 256;
const MAX_PLAN_RECORDS = 100000;
const MAX_STATE_BYTES = 64 * 1024;
const MAX_RECENT_PLANS = 20;
const MAX_SNAPSHOT_FILES = 8192;
const MAX_SNAPSHOT_BYTES = 256 * 1024 * 1024;
const DEFAULT_MIN_SEGMENTS = 32;
const DEFAULT_MAX_SEGMENTS = 128;
const HOUR_MS = 60 * 60 * 1000;
/** Minimum spacing between scheduled compactions. Slightly below the nominal cadence to tolerate cron jitter. */
const SCHEDULE_INTERVALS_MS = { daily: 20 * HOUR_MS, weekly: (7 * 24 - 4) * HOUR_MS, manual: null };
const TRIGGERS = new Set(["scheduled", "requested"]);
const LEDGER_NAME = /^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/;
const DIGEST = /^sha256:[0-9a-f]{64}$/;
const COMMIT = /^(?:[0-9a-f]{40}|[0-9a-f]{64})$/;
const ISO_TIMESTAMP = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,3})?Z$/;

const PLAN_KEYS = ["base_commit", "branch", "created_at", "ledger", "plan_id", "replacement", "sources", "trigger", "version"];
const SOURCE_KEYS = ["bytes", "records", "segment", "sha256"];
const STATE_KEYS = ["last_applied_at", "last_plan_id", "last_trigger", "ledger", "recent_plan_ids", "version"];

class RejectedPlanError extends Error {
  /** @param {string} message */
  constructor(message) {
    super(message);
    this.name = "RejectedPlanError";
  }
}

/** @param {unknown} value */
function isPlainObject(value) {
  return !!value && typeof value === "object" && !Array.isArray(value) && Object.getPrototypeOf(value) === Object.prototype;
}

/**
 * @param {unknown} value
 * @param {string[]} keys
 */
function hasExactKeys(value, keys) {
  if (!isPlainObject(value)) return false;
  const actual = Object.keys(Object(value)).sort();
  return actual.length === keys.length && actual.every((key, index) => key === keys[index]);
}

/** @param {Buffer | string} bytes */
function digestBytes(bytes) {
  return `sha256:${crypto.createHash("sha256").update(bytes).digest("hex")}`;
}

/**
 * @param {unknown} values
 * @param {RegExp} pattern
 * @param {number} max
 */
function isStrictlySortedList(values, pattern, max) {
  if (!Array.isArray(values) || values.length === 0 || values.length > max) return false;
  for (let index = 0; index < values.length; index++) {
    const value = values[index];
    if (typeof value !== "string" || !pattern.test(value)) return false;
    if (index > 0 && !(values[index - 1] < value)) return false;
  }
  return true;
}

/** @param {number} value @param {number} min @param {number} max */
function isIntegerInRange(value, min, max) {
  return Number.isSafeInteger(value) && value >= min && value <= max;
}

/**
 * Parse the trusted, compiler-generated per-ledger compaction configuration.
 * @param {string | undefined} value JSON
 */
function parseCompactionConfig(value = process.env.GH_AW_LEDGER_COMPACTION_CONFIG) {
  if (!value) throw new TypeError("Missing ledger compaction configuration");
  let raw;
  try {
    raw = JSON.parse(value);
  } catch (error) {
    throw new TypeError("Invalid ledger compaction configuration", { cause: error });
  }
  if (
    !isPlainObject(raw) ||
    typeof raw.name !== "string" ||
    !LEDGER_NAME.test(raw.name) ||
    raw.branch_name !== `ledgers/${raw.name}` ||
    !isIntegerInRange(raw.max_record_kb, 1, 32) ||
    !isIntegerInRange(raw.max_segment_kb, raw.max_record_kb, 10240) ||
    !isIntegerInRange(raw.max_patch_kb, 1, 10240) ||
    !isPlainObject(raw.compaction)
  ) {
    throw new TypeError("Invalid ledger compaction configuration");
  }
  const compaction = raw.compaction;
  const schedule = compaction.schedule ?? "daily";
  const minSegments = compaction.min_segments ?? DEFAULT_MIN_SEGMENTS;
  const maxSegments = compaction.max_segments ?? Math.max(DEFAULT_MAX_SEGMENTS, minSegments);
  if (
    Object.keys(compaction).some(key => !["max_segments", "min_segments", "schedule"].includes(key)) ||
    !Object.hasOwn(SCHEDULE_INTERVALS_MS, schedule) ||
    !isIntegerInRange(minSegments, 2, MAX_PLAN_SOURCES) ||
    !isIntegerInRange(maxSegments, minSegments, MAX_PLAN_SOURCES)
  ) {
    throw new TypeError("Invalid ledger compaction policy");
  }
  const limits = configuredLedgerLimits(raw);
  return {
    name: raw.name,
    branch: raw.branch_name,
    maxSegmentBytes: raw.max_segment_kb * 1024,
    maxRecordBytes: limits.maxRecordBytes,
    maxPatchBytes: limits.maxPatchBytes,
    schedule,
    minSegments,
    maxSegments,
  };
}

/** @param {string | undefined} value */
function parseTrigger(value = process.env.GH_AW_LEDGER_COMPACTION_TRIGGER) {
  const trigger = value || "scheduled";
  if (!TRIGGERS.has(trigger)) throw new TypeError("Invalid ledger compaction trigger");
  return trigger;
}

/**
 * Materialize ledger shards and compaction state from a fetched ref into sourceDir.
 * Oversized shards are not materialized: they can never be compaction sources.
 * @param {{workspaceDir: string, refName: string, sourceDir: string, config: ReturnType<typeof parseCompactionConfig>}} options
 */
function materializeSnapshot({ workspaceDir, refName, sourceDir, config }) {
  const listing = cp.spawnSync("git", ["ls-tree", "-r", "-z", "-l", "--full-tree", refName, "--", "ledger/shards", STATE_PATH], {
    cwd: workspaceDir,
    encoding: "utf8",
    maxBuffer: 64 * 1024 * 1024,
    timeout: 120000,
  });
  if (listing.error || listing.status !== 0) throw new Error("Failed to list ledger branch contents");
  /** @type {{path: string, oid: string, size: number}[]} */
  const wanted = [];
  let skippedOversized = 0;
  for (const entry of listing.stdout.split("\0").filter(Boolean)) {
    const match = /^(\d{6}) (\w+) ([0-9a-f]{40,64}) +(\d+|-)\t(.+)$/s.exec(entry);
    if (!match) throw new TypeError("Ledger branch listing is malformed");
    const [, mode, type, oid, sizeText, relativePath] = match;
    const isShard = relativePath.startsWith(SHARD_PREFIX) && relativePath.endsWith(".jsonl") && SEGMENT_ID.test(relativePath.slice(SHARD_PREFIX.length, -".jsonl".length));
    if (!isShard && relativePath !== STATE_PATH) continue;
    if (type !== "blob" || (mode !== "100644" && mode !== "100755")) continue;
    const size = Number(sizeText);
    if (!Number.isSafeInteger(size)) continue;
    if ((isShard && size > config.maxSegmentBytes) || (!isShard && size > MAX_STATE_BYTES)) {
      skippedOversized++;
      continue;
    }
    wanted.push({ path: relativePath, oid, size });
  }
  if (wanted.length > MAX_SNAPSHOT_FILES) throw new RangeError("Ledger branch has too many files to compact safely");
  const totalBytes = wanted.reduce((sum, item) => sum + item.size, 0);
  if (totalBytes > MAX_SNAPSHOT_BYTES) throw new RangeError("Ledger branch is too large to compact safely");
  fs.mkdirSync(path.join(sourceDir, "ledger", "shards"), { recursive: true, mode: 0o700 });
  if (wanted.length) {
    const batch = cp.spawnSync("git", ["cat-file", "--batch"], {
      cwd: workspaceDir,
      input: `${wanted.map(item => item.oid).join("\n")}\n`,
      maxBuffer: totalBytes + wanted.length * 128 + 1024,
      timeout: 300000,
    });
    if (batch.error || batch.status !== 0) throw new Error("Failed to read ledger branch contents");
    const output = batch.stdout;
    let offset = 0;
    for (const item of wanted) {
      const newline = output.indexOf(0x0a, offset);
      if (newline < 0) throw new Error("Truncated ledger object stream");
      const header = output.subarray(offset, newline).toString("utf8").split(" ");
      if (header[0] !== item.oid || header[1] !== "blob" || Number(header[2]) !== item.size) throw new Error("Unexpected ledger object stream");
      const start = newline + 1;
      const end = start + item.size;
      if (end + 1 > output.length || output[end] !== 0x0a) throw new Error("Truncated ledger object stream");
      const destination = path.join(sourceDir, item.path);
      fs.mkdirSync(path.dirname(destination), { recursive: true, mode: 0o700 });
      fs.writeFileSync(destination, output.subarray(start, end), { mode: 0o600 });
      offset = end + 1;
    }
  }
  return { files: wanted.length, bytes: totalBytes, skippedOversized };
}

/**
 * Read and verify every canonical segment in a materialized ledger.
 * @param {string} sourceDir
 * @param {ReturnType<typeof parseCompactionConfig>} config
 */
function loadSegments(sourceDir, config) {
  const ledger = new Ledger({ memoryDir: sourceDir, maxSegmentBytes: config.maxSegmentBytes, maxRecordBytes: config.maxRecordBytes, maxPatchBytes: config.maxPatchBytes });
  const shardDir = path.join(sourceDir, "ledger", "shards");
  const names = fs.existsSync(shardDir) ? fs.readdirSync(shardDir).filter(name => name.endsWith(".jsonl") && SEGMENT_ID.test(name.slice(0, -6))) : [];
  /** @type {Map<string, {id: string, bytes: number, sha256: string, records: any[]}>} */
  const segments = new Map();
  let invalid = 0;
  for (const name of names.sort()) {
    const id = name.slice(0, -6);
    try {
      const fullPath = path.join(shardDir, name);
      const stat = fs.lstatSync(fullPath);
      if (!stat.isFile() || stat.isSymbolicLink()) throw new TypeError("Invalid ledger segment file");
      const bytes = fs.readFileSync(fullPath);
      const records = ledger.readRecords(id);
      segments.set(id, { id, bytes: bytes.length, sha256: digestBytes(bytes), records });
    } catch {
      invalid++;
    }
  }
  return { segments, invalid };
}

/**
 * @param {{segments: Map<string, any>}} loaded
 * @param {string[]} ids
 * @param {ReturnType<typeof parseCompactionConfig>} config
 */
function unionRecords(loaded, ids, config) {
  const union = new Map();
  for (const id of ids) {
    for (const record of loaded.segments.get(id).records) union.set(record.sha, record);
  }
  return buildSegment([...union.values()], { metadata: SEGMENT_METADATA, maxSegmentBytes: config.maxSegmentBytes });
}

/**
 * Read the maintenance-owned compaction state. Invalid state is ignored rather than trusted.
 * @param {string} sourceDir
 */
function readState(sourceDir) {
  const fullPath = path.join(sourceDir, STATE_PATH);
  try {
    const stat = fs.lstatSync(fullPath);
    if (!stat.isFile() || stat.isSymbolicLink() || stat.size > MAX_STATE_BYTES) return null;
    const state = JSON.parse(fs.readFileSync(fullPath, "utf8"));
    if (
      !hasExactKeys(state, STATE_KEYS) ||
      state.version !== STATE_VERSION ||
      typeof state.ledger !== "string" ||
      !DIGEST.test(state.last_plan_id) ||
      !TRIGGERS.has(state.last_trigger) ||
      typeof state.last_applied_at !== "string" ||
      !ISO_TIMESTAMP.test(state.last_applied_at) ||
      Number.isNaN(Date.parse(state.last_applied_at)) ||
      !Array.isArray(state.recent_plan_ids) ||
      state.recent_plan_ids.length > MAX_RECENT_PLANS ||
      !state.recent_plan_ids.every(id => typeof id === "string" && DIGEST.test(id))
    ) {
      return null;
    }
    return state;
  } catch {
    return null;
  }
}

/**
 * Decide whether maintenance should consider compacting this ledger now.
 * @param {{config: ReturnType<typeof parseCompactionConfig>, trigger: string, state: any, now?: Date}} options
 */
function isCompactionDue({ config, trigger, state, now = new Date() }) {
  if (trigger === "requested") return { due: true, reason: "explicitly requested" };
  const interval = SCHEDULE_INTERVALS_MS[config.schedule];
  if (interval === null) return { due: false, reason: "schedule is manual; compaction only runs when requested" };
  if (!state) return { due: true, reason: "no previous compaction recorded" };
  const elapsed = now.getTime() - Date.parse(state.last_applied_at);
  if (!(elapsed >= interval)) return { due: false, reason: `last compaction at ${state.last_applied_at} is within the ${config.schedule} schedule` };
  return { due: true, reason: `${config.schedule} schedule elapsed` };
}

/**
 * Select the source segments to compact.
 * @param {{segments: Map<string, any>}} loaded
 * @param {ReturnType<typeof parseCompactionConfig>} config
 * @returns {{sources: string[], reason?: undefined} | {sources: null, reason: string}}
 */
function selectSources(loaded, config) {
  const candidates = [...loaded.segments.values()].filter(segment => segment.bytes < config.maxSegmentBytes).sort((left, right) => (left.id < right.id ? -1 : left.id > right.id ? 1 : 0));
  if (candidates.length < config.minSegments) {
    return { sources: null, reason: `${candidates.length} eligible segment(s); compaction needs at least ${config.minSegments}` };
  }
  const selected = [];
  const seen = new Set();
  let bytes = 0;
  for (const segment of candidates) {
    if (selected.length >= config.maxSegments) break;
    let added = 0;
    for (const record of segment.records) {
      if (!seen.has(record.sha)) added += Buffer.byteLength(canonicalJSON(record)) + 1;
    }
    if (bytes + added > config.maxSegmentBytes) continue;
    for (const record of segment.records) seen.add(record.sha);
    bytes += added;
    selected.push(segment.id);
  }
  if (selected.length < 2) return { sources: null, reason: "no combination of eligible segments fits within max-segment-kb" };
  return { sources: selected };
}

/** @param {any} plan */
function computePlanId(plan) {
  const body = {
    version: plan.version,
    ledger: plan.ledger,
    branch: plan.branch,
    sources: plan.sources.map(source => ({ segment: source.segment, sha256: source.sha256 })),
    replacement: { segment: plan.replacement.segment, sha256: plan.replacement.sha256 },
  };
  return `sha256:${crypto.createHash("sha256").update(canonicalJSON(body)).digest("hex")}`;
}

/**
 * Build a compaction plan from a verified snapshot. Returns null when there is nothing to do.
 * @param {{loaded: {segments: Map<string, any>}, sources: string[], config: ReturnType<typeof parseCompactionConfig>, trigger: string, baseCommit: string, now?: Date}} options
 */
function createPlan({ loaded, sources, config, trigger, baseCommit, now = new Date() }) {
  const replacement = unionRecords(loaded, sources, config);
  const retired = sources.filter(id => id !== replacement.id).sort();
  if (retired.length === 0) return null;
  const plan = {
    version: PLAN_VERSION,
    ledger: config.name,
    branch: config.branch,
    trigger,
    created_at: now.toISOString(),
    base_commit: baseCommit,
    sources: retired.map(id => {
      const segment = loaded.segments.get(id);
      return { segment: id, sha256: segment.sha256, bytes: segment.bytes, records: segment.records.map(record => record.sha).sort() };
    }),
    replacement: {
      segment: replacement.id,
      sha256: digestBytes(replacement.content),
      bytes: replacement.content.length,
      records: replacement.records.map(record => record.sha).sort(),
    },
    plan_id: "",
  };
  plan.plan_id = computePlanId(plan);
  return plan;
}

/**
 * Strictly validate an untrusted compaction plan for the configured ledger.
 * @param {any} plan untrusted input
 * @param {ReturnType<typeof parseCompactionConfig>} config
 */
function validatePlan(plan, config) {
  const reject = message => {
    throw new RejectedPlanError(`Rejected ledger compaction plan: ${message}`);
  };
  if (!hasExactKeys(plan, PLAN_KEYS)) reject("unexpected plan structure");
  const candidate = plan;
  if (candidate.version !== PLAN_VERSION) reject("unsupported plan version");
  if (candidate.ledger !== config.name) reject("plan targets a different ledger");
  if (candidate.branch !== config.branch) reject("plan targets a different branch");
  if (!TRIGGERS.has(candidate.trigger)) reject("invalid trigger");
  if (typeof candidate.created_at !== "string" || !ISO_TIMESTAMP.test(candidate.created_at) || Number.isNaN(Date.parse(candidate.created_at))) reject("invalid created_at");
  if (typeof candidate.base_commit !== "string" || !COMMIT.test(candidate.base_commit)) reject("invalid base_commit");
  if (!Array.isArray(candidate.sources) || candidate.sources.length === 0 || candidate.sources.length > Math.min(config.maxSegments, MAX_PLAN_SOURCES)) reject("invalid source count");
  let totalRecords = 0;
  const sourceRecords = new Set();
  for (let index = 0; index < candidate.sources.length; index++) {
    const source = candidate.sources[index];
    if (!hasExactKeys(source, SOURCE_KEYS)) reject("unexpected source structure");
    if (typeof source.segment !== "string" || !SEGMENT_ID.test(source.segment)) reject("invalid source segment identity");
    if (index > 0 && !(candidate.sources[index - 1].segment < source.segment)) reject("sources must be unique and sorted");
    if (typeof source.sha256 !== "string" || !DIGEST.test(source.sha256)) reject("invalid source digest");
    if (!isIntegerInRange(source.bytes, 1, config.maxSegmentBytes)) reject("invalid source size");
    if (!isStrictlySortedList(source.records, DIGEST, MAX_PLAN_RECORDS)) reject("invalid source record identities");
    totalRecords += source.records.length;
    if (totalRecords > MAX_PLAN_RECORDS) reject("too many records");
    for (const sha of source.records) sourceRecords.add(sha);
  }
  const replacement = candidate.replacement;
  if (!hasExactKeys(replacement, SOURCE_KEYS)) reject("unexpected replacement structure");
  if (typeof replacement.segment !== "string" || !SEGMENT_ID.test(replacement.segment)) reject("invalid replacement segment identity");
  if (candidate.sources.some(source => source.segment === replacement.segment)) reject("replacement cannot also be a source");
  if (typeof replacement.sha256 !== "string" || !DIGEST.test(replacement.sha256)) reject("invalid replacement digest");
  if (!isIntegerInRange(replacement.bytes, 1, config.maxSegmentBytes)) reject("invalid replacement size");
  if (!isStrictlySortedList(replacement.records, DIGEST, MAX_PLAN_RECORDS)) reject("invalid replacement record identities");
  const replacementRecords = new Set(replacement.records);
  for (const sha of sourceRecords) {
    if (!replacementRecords.has(sha)) reject("replacement does not preserve every source record");
  }
  if (typeof candidate.plan_id !== "string" || candidate.plan_id !== computePlanId(candidate)) reject("plan_id does not match plan contents");
  return candidate;
}

/**
 * Read a plan artifact from disk without following symlinks and with a strict size bound.
 * @param {string} file
 */
function readPlanFile(file) {
  const stat = fs.lstatSync(file);
  if (!stat.isFile() || stat.isSymbolicLink() || stat.size === 0 || stat.size > MAX_PLAN_BYTES) throw new RejectedPlanError("Rejected ledger compaction plan: invalid plan file");
  const fd = fs.openSync(file, fs.constants.O_RDONLY | (fs.constants.O_NOFOLLOW || 0));
  let content;
  try {
    content = fs.readFileSync(fd);
  } finally {
    fs.closeSync(fd);
  }
  if (content.length > MAX_PLAN_BYTES) throw new RejectedPlanError("Rejected ledger compaction plan: invalid plan file");
  try {
    return JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(content));
  } catch {
    throw new RejectedPlanError("Rejected ledger compaction plan: malformed JSON");
  }
}

/**
 * @typedef {{sourceSegments: number, sourceRecords: number, replacementRecords: number, bytesBefore: number, bytesAfter: number, unrelatedSegments: number}} PrepareStats
 * @typedef {{status: "ready", reason: string, additions: {path: string, contents: Buffer}[], deletions: {path: string}[], stats: PrepareStats}} PreparedTransition
 * @typedef {PreparedTransition | {status: "already_applied" | "stale" | "conflict" | "rejected", reason: string}} PrepareResult
 */

/**
 * Revalidate a (strictly validated) plan against the latest ledger state and compute the exact
 * atomic file transition. Never executes user code.
 *
 * Returns one of:
 * - ready: every source is present and unchanged; additions/deletions describe the transition
 * - already_applied: sources are gone and the plan's effect is provably present
 * - stale: the plan no longer matches the ledger (some sources were retired by another plan)
 * - conflict: a referenced segment exists with unexpected content
 * @param {{plan: any, sourceDir: string, config: ReturnType<typeof parseCompactionConfig>, now?: Date}} options
 * @returns {PrepareResult}
 */
function prepareApply({ plan, sourceDir, config, now = new Date() }) {
  const loaded = loadSegments(sourceDir, config);
  const state = readState(sourceDir);
  const shardDir = path.join(sourceDir, "ledger", "shards");
  const present = id => fs.existsSync(path.join(shardDir, `${id}.jsonl`));
  const missing = plan.sources.filter(source => !present(source.segment));
  for (const source of plan.sources) {
    if (!present(source.segment)) continue;
    const actual = loaded.segments.get(source.segment);
    if (!actual) return { status: "conflict", reason: `source segment ${source.segment} is not a valid canonical segment` };
    const actualRecords = actual.records.map(record => record.sha).sort();
    if (actual.sha256 !== source.sha256 || actual.bytes !== source.bytes || actualRecords.length !== source.records.length || actualRecords.some((sha, index) => sha !== source.records[index])) {
      return { status: "conflict", reason: `source segment ${source.segment} no longer matches the plan` };
    }
  }
  const replacementPresent = present(plan.replacement.segment);
  const existingReplacement = loaded.segments.get(plan.replacement.segment);
  if (replacementPresent && (!existingReplacement || existingReplacement.sha256 !== plan.replacement.sha256 || existingReplacement.bytes !== plan.replacement.bytes)) {
    return { status: "conflict", reason: `replacement segment ${plan.replacement.segment} exists with different content` };
  }
  if (missing.length === plan.sources.length) {
    const recorded = !!state && state.recent_plan_ids.includes(plan.plan_id);
    if (replacementPresent || recorded) return { status: "already_applied", reason: "every source segment has already been retired by this plan" };
    return { status: "stale", reason: "every source segment has already been retired" };
  }
  if (missing.length > 0) return { status: "stale", reason: `${missing.length} source segment(s) were retired after planning` };

  // Rebuild the replacement from the branch's own verified records; never trust plan content.
  const sourceIds = plan.sources.map(source => source.segment);
  const rebuilt = existingReplacement ? buildSegment(existingReplacement.records, { metadata: SEGMENT_METADATA, maxSegmentBytes: config.maxSegmentBytes }) : unionRecords(loaded, sourceIds, config);
  const rebuiltRecords = rebuilt.records.map(record => record.sha).sort();
  if (
    rebuilt.id !== plan.replacement.segment ||
    digestBytes(rebuilt.content) !== plan.replacement.sha256 ||
    rebuilt.content.length !== plan.replacement.bytes ||
    rebuiltRecords.length !== plan.replacement.records.length ||
    rebuiltRecords.some((sha, index) => sha !== plan.replacement.records[index])
  ) {
    return { status: "rejected", reason: "replacement segment does not match the content-addressed union of the source records" };
  }

  // Prove the transition is lossless: every record visible before remains visible after.
  const before = new Set();
  for (const segment of loaded.segments.values()) for (const record of segment.records) before.add(record.sha);
  const after = new Set(rebuiltRecords);
  for (const segment of loaded.segments.values()) {
    if (sourceIds.includes(segment.id)) continue;
    for (const record of segment.records) after.add(record.sha);
  }
  for (const sha of before) {
    if (!after.has(sha)) return { status: "rejected", reason: "transition would lose ledger records" };
  }

  const recent = [plan.plan_id, ...((state && state.recent_plan_ids) || []).filter(id => id !== plan.plan_id)].slice(0, MAX_RECENT_PLANS);
  const nextState = {
    version: STATE_VERSION,
    ledger: config.name,
    last_plan_id: plan.plan_id,
    last_trigger: plan.trigger,
    last_applied_at: now.toISOString(),
    recent_plan_ids: recent,
  };
  /** @type {{path: string, contents: Buffer}[]} */
  const additions = [];
  if (!replacementPresent) additions.push({ path: `${SHARD_PREFIX}${rebuilt.id}.jsonl`, contents: rebuilt.content });
  additions.push({ path: STATE_PATH, contents: Buffer.from(`${canonicalJSON(nextState)}\n`) });
  const deletions = sourceIds.map(id => ({ path: `${SHARD_PREFIX}${id}.jsonl` }));
  const bytesBefore = plan.sources.reduce((sum, source) => sum + source.bytes, 0) + (replacementPresent ? plan.replacement.bytes : 0);
  return {
    status: "ready",
    reason: "plan validated against the latest ledger state",
    additions,
    deletions,
    stats: {
      sourceSegments: sourceIds.length,
      sourceRecords: plan.sources.reduce((sum, source) => sum + source.records.length, 0),
      replacementRecords: rebuiltRecords.length,
      bytesBefore,
      bytesAfter: rebuilt.content.length,
      unrelatedSegments: loaded.segments.size - sourceIds.length - (replacementPresent ? 1 : 0),
    },
  };
}

/**
 * Apply a prepared transition to a local directory. Used to verify plans offline and in tests;
 * the maintenance apply job publishes the same transition through an atomic remote commit.
 * @param {string} sourceDir
 * @param {{additions: {path: string, contents: Buffer}[], deletions: {path: string}[]}} transition
 */
function applyTransitionToDirectory(sourceDir, transition) {
  for (const addition of transition.additions) {
    const destination = path.join(sourceDir, addition.path);
    fs.mkdirSync(path.dirname(destination), { recursive: true });
    fs.writeFileSync(destination, addition.contents, { mode: 0o600 });
  }
  for (const deletion of transition.deletions) fs.rmSync(path.join(sourceDir, deletion.path), { force: true });
}

module.exports = {
  MAX_PLAN_BYTES,
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
};
