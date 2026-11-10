// @ts-check
"use strict";

const fs = require("node:fs");
const { ERR_VALIDATION } = require("./error_codes.cjs");
const { normalizeHarnessRetryRecord, normalizeHarnessAttemptEvent, normalizeHarnessDecisionData, MAX_HARNESS_ATTEMPTS } = require("./harness_retry_record.cjs");

const COMPACT_UNIFIED_SESSION_VERSION = 1;
const MAX_COMPACT_SESSION_BYTES = 16 * 1024;
const MAX_COMPACT_SESSION_EVENTS = 128;
const MAX_COMPACT_STRING_BYTES = 256;
const MAX_SOURCE_LINE_BYTES = 16 * 1024;
const MAX_SOURCE_SCAN_BYTES = 64 * 1024 * 1024;

/** @type {Record<string, readonly string[]>} */
const EVENT_FIELDS = {
  "durable.restore": ["engine", "storage", "runId", "runAttempt", "status", "parentRunId", "artifactId"],
  "durable.session": ["engine", "storage", "runId", "runAttempt", "status", "parentRunId", "sessionId"],
  "session.format": ["version"],
  "workflow.info": ["engineId", "agentVersion", "cliVersion", "awfVersion", "mcpgVersion", "model", "requestedModel", "triggerType", "dryRun", "workflowId", "workflowCallId", "repository", "runId", "runAttempt", "ref", "sha"],
  "session.result": ["sourceEngine", "numTurns", "durationMs", "totalCostUsd", "status", "sourceType", "sessionId", "agentId"],
  "usage.report": ["stepIndex", "provider", "model", "purpose", "status", "aic", "totalAic", "premiumRequests", "durationMs", "sessionId", "agentId"],
  "guardrail.admission": ["status", "maxAiCredits", "threatDetectionMaxAiCredits", "evalsMaxAiCredits"],
  "guardrail.daily_aic": ["status", "exceeded", "total", "estimated", "threshold"],
  "workflow.job_result": ["jobId", "result", "durationMs"],
  "memory.push": ["memoryId", "jobResult", "outcome", "status", "branch", "commit"],
  "execution.result": ["outcome", "conclusion", "exitCode", "durationMs", "startedAt", "finishedAt"],
  "harness.retry": ["attempts", "exitCode"],
  "harness.attempt": [],
  "harness.decision": ["proposedAttempt", "outcome", "latencyMs", "avoidedInvocation", "error"],
  "engine.retry": ["sourceEngine", "sourceType", "sessionId", "agentId"],
  "grader.result": ["id", "status", "value", "score", "unit", "passed", "direction", "threshold", "enabled", "min", "max", "version"],
};
EVENT_FIELDS["firewall.token_usage"] = EVENT_FIELDS["usage.report"];
const USAGE_FIELDS = ["totalTokens", "inputTokens", "outputTokens", "reasoningOutputTokens", "cacheReadInputTokens", "cacheCreationInputTokens", "inputTokensIncludeCache"];
const GRADER_RUN_FIELDS = ["id", "attempt", "graderCount", "passed", "failed", "errors"];

/** @type {Record<string, number>} Lower numbers preserve more essential evidence. */
const SESSION_EVENT_PRIORITIES = {
  "durable.restore": 2,
  "durable.session": 2,
  "session.format": 0,
  "workflow.info": 1,
  "execution.result": 2,
  "session.result": 2,
  "workflow.job_result": 2,
  "guardrail.admission": 3,
  "guardrail.daily_aic": 3,
  "grader.result": 3,
  "harness.retry": 4,
  "harness.attempt": 4,
  "harness.decision": 4,
  "engine.retry": 4,
  "usage.report": 5,
  "firewall.token_usage": 5,
  "memory.push": 6,
};
const FAILURE_STATUSES = new Set(["failure", "failed", "fail", "error", "denied", "blocked", "quota_blocked", "exceeded", "structural_error", "transient_error", "timed_out", "cancelled", "interrupted"]);

/** @param {unknown} event @returns {number} */
function sessionEventPriority(event) {
  if (!isRecord(event) || typeof event.type !== "string") return 7;
  const priority = Object.hasOwn(SESSION_EVENT_PRIORITIES, event.type) ? SESSION_EVENT_PRIORITIES[event.type] : 7;
  const data = isRecord(event.data) ? event.data : {};
  const run = data.run;
  const failed =
    ["status", "result", "outcome", "conclusion", "jobResult"].some(key => typeof data[key] === "string" && FAILURE_STATUSES.has(data[key])) ||
    (typeof data.exitCode === "number" && data.exitCode !== 0) ||
    data.exceeded === true ||
    data.passed === false ||
    (isRecord(run) && ["failed", "errors"].some(key => typeof run[key] === "number" && run[key] > 0));
  return failed ? Math.min(priority, 2) : priority;
}

/**
 * Drop the latest least-essential record, preserving earlier evidence on ties.
 * @param {readonly unknown[]} events
 * @returns {number}
 */
function leastEssentialSessionEventIndex(events) {
  let index = -1;
  let priority = -1;
  for (let i = 0; i < events.length; i++) {
    const candidatePriority = sessionEventPriority(events[i]);
    if (candidatePriority >= priority) {
      priority = candidatePriority;
      index = i;
    }
  }
  return index;
}

/**
 * @typedef {{version: number, session: string, includedEvents: number, omittedEvents: number, oversizedLines: number, scanTruncated: boolean, truncated: boolean}} CompactUnifiedSession
 */

/** @param {unknown} value @returns {value is Record<string, unknown>} */
function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/** @param {Record<string, unknown>} record @param {readonly string[]} fields */
function projectScalars(record, fields) {
  /** @type {Record<string, string | number | boolean>} */
  const projected = {};
  for (const field of fields) {
    if (!Object.hasOwn(record, field)) continue;
    const value = record[field];
    if (typeof value === "boolean" || (typeof value === "number" && Number.isFinite(value))) projected[field] = value;
    else if (typeof value === "string" && Buffer.byteLength(value) <= MAX_COMPACT_STRING_BYTES && Buffer.from(value).toString("utf8") === value) projected[field] = value;
  }
  return projected;
}

/** @param {unknown} event */
function projectEvent(event) {
  if (!isRecord(event) || typeof event.type !== "string" || !Object.hasOwn(EVENT_FIELDS, event.type) || !isRecord(event.data)) return undefined;
  /** @type {Record<string, unknown>} */
  const data = projectScalars(event.data, EVENT_FIELDS[event.type]);
  if (event.type === "harness.decision") {
    const decision = normalizeHarnessDecisionData(event.data);
    if (!decision) return undefined;
    Object.assign(data, decision);
  }
  if (event.type === "engine.retry") {
    const retry = event.data;
    if (typeof retry.attempt !== "number" || !Number.isSafeInteger(retry.attempt) || retry.attempt < 1) return undefined;
    data.attempt = retry.attempt;
    for (const field of ["maxRetries", "backoffMs"]) {
      const value = retry[field];
      if (typeof value === "number" && Number.isSafeInteger(value) && value >= 0) data[field] = value;
    }
  }
  if (event.type === "harness.retry" || event.type === "harness.attempt") {
    const retry = event.type === "harness.retry" ? normalizeHarnessRetryRecord(event.data) : normalizeHarnessAttemptEvent(event.data);
    if (!retry) return undefined;
    Object.assign(data, retry);
    if (event.type === "harness.retry") delete data.decisionResults;
  }
  if (["session.result", "usage.report", "firewall.token_usage"].includes(event.type) && isRecord(event.data.usage)) data.usage = projectScalars(event.data.usage, USAGE_FIELDS);
  if (event.type === "grader.result" && isRecord(event.data.run)) data.run = projectScalars(event.data.run, GRADER_RUN_FIELDS);
  /** @type {Record<string, unknown>} */
  const projected = { type: event.type, data, ...projectScalars(event, ["timestamp"]) };
  if (isRecord(event.provenance)) {
    const provenance = projectScalars(event.provenance, ["component", "phase", "path", "index"]);
    if (typeof event.provenance.timestampMs === "number" && Number.isFinite(event.provenance.timestampMs)) provenance.timestampMs = event.provenance.timestampMs;
    for (const field of ["attemptId", "startupAttemptId"]) {
      const id = event.provenance[field];
      if (typeof id !== "string") continue;
      const parts = id.split(":");
      if (
        /^(harness:[1-9][0-9]{0,2}|startup:[1-9][0-9]{0,2}(:harness:[1-9][0-9]{0,2})?)$/.test(id) &&
        Number(parts[1]) <= MAX_HARNESS_ATTEMPTS &&
        (parts.length === 2 || Number(parts[3]) <= MAX_HARNESS_ATTEMPTS) &&
        (field !== "startupAttemptId" || (parts.length === 2 && parts[0] === "startup"))
      )
        provenance[field] = id;
    }
    projected.provenance = provenance;
  }
  return projected;
}

/**
 * Deterministic, lossy metadata projection. It never mutates its source events.
 * @param {Iterable<unknown>} events
 * @returns {CompactUnifiedSession}
 */
function compactUnifiedSession(events) {
  /** @type {Array<{event: Record<string, unknown>, line: string, length: number}>} */
  const retained = [];
  let bytes = 0;
  let inspectedEvents = 0;
  let truncated = false;
  for (const event of events) {
    inspectedEvents++;
    const projected = projectEvent(event);
    if (!projected) continue;
    const line = JSON.stringify(projected) + "\n";
    const length = Buffer.byteLength(line);
    if (length > MAX_COMPACT_SESSION_BYTES) {
      truncated = true;
      continue;
    }
    // Simulate eviction so a candidate that cannot fit never costs lower-priority records already retained.
    const trial = [...retained, { event: projected, line, length }];
    let trialBytes = bytes + length;
    let evictedCandidate = false;
    while (trial.length > MAX_COMPACT_SESSION_EVENTS || trialBytes > MAX_COMPACT_SESSION_BYTES) {
      const index = leastEssentialSessionEventIndex(trial.map(candidate => candidate.event));
      if (index === trial.length - 1) evictedCandidate = true;
      trialBytes -= trial[index].length;
      trial.splice(index, 1);
      truncated = true;
      if (evictedCandidate) break;
    }
    if (evictedCandidate) continue;
    retained.splice(0, retained.length, ...trial);
    bytes = trialBytes;
  }
  const session = retained.map(candidate => candidate.line).join("");
  return { version: COMPACT_UNIFIED_SESSION_VERSION, session, includedEvents: retained.length, omittedEvents: inspectedEvents - retained.length, oversizedLines: 0, scanTruncated: false, truncated };
}

/**
 * Skip oversized records without retaining or decoding their unbounded content.
 * @param {number} fd
 * @param {number} size
 * @param {{oversizedLines: number}} stats
 */
function* readEvents(fd, size, stats) {
  const chunk = Buffer.alloc(64 * 1024);
  const line = Buffer.alloc(MAX_SOURCE_LINE_BYTES);
  let length = 0;
  let oversized = false;
  let read = 0;
  /** @returns {unknown} */
  function decode() {
    if (oversized) {
      stats.oversizedLines++;
      return undefined;
    }
    try {
      const text = new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(line.subarray(0, length));
      return text.trim() ? JSON.parse(text) : undefined;
    } catch {
      // Native parser diagnostics can include private source content.
      throw new Error(`${ERR_VALIDATION}: Invalid UTF-8 or JSON in compact unified session source.`);
    }
  }
  while (read < Math.min(size, MAX_SOURCE_SCAN_BYTES)) {
    const count = fs.readSync(fd, chunk, 0, Math.min(chunk.length, size - read, MAX_SOURCE_SCAN_BYTES - read), null);
    if (!count) throw new Error(`${ERR_VALIDATION}: Compact unified session source ended before its snapshot size.`);
    read += count;
    let start = 0;
    while (start < count) {
      const newline = chunk.indexOf(10, start);
      const end = newline < 0 || newline >= count ? count : newline;
      const part = end - start;
      if (!oversized && length + part <= line.length) {
        chunk.copy(line, length, start, end);
        length += part;
      } else oversized = true;
      if (end < count) {
        if (length || oversized) yield decode();
        length = 0;
        oversized = false;
      }
      start = end + 1;
    }
  }
  // A scan-boundary fragment is not a complete source record.
  if (read === size && (length || oversized)) yield decode();
}

/** @param {string} filePath @returns {CompactUnifiedSession} */
function readCompactUnifiedSession(filePath) {
  const fd = fs.openSync(filePath, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW | fs.constants.O_NONBLOCK);
  try {
    const stat = fs.fstatSync(fd);
    if (!stat.isFile() || !Number.isSafeInteger(stat.size) || stat.size === 0) throw new Error(`${ERR_VALIDATION}: Compact unified session requires a nonempty regular source file.`);
    const stats = { oversizedLines: 0 };
    const compact = compactUnifiedSession(readEvents(fd, stat.size, stats));
    const scanTruncated = stat.size > MAX_SOURCE_SCAN_BYTES;
    return { ...compact, ...stats, scanTruncated, truncated: compact.truncated || scanTruncated || stats.oversizedLines > 0 };
  } finally {
    fs.closeSync(fd);
  }
}

module.exports = {
  compactUnifiedSession,
  readCompactUnifiedSession,
  sessionEventPriority,
  leastEssentialSessionEventIndex,
  COMPACT_UNIFIED_SESSION_VERSION,
  MAX_COMPACT_SESSION_BYTES,
  MAX_COMPACT_SESSION_EVENTS,
  MAX_COMPACT_STRING_BYTES,
  MAX_SOURCE_LINE_BYTES,
  MAX_SOURCE_SCAN_BYTES,
};
